package telescope

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// A WebSocket upgrade behind the request watcher needs the recorder to be
// a Hijacker; without it every upgrade fails with a 500.
func TestRecorderHijacks(t *testing.T) {
	hijacked := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &responseRecorder{ResponseWriter: w, status: 200}
		h, ok := any(rec).(http.Hijacker)
		if !ok {
			t.Error("recorder is not a Hijacker")
			return
		}
		conn, _, err := h.Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		hijacked = true
		_, _ = conn.Write([]byte("HTTP/1.1 204 No Content\r\n\r\n"))
		conn.Close()
	}))
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !hijacked || resp.StatusCode != http.StatusNoContent {
		t.Errorf("hijacked=%v status=%d", hijacked, resp.StatusCode)
	}
}
