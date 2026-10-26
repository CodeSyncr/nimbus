package session

import (
	"bufio"
	"context"
	"errors"
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/lucid"
	"github.com/CodeSyncr/nimbus/redis"
	"github.com/alicebob/miniredis/v2"
	"gorm.io/driver/sqlite"
)

func securityStores(t *testing.T) map[string]Store {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { client.Close() })
	db, err := lucid.Open(sqlite.Open(filepath.Join(t.TempDir(), "sessions.db")), &lucid.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	database := NewDatabaseStore(db, "sessions")
	if err := database.EnsureTable(); err != nil {
		t.Fatal(err)
	}
	mem, rs := NewMemoryStore(), NewRedisStore(client)
	key := []byte("01234567890123456789012345678901")
	return map[string]Store{"memory": mem, "redis": rs, "database": database, "cookie-memory": NewCookieStoreWithRegistry(key, NewMemoryStore()), "cookie-redis": NewCookieStoreWithRegistry(key, NewRedisStoreWithPrefix(client, "cookie-registry:")), "cookie-database": NewCookieStoreWithRegistry(key, NewDatabaseStore(db, "sessions"))}
}

func TestRevocationWinsOverStaleRequest(t *testing.T) {
	for name, store := range securityStores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			id, err := store.Set(ctx, "", map[string]any{"user_id": "42"}, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			req := func() *nhttp.Context {
				r := httptest.NewRequest("GET", "/", nil)
				r.AddCookie(&nhttp.Cookie{Name: "nimbus_session", Value: id})
				return &nhttp.Context{Request: r, Response: httptest.NewRecorder()}
			}
			mw := Middleware(Config{Store: store})
			loaded, release := make(chan struct{}), make(chan struct{})
			done := make(chan error, 1)
			a := req()
			go func() {
				done <- mw(func(c *nhttp.Context) error {
					close(loaded)
					<-release
					c.Session.Set("last_seen", "now")
					c.String(200, "done")
					return nil
				})(a)
			}()
			<-loaded
			if err := mw(func(c *nhttp.Context) error { return FromContext(c.Ctx()).Invalidate(c.Ctx()) })(req()); err != nil {
				t.Fatal(err)
			}
			close(release)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if data, err := store.Get(ctx, id); err != nil || data != nil {
				t.Fatalf("revoked session returned: %v %v", data, err)
			}
			if cookies := a.Response.Header().Values("Set-Cookie"); len(cookies) != 0 {
				t.Fatalf("stale request issued cookie: %v", cookies)
			}
		})
	}
}

