package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

// helper: creates a handler that writes "ok" with 200
func okHandler() router.HandlerFunc {
	return func(c *nhttp.Context) error {
		c.String(200, "ok")
		return nil
	}
}

// ── CORS ────────────────────────────────────────────────────────

func TestCORS_SetsHeaders(t *testing.T) {
	r := router.New()
	r.Use(CORS("https://example.com"))
	r.Get("/test", okHandler())

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	origin := rec.Header().Get("Access-Control-Allow-Origin")
	if origin != "https://example.com" {
		t.Errorf("expected origin 'https://example.com', got %q", origin)
	}
	methods := rec.Header().Get("Access-Control-Allow-Methods")
	if methods == "" {
		t.Error("expected Access-Control-Allow-Methods to be set")
	}
	headers := rec.Header().Get("Access-Control-Allow-Headers")
	if !strings.Contains(headers, "Authorization") {
		t.Error("expected Authorization in allowed headers")
	}
}

func TestCORS_WildcardOrigin(t *testing.T) {
	r := router.New()
	r.Use(CORS("*"))
	r.Get("/test", okHandler())

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	origin := rec.Header().Get("Access-Control-Allow-Origin")
	if origin != "*" {
		t.Errorf("expected origin '*', got %q", origin)
	}
}

// ── CSRF ────────────────────────────────────────────────────────

func TestCSRF_AllowsGET(t *testing.T) {
	store := NewMemoryCSRFStore()
	r := router.New()
	r.Use(CSRF(store))
	r.Get("/test", okHandler())

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("GET should pass CSRF, got %d", rec.Code)
	}
}

func TestCSRF_BlocksPOSTWithoutToken(t *testing.T) {
	store := NewMemoryCSRFStore()
	r := router.New()
	r.Use(CSRF(store))
	r.Post("/test", okHandler())

	req := httptest.NewRequest("POST", "/test", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("POST without CSRF token should be 403, got %d", rec.Code)
	}
}

func TestCSRF_AllowsPOSTWithValidToken(t *testing.T) {
	store := NewMemoryCSRFStore()
	token := store.Create()
	r := router.New()
	r.Use(CSRF(store))
	r.Post("/test", okHandler())

	req := httptest.NewRequest("POST", "/test", nil)
	req.Header.Set(CSRFHeader, token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("POST with valid CSRF token should be 200, got %d", rec.Code)
	}
}

// ── MemoryCSRFStore ─────────────────────────────────────────────

func TestMemoryCSRFStore_CreateAndValidate(t *testing.T) {
	store := NewMemoryCSRFStore()
	token := store.Create()
	if token == "" {
		t.Fatal("expected non-empty token")
	}
	if !store.Valid(context.Background(), token) {
		t.Error("created token should be valid")
	}
	if store.Valid(context.Background(), "bogus") {
		t.Error("bogus token should be invalid")
	}
}

func TestGenerateCSRFToken_Unique(t *testing.T) {
	t1 := GenerateCSRFToken()
	t2 := GenerateCSRFToken()
	if t1 == "" || t2 == "" {
		t.Fatal("tokens should be non-empty")
	}
	if t1 == t2 {
		t.Error("generated tokens should be unique")
	}
}

// ── RateLimit ───────────────────────────────────────────────────

func TestRateLimit_AllowsWithinLimit(t *testing.T) {
	r := router.New()
	r.Use(RateLimit(5, time.Minute, func(req *nhttp.Request) string {
		return "test-client"
	}))
	r.Get("/test", okHandler())

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("request %d should be allowed, got %d", i+1, rec.Code)
		}
	}
}

func TestRateLimit_BlocksAfterLimit(t *testing.T) {
	r := router.New()
	r.Use(RateLimit(2, time.Minute, func(req *nhttp.Request) string {
		return "test-client"
	}))
	r.Get("/test", okHandler())

	// Exhaust the limit
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
	}

	// 3rd request should be blocked
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", rec.Code)
	}
}

// ── SecureHeaders ───────────────────────────────────────────────

func TestSecureHeaders_SetsDefaults(t *testing.T) {
	cfg := DefaultSecureHeadersConfig()
	r := router.New()
	r.Use(SecureHeaders(cfg))
	r.Get("/test", okHandler())

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	checks := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":       "DENY",
		"X-XSS-Protection":      "1; mode=block",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
	}
	for header, expected := range checks {
		got := rec.Header().Get(header)
		if got != expected {
			t.Errorf("expected %s: %q, got %q", header, expected, got)
		}
	}
}

func TestSecureHeaders_HSTS(t *testing.T) {
	cfg := DefaultSecureHeadersConfig()
	r := router.New()
	r.Use(SecureHeaders(cfg))
	r.Get("/test", okHandler())

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	hsts := rec.Header().Get("Strict-Transport-Security")
	if !strings.Contains(hsts, "max-age") {
		t.Errorf("expected HSTS header with max-age, got %q", hsts)
	}
}

// ── Logger ──────────────────────────────────────────────────────

func TestLogger_DoesNotModifyResponse(t *testing.T) {
	r := router.New()
	r.Use(Logger())
	r.Get("/test", okHandler())

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

// ── Recover ─────────────────────────────────────────────────────

func TestRecover_CatchesPanic(t *testing.T) {
	r := router.New()
	r.Use(Recover())
	r.Get("/panic", func(c *nhttp.Context) error {
		panic("test panic")
	})

	req := httptest.NewRequest("GET", "/panic", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// Should not crash — Recover catches the panic
	if rec.Code == 0 {
		t.Error("expected a response code after panic recovery")
	}
}
