package router

import (
	"net/http/httptest"
	"testing"

	nhttp "github.com/CodeSyncr/nimbus/http"
)

func TestFallbackRunsGlobalMiddleware(t *testing.T) {
	t.Parallel()
	r := New()
	r.Fallback(func(c *nhttp.Context) error {
		c.Response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		c.Response.WriteHeader(nhttp.StatusNotFound)
		_, _ = c.Response.Write([]byte("nf"))
		return nil
	})
	r.Use(func(next HandlerFunc) HandlerFunc {
		return func(c *nhttp.Context) error {
			c.Response.Header().Set("X-Fallback-Mw", "1")
			return next(c)
		}
	})
	req := httptest.NewRequest(nhttp.MethodGet, "/no-such-path", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != nhttp.StatusNotFound {
		t.Fatalf("status %d", rec.Code)
	}
	if rec.Header().Get("X-Fallback-Mw") != "1" {
		t.Fatal("expected fallback to run after global middleware")
	}
	if rec.Body.String() != "nf" {
		t.Fatalf("body %q", rec.Body.String())
	}
}

func TestRouterNamedURL(t *testing.T) {
	t.Parallel()
	r := New()
	r.Get("/users/:id", func(c *nhttp.Context) error { return nil }).As("users.show")
	if u := r.URL("users.show", "id", "42"); u != "/users/42" {
		t.Fatalf("URL(users.show): got %q want /users/42", u)
	}
	if r.URL("missing.route") != "" {
		t.Fatal("expected empty URL for unknown name")
	}
}

func TestRouterGroupPrefix(t *testing.T) {
	t.Parallel()
	r := New()
	g := r.Group("/api")
	g.Get("/hello", func(c *nhttp.Context) error {
		c.String(nhttp.StatusOK, "ok")
		return nil
	})
	req := httptest.NewRequest(nhttp.MethodGet, "/api/hello", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != nhttp.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestRouterMethodAndRoutes(t *testing.T) {
	t.Parallel()
	r := New()
	rt := r.Post("/items", func(c *nhttp.Context) error { return nil }).As("items.store")
	if rt.Method() != nhttp.MethodPost {
		t.Fatalf("method %s", rt.Method())
	}
	if rt.Name() != "items.store" {
		t.Fatalf("name %q", rt.Name())
	}
	found := false
	for _, x := range r.Routes() {
		if x.Name() == "items.store" && x.Path() == "/items" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("route not in Routes()")
	}
}
