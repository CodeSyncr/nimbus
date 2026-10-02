package middleware

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/redis"
	"github.com/CodeSyncr/nimbus/router"
	"github.com/alicebob/miniredis/v2"
)

func hit(r *router.Router) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/test", nil))
	return rec
}

func TestRateLimit_SetsRetryAfterAndRemaining(t *testing.T) {
	r := router.New()
	r.Use(RateLimit(2, 30*time.Second, func(*nhttp.Request) string { return "k" }))
	r.Get("/test", okHandler())

	if rec := hit(r); rec.Header().Get("X-RateLimit-Remaining") != "1" {
		t.Fatalf("remaining = %q, want 1", rec.Header().Get("X-RateLimit-Remaining"))
	}
	hit(r)
	rec := hit(r)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
	ra, err := strconv.Atoi(rec.Header().Get("Retry-After"))
	if err != nil || ra < 1 || ra > 30 {
		t.Fatalf("Retry-After = %q, want 1..30 seconds", rec.Header().Get("Retry-After"))
	}
}

func TestRateLimit_ForgetsExpiredKeys(t *testing.T) {
	rl := &rateLimiter{counts: map[string]*rateEntry{}, limit: 5, window: 20 * time.Millisecond, lastSweep: time.Now()}
	for i := 0; i < 1000; i++ {
		rl.allow("ip-" + strconv.Itoa(i))
	}
	if rl.size() != 1000 {
		t.Fatalf("size = %d", rl.size())
	}
	time.Sleep(30 * time.Millisecond)
	rl.allow("new-ip")
	if n := rl.size(); n != 1 {
		t.Fatalf("expired keys were not swept: %d keys left", n)
	}
}

func TestRateLimit_WindowResets(t *testing.T) {
	r := router.New()
	r.Use(RateLimit(1, 50*time.Millisecond, func(*nhttp.Request) string { return "k" }))
	r.Get("/test", okHandler())
	hit(r)
	if hit(r).Code != http.StatusTooManyRequests {
		t.Fatal("second request should be limited")
	}
	time.Sleep(60 * time.Millisecond)
	if rec := hit(r); rec.Code != http.StatusOK {
		t.Fatalf("after the window, got %d", rec.Code)
	}
}

func TestRateLimitRedis_WindowIsNotExtendedByBlockedRequests(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	r := router.New()
	r.Use(RateLimitRedis(rdb, 1, 10*time.Second, func(*nhttp.Request) string { return "k" }))
	r.Get("/test", okHandler())

	hit(r)
	mr.FastForward(6 * time.Second)
	rec := hit(r) // blocked; must not push the reset out
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
	if ra := rec.Header().Get("Retry-After"); ra != "4" {
		t.Fatalf("Retry-After = %q, want 4", ra)
	}
	mr.FastForward(5 * time.Second)
	if rec := hit(r); rec.Code != http.StatusOK {
		t.Fatalf("window should have reset, got %d", rec.Code)
	}
}
