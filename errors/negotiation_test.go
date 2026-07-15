package errors_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CodeSyncr/nimbus/errors"
	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

// buildRouter wires the router the way the generated kernel does: the global
// errors.Handler plus a NotFound fallback, so 404s and returned errors flow
// through content negotiation.
func buildRouter() *router.Router {
	r := router.New()
	r.Use(errors.Handler())
	r.Fallback(errors.NotFoundHandler())
	r.Get("/boom", func(c *nhttp.Context) error {
		return errors.HTTPError{Status: nhttp.StatusBadGateway, Message: "upstream down"}
	})
	return r
}

func request(r *router.Router, path, accept string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// TestZeroConfigFallbackStatus guards the router safety net: an app that wires
// only the NotFound fallback (no errors.Handler middleware) must still return
// the correct 404 status, not a collapsed 500.
func TestZeroConfigFallbackStatus(t *testing.T) {
	r := router.New()
	r.Fallback(errors.NotFoundHandler()) // deliberately no errors.Handler()
	rec := request(r, "/missing", "text/html")
	if rec.Code != 404 {
		t.Fatalf("zero-config 404: got status %d, want 404", rec.Code)
	}
}

func TestErrorContentNegotiation(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		accept       string
		wantStatus   int
		wantHTML     bool
		bodyContains string
	}{
		{"404 browser -> HTML", "/nope", "text/html", 404, true, "could not be found"},
		{"404 API -> JSON", "/nope", "application/json", 404, false, `"error"`},
		{"502 browser -> HTML", "/boom", "text/html", 502, true, "upstream down"},
		{"502 API -> JSON", "/boom", "application/json", 502, false, "upstream down"},
		{"no Accept defaults to HTML", "/nope", "", 404, true, "<!DOCTYPE html>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := request(buildRouter(), tt.path, tt.accept)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status: got %d, want %d", rec.Code, tt.wantStatus)
			}
			ct := rec.Header().Get("Content-Type")
			isHTML := strings.Contains(ct, "text/html")
			if isHTML != tt.wantHTML {
				t.Fatalf("content-type %q: got html=%v, want html=%v", ct, isHTML, tt.wantHTML)
			}
			if !strings.Contains(rec.Body.String(), tt.bodyContains) {
				t.Fatalf("body missing %q; got: %s", tt.bodyContains, rec.Body.String())
			}
		})
	}
}
