package session

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

func TestMiddlewareExposesSession(t *testing.T) {
	t.Parallel()
	store := NewMemoryStore()
	r := router.New()
	r.Use(Middleware(Config{
		Store:    store,
		MaxAge:   time.Hour,
		HttpOnly: true,
	}))
	r.Get("/s", func(c *http.Context) error {
		s := FromContext(c.Request.Context())
		if s == nil {
			c.String(http.StatusInternalServerError, "no session")
			return nil
		}
		s.Set("uid", "42")
		c.String(http.StatusOK, "ok")
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/s", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}
