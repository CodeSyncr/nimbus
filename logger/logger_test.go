package logger

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	nhttp "github.com/CodeSyncr/nimbus/http"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// observe swaps the global logger for an in-memory one for the test.
func observe(t *testing.T) *observer.ObservedLogs {
	t.Helper()
	prev := Log
	core, logs := observer.New(zap.DebugLevel)
	Set(zap.New(core))
	t.Cleanup(func() { Log = prev })
	return logs
}

func TestConvenienceFunctionsAndFields(t *testing.T) {
	logs := observe(t)
	Info("paid", "amount", 42)
	Warnf("low %s", "stock")
	WithFields(map[string]any{"user": 7}).Error("boom")

	all := logs.All()
	if len(all) != 3 {
		t.Fatalf("got %d entries", len(all))
	}
	if all[0].Message != "paid" || all[0].ContextMap()["amount"] != int64(42) {
		t.Fatalf("Info entry = %+v", all[0])
	}
	if all[1].Message != "low stock" {
		t.Fatalf("Warnf entry = %q", all[1].Message)
	}
	if all[2].ContextMap()["user"] != int64(7) {
		t.Fatalf("WithFields entry = %+v", all[2].ContextMap())
	}
}

func TestParseLevel(t *testing.T) {
	cases := map[string]string{"debug": "debug", "WARNING": "warn", "error": "error", "fatal": "fatal", "": "info", "nonsense": "info"}
	for in, want := range cases {
		if got := parseLevel(in).String(); got != want {
			t.Errorf("parseLevel(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestConfigureFileChannelJSON(t *testing.T) {
	prev := Log
	t.Cleanup(func() { Log = prev })
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "audit.log")
	err := Configure(Config{Level: "debug", Format: "json", Channels: map[string]ChannelConfig{
		"audit": {Driver: "file", Path: path},
	}})
	if err != nil {
		t.Fatal(err)
	}
	Channel("audit").Infow("signed in", "user", "ana")
	Sync()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var line map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &line); err != nil {
		t.Fatalf("channel output is not JSON: %q", data)
	}
	if line["msg"] != "signed in" || line["channel"] != "audit" || line["user"] != "ana" {
		t.Fatalf("line = %v", line)
	}
}

func TestConfigureFileChannelWithoutDirectory(t *testing.T) {
	prev := Log
	t.Cleanup(func() { Log = prev })
	wd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(wd) })
	_ = os.Chdir(t.TempDir())
	// Used to panic slicing the path at index -1.
	if err := Configure(Config{Channels: map[string]ChannelConfig{"x": {Driver: "file", Path: "app.log"}}}); err != nil {
		t.Fatal(err)
	}
	if err := Configure(Config{Channels: map[string]ChannelConfig{"y": {Driver: "file"}}}); err == nil {
		t.Fatal("file channel without path should fail")
	}
}

func TestChannelFallsBackToGlobal(t *testing.T) {
	logs := observe(t)
	Channel("never-configured").Info("hello")
	if logs.Len() != 1 || logs.All()[0].LoggerName != "never-configured" {
		t.Fatalf("fallback channel entries = %+v", logs.All())
	}
}

func TestForRequestCarriesRequestID(t *testing.T) {
	logs := observe(t)
	c := nhttp.New(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil), nil)
	c.Set("request_id", "req-123")
	l := ForRequest(c)
	l.Info("handled")
	if ForRequest(c) != l {
		t.Fatal("ForRequest should reuse the scoped logger")
	}
	if got := logs.All()[0].ContextMap()["request_id"]; got != "req-123" {
		t.Fatalf("request_id = %v", got)
	}
	WithContext(c, Log.With("user_id", 9))
	ForRequest(c).Info("again")
	if got := logs.All()[1].ContextMap()["user_id"]; got != int64(9) {
		t.Fatalf("WithContext logger not used: %v", logs.All()[1].ContextMap())
	}
}

func TestRotatingWriterRotatesAndPrunes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	unrelated := filepath.Join(dir, "application.log")
	_ = os.WriteFile(unrelated, []byte("keep me"), 0o644)

	w, err := NewRotatingWriter(RotationConfig{Path: path, MaxSizeMB: 1, MaxBackups: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	chunk := []byte(strings.Repeat("x", 600*1024))
	for i := 0; i < 5; i++ {
		if _, err := w.Write(chunk); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	entries, _ := os.ReadDir(dir)
	var backups []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "app-") {
			backups = append(backups, e.Name())
		}
	}
	if len(backups) != 2 {
		t.Fatalf("backups = %v, want 2 kept (and none overwritten)", backups)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatal("rotation deleted an unrelated file with the same prefix")
	}
	info, _ := os.Stat(path)
	if info.Size() > 1024*1024 {
		t.Fatalf("current file exceeded max size: %d", info.Size())
	}
}

func TestTeeCore(t *testing.T) {
	prev := Log
	t.Cleanup(func() { Log = prev })
	base, baseLogs := observer.New(zap.InfoLevel)
	Set(zap.New(base))
	extra, extraLogs := observer.New(zap.InfoLevel)
	if err := TeeCore(extra); err != nil {
		t.Fatal(err)
	}
	Info("both")
	if baseLogs.Len() != 1 || extraLogs.Len() != 1 {
		t.Fatalf("tee: base=%d extra=%d", baseLogs.Len(), extraLogs.Len())
	}
}
