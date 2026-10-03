package middleware

import (
	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientIPIgnoresPortsAndUntrustedHeaders(t *testing.T) {
	for _, addr := range []string{"192.0.2.1:1000", "192.0.2.1:2000"} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = addr
		r.Header.Set("X-Forwarded-For", "evil")
		if got := DefaultKeyFn(r); got != "192.0.2.1" {
			t.Fatalf("IP = %s", got)
		}
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "2001:db8::1"
	if got := ClientIP(r); got != "2001:db8::1" {
		t.Fatal(got)
	}
}
func TestTrustedClientIPChain(t *testing.T) {
	for _, tc := range []struct{ peer, xff, want string }{
		{"10.0.0.1:90", "198.51.100.9, 192.0.2.1", "192.0.2.1"},
		{"10.0.0.1:90", "192.0.2.1, 10.0.0.2", "192.0.2.1"},
		{"192.0.2.1:90", "198.51.100.9", "192.0.2.1"},
		{"10.0.0.1:90", "192.0.2.1, invalid", "10.0.0.1"},
	} {
		r := router.New()
		r.Use(TrustedProxies("10.0.0.0/8"))
		r.Get("/", func(c *nhttp.Context) error {
			if got := ClientIP(c.Request); got != tc.want {
				t.Errorf("got %s want %s", got, tc.want)
			}
			return nil
		})
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = tc.peer
		req.Header.Set("X-Forwarded-For", tc.xff)
		r.ServeHTTP(httptest.NewRecorder(), req)
	}
}
func TestRateLimitCannotBeResetByNewConnection(t *testing.T) {
	r := router.New()
	r.Use(RateLimit(1, time.Minute, nil))
	r.Get("/", func(c *nhttp.Context) error { return c.JSON(200, "ok") })
	for i, addr := range []string{"192.0.2.1:1000", "192.0.2.1:2000"} {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = addr
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if i == 1 && w.Code != 429 {
			t.Fatalf("new connection bypassed limit: %d", w.Code)
		}
	}
}
