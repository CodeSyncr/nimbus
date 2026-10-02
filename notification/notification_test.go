package notification

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"sync"
	"testing"

	"github.com/CodeSyncr/nimbus/lucid"
	"github.com/CodeSyncr/nimbus/mail"
	"github.com/CodeSyncr/nimbus/plugins/transmit"
	"gorm.io/driver/sqlite"
)

type recordingDriver struct {
	sent []*mail.Message
	err  error
}

func (d *recordingDriver) Send(m *mail.Message) error {
	d.sent = append(d.sent, m)
	return d.err
}

type welcome struct {
	noMail  bool
	channel string
	slack   *SlackMessage
	discord *DiscordMessage
	dbData  map[string]any
}

func (w welcome) ToMail() *mail.Message {
	if w.noMail {
		return nil
	}
	return &mail.Message{To: []string{"ana@example.com"}, Subject: "Welcome"}
}
func (w welcome) ToBroadcast() (string, any) { return w.channel, map[string]any{"hi": true} }
func (w welcome) ToSlack() *SlackMessage     { return w.slack }
func (w welcome) ToDiscord() *DiscordMessage { return w.discord }
func (w welcome) ToDatabase() map[string]any { return w.dbData }

func useMail(t *testing.T, d mail.Driver) {
	t.Helper()
	prev := mail.Default
	mail.Default = d
	t.Cleanup(func() { mail.Default = prev })
}

var broadcastsMu sync.Mutex
var broadcasts []string

func init() {
	transmit.OnBroadcast(func(channel string, _ any) {
		broadcastsMu.Lock()
		broadcasts = append(broadcasts, channel)
		broadcastsMu.Unlock()
	})
}

func sawBroadcast(channel string) bool {
	broadcastsMu.Lock()
	defer broadcastsMu.Unlock()
	for _, c := range broadcasts {
		if c == channel {
			return true
		}
	}
	return false
}

func TestSendMailsBroadcastsAndRunsHooks(t *testing.T) {
	d := &recordingDriver{}
	useMail(t, d)
	var hookErr error
	hooked := false
	AfterSend(func(_ Notification, err error) { hooked, hookErr = true, err })

	if err := Send(welcome{channel: "users/1"}); err != nil {
		t.Fatal(err)
	}
	if len(d.sent) != 1 || d.sent[0].Subject != "Welcome" {
		t.Fatalf("mail not sent: %+v", d.sent)
	}
	if !sawBroadcast("users/1") {
		t.Fatal("notification was not broadcast")
	}
	if !hooked || hookErr != nil {
		t.Fatalf("AfterSend hook: ran=%v err=%v", hooked, hookErr)
	}
}

func TestSendStopsOnMailError(t *testing.T) {
	useMail(t, &recordingDriver{err: errors.New("smtp down")})
	if err := Send(welcome{channel: "users/never"}); err == nil {
		t.Fatal("mail failure should be returned")
	}
	if sawBroadcast("users/never") {
		t.Fatal("should not broadcast after mail failed")
	}
}

func TestSendWithoutMailOrBroadcast(t *testing.T) {
	useMail(t, nil)
	if err := Send(welcome{noMail: true}); err != nil {
		t.Fatalf("a notification without mail should not need a driver: %v", err)
	}
	if err := SendMail(welcome{}); err == nil {
		t.Fatal("mail without a configured driver should fail")
	}
}

func TestWebhookChannels(t *testing.T) {
	var got map[string]any
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		if r.Header.Get("Content-Type") != "application/json" {
			w.WriteHeader(400)
		}
	}))
	defer ok.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte("nope"))
	}))
	defer bad.Close()

	n := welcome{slack: &SlackMessage{Text: "hello slack"}, discord: &DiscordMessage{Content: "hello discord"}}
	if err := NewSlackChannel(ok.URL).Send(n); err != nil || got["text"] != "hello slack" {
		t.Fatalf("slack: %v %v", err, got)
	}
	if err := NewDiscordChannel(ok.URL).Send(n); err != nil || got["content"] != "hello discord" {
		t.Fatalf("discord: %v %v", err, got)
	}
	if err := NewSlackChannel(bad.URL).Send(n); err == nil {
		t.Fatal("slack 500 should fail")
	}
	if err := NewDiscordChannel(bad.URL).Send(n); err == nil {
		t.Fatal("discord 500 should fail")
	}
	// nil messages skip the channel entirely.
	if err := NewSlackChannel("http://127.0.0.1:1").Send(welcome{}); err != nil {
		t.Fatalf("nil slack message: %v", err)
	}
	if err := NewDiscordChannel("http://127.0.0.1:1").Send(welcome{}); err != nil {
		t.Fatalf("nil discord message: %v", err)
	}
}

func TestDatabaseChannel(t *testing.T) {
	db, err := lucid.Open(sqlite.Open(filepath.Join(t.TempDir(), "n.db")), &lucid.Config{})
	if err != nil {
		t.Fatal(err)
	}
	ch := NewDatabaseChannel(db)
	if err := ch.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := ch.Send(ctx, "7", "user", "invoice.paid", welcome{dbData: map[string]any{"n": i}}); err != nil {
			t.Fatal(err)
		}
	}
	_ = ch.Send(ctx, "7", "user", "skipped", welcome{}) // nil data: skipped
	_ = ch.Send(ctx, "8", "user", "other", welcome{dbData: map[string]any{}})

	all, _ := ch.All(ctx, "7", "user")
	if len(all) != 3 {
		t.Fatalf("All = %d, want 3", len(all))
	}
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(all[0].ID) {
		t.Fatalf("ID is not a v4 UUID: %q", all[0].ID)
	}
	if err := ch.MarkAsRead(ctx, all[0].ID); err != nil {
		t.Fatal(err)
	}
	unread, _ := ch.Unread(ctx, "7", "user")
	if len(unread) != 2 {
		t.Fatalf("Unread after MarkAsRead = %d, want 2", len(unread))
	}
	_ = ch.MarkAllAsRead(ctx, "7", "user")
	if unread, _ = ch.Unread(ctx, "7", "user"); len(unread) != 0 {
		t.Fatalf("Unread after MarkAllAsRead = %d", len(unread))
	}
	_ = ch.Delete(ctx, all[0].ID)
	if all, _ = ch.All(ctx, "7", "user"); len(all) != 2 {
		t.Fatalf("All after Delete = %d", len(all))
	}
	_ = ch.DeleteAll(ctx, "7", "user")
	if all, _ = ch.All(ctx, "7", "user"); len(all) != 0 {
		t.Fatalf("All after DeleteAll = %d", len(all))
	}
	if other, _ := ch.All(ctx, "8", "user"); len(other) != 1 {
		t.Fatal("DeleteAll removed another notifiable's notifications")
	}
}