func TestAtomicUpdateRotationAndCookieFamilyRevocation(t *testing.T) {
	for name, store := range securityStores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			data := map[string]any{"user_id": "u"}
			id, err := store.Set(ctx, "", data, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			next, err := store.(Updater).Update(ctx, id, data, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			// Logout may have loaded the old cookie before the update completed.
			if err := store.Destroy(ctx, id); err != nil {
				t.Fatal(err)
			}
			if got, err := store.Get(ctx, next); err != nil || got != nil {
				t.Fatalf("replacement survived logout: %v %v", got, err)
			}
			if _, err := store.(Updater).Update(ctx, next, data, time.Hour); !errors.Is(err, ErrExpired) {
				t.Fatalf("stale update: %v", err)
			}
			if _, err := store.(Rotator).Rotate(ctx, id, data, time.Hour); !errors.Is(err, ErrExpired) {
				t.Fatalf("stale rotation: %v", err)
			}
			live, err := store.Set(ctx, "", data, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			rotated, err := store.(Rotator).Rotate(ctx, live, data, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			if rotated == live {
				t.Fatal("ID did not rotate")
			}
			if got, _ := store.Get(ctx, live); got != nil {
				t.Fatal("old ID survived rotation")
			}
			if got, _ := store.Get(ctx, rotated); got["user_id"] != "u" {
				t.Fatal("rotation lost data")
			}
		})
	}
}

func TestMemorySnapshotIsolationAndParallelFlash(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	original := map[string]any{"_flash": map[string]any{"notice": "original"}, "list": []any{map[string]any{"value": "original"}}}
	id, err := store.Set(ctx, "", original, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	original["_flash"].(map[string]any)["notice"] = "outside"
	a, _ := store.Get(ctx, id)
	a["_flash"].(map[string]any)["notice"] = "request A"
	a["list"].([]any)[0].(map[string]any)["value"] = "request A"
	b, _ := store.Get(ctx, id)
	if b["_flash"].(map[string]any)["notice"] != "original" || b["list"].([]any)[0].(map[string]any)["value"] != "original" {
		t.Fatal("shared snapshot")
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRequest("GET", "/", nil)
			r.AddCookie(&nhttp.Cookie{Name: "nimbus_session", Value: id})
			c := &nhttp.Context{Request: r, Response: httptest.NewRecorder()}
			err := Middleware(Config{Store: store})(func(c *nhttp.Context) error {
				<-start
				for j := 0; j < 500; j++ {
					c.Session.SetFlash("notice", j)
					c.Session.GetFlash("notice")
				}
				return nil
			})(c)
			if err != nil {
				t.Error(err)
			}
		}()
	}
	close(start)
	wg.Wait()
}

func TestSessionMiddlewarePreservesSSE(t *testing.T) {
	rec := httptest.NewRecorder()
	c := &nhttp.Context{Request: httptest.NewRequest("GET", "/events", nil), Response: rec}
	err := Middleware(Config{Store: NewMemoryStore()})(func(c *nhttp.Context) error {
		c.Session.Set("hello", "world")
		return c.SSEStream(func(w *nhttp.SSEWriter) error { return w.Event("update", "hello") })
	})(c)
	if err != nil {
		t.Fatal(err)
	}
	if !rec.Flushed || !strings.Contains(rec.Body.String(), "data: hello") || len(rec.Result().Cookies()) == 0 {
		t.Fatalf("stream/cookie not flushed: %s", rec.Body.String())
	}
}

type failingSessionStore struct{ Store }

func (s failingSessionStore) Set(context.Context, string, map[string]any, time.Duration) (string, error) {
	return "", errors.New("storage unavailable")
}

func TestSessionSaveFailureDoesNotSendSuccess(t *testing.T) {
	rec := httptest.NewRecorder()
	c := &nhttp.Context{Request: httptest.NewRequest("GET", "/", nil), Response: rec}
	err := Middleware(Config{Store: failingSessionStore{NewMemoryStore()}})(func(c *nhttp.Context) error {
		c.Session.Set("user_id", "u")
		return c.JSON(200, map[string]any{"ok": true})
	})(c)
	if err == nil || rec.Code != 503 || len(rec.Result().Cookies()) != 0 || strings.Contains(rec.Body.String(), "true") {
		t.Fatalf("false success: %d %s %v", rec.Code, rec.Body.String(), err)
	}
}

func TestUnknownClientIDIsNotAdopted(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&nhttp.Cookie{Name: "nimbus_session", Value: "chosen-by-client"})
	c := &nhttp.Context{Request: req, Response: rec}
	if err := Middleware(Config{Store: NewMemoryStore()})(func(c *nhttp.Context) error { c.Session.Set("value", "x"); return nil })(c); err != nil {
		t.Fatal(err)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Value == "chosen-by-client" {
		t.Fatal("untrusted ID adopted")
	}
}

func TestRegenerateMiddleware(t *testing.T) {
	for name, store := range securityStores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			id, err := store.Set(ctx, "", map[string]any{"value": "keep"}, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("GET", "/", nil)
			req.AddCookie(&nhttp.Cookie{Name: "nimbus_session", Value: id})
			rec := httptest.NewRecorder()
			c := &nhttp.Context{Request: req, Response: rec}
			if err := Middleware(Config{Store: store})(func(c *nhttp.Context) error { c.Session.Regenerate(); c.Session.Set("user_id", "u"); return nil })(c); err != nil {
				t.Fatal(err)
			}
			cookies := rec.Result().Cookies()
			if len(cookies) != 1 || cookies[0].Value == id {
				t.Fatal("missing rotated cookie")
			}
			if old, _ := store.Get(ctx, id); old != nil {
				t.Fatal("old ID retained")
			}
			got, err := store.Get(ctx, cookies[0].Value)
			if err != nil || got["value"] != "keep" || got["user_id"] != "u" {
				t.Fatalf("rotation failed: %v %v", got, err)
			}
		})
	}
}

type waitingSessionStore struct{ Store }

func (s waitingSessionStore) Set(ctx context.Context, _ string, _ map[string]any, _ time.Duration) (string, error) {
	<-ctx.Done()
	return "", ctx.Err()
}

func TestSessionSaveHonorsDeadline(t *testing.T) {
	rec := httptest.NewRecorder()
	c := &nhttp.Context{Request: httptest.NewRequest("GET", "/", nil), Response: rec}
	start := time.Now()
	err := Middleware(Config{Store: waitingSessionStore{NewMemoryStore()}, SaveTimeout: 20 * time.Millisecond})(func(c *nhttp.Context) error { c.Session.Set("x", "y"); return nil })(c)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
		t.Fatalf("unbounded save: %v", err)
	}
}

type upgradeWriter struct {
	*httptest.ResponseRecorder
	conn     net.Conn
	deadline bool
}

func (w *upgradeWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.conn, bufio.NewReadWriter(bufio.NewReader(w.conn), bufio.NewWriter(w.conn)), nil
}
func (w *upgradeWriter) SetWriteDeadline(time.Time) error { w.deadline = true; return nil }
func TestSessionPreservesHijackAndResponseController(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	w := &upgradeWriter{ResponseRecorder: httptest.NewRecorder(), conn: left}
	c := &nhttp.Context{Request: httptest.NewRequest("GET", "/socket", nil), Response: w}
	err := Middleware(Config{Store: NewMemoryStore()})(func(c *nhttp.Context) error {
		c.Session.Set("x", "y")
		if err := stdhttp.NewResponseController(c.Response).SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
			return err
		}
		conn, _, err := c.Response.(stdhttp.Hijacker).Hijack()
		if err != nil {
			return err
		}
		if conn != left {
			t.Fatal("wrong hijacked connection")
		}
		return nil
	})(c)
	if err != nil || !w.deadline || len(w.Result().Cookies()) == 0 {
		t.Fatalf("upgrade/deadline not forwarded: %v", err)
	}
}
