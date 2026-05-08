package session

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

func TestMemoryStore_SetGet(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	data := map[string]any{"user_id": "42"}
	id, err := store.Set(ctx, "", data, time.Hour)
	if err != nil {
		t.Fatalf("Set error: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty session ID")
	}
	got, err := store.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if got["user_id"] != "42" {
		t.Errorf("expected '42', got %v", got["user_id"])
	}
}

func TestMemoryStore_GetMissing(t *testing.T) {
	store := NewMemoryStore()
	got, _ := store.Get(context.Background(), "nonexistent")
	if got != nil {
		t.Error("expected nil for missing session")
	}
}

func TestMemoryStore_Expiration(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	id, _ := store.Set(ctx, "", map[string]any{"x": 1}, 50*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	got, _ := store.Get(ctx, id)
	if got != nil {
		t.Error("expected expired session")
	}
}

func TestMemoryStore_Destroy(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	id, _ := store.Set(ctx, "", map[string]any{"x": 1}, time.Hour)
	_ = store.Destroy(ctx, id)
	got, _ := store.Get(ctx, id)
	if got != nil {
		t.Error("expected destroyed session")
	}
}

func TestMemoryStore_DataIsolation(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	original := map[string]any{"key": "original"}
	id, _ := store.Set(ctx, "", original, time.Hour)
	original["key"] = "mutated"
	got, _ := store.Get(ctx, id)
	if got["key"] != "original" {
		t.Error("stored data should not be affected by external mutations")
	}
}

func TestMemoryStore_UniqueIDs(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	id1, _ := store.Set(ctx, "", map[string]any{}, time.Hour)
	id2, _ := store.Set(ctx, "", map[string]any{}, time.Hour)
	if id1 == id2 {
		t.Error("expected unique session IDs")
	}
}

func TestMiddleware_SessionPersistence(t *testing.T) {
	store := NewMemoryStore()
	r := router.New()
	r.Use(Middleware(Config{Store: store, MaxAge: time.Hour}))
	r.Get("/write", func(c *http.Context) error {
		s := FromContext(c.Request.Context())
		s.Set("color", "blue")
		c.String(http.StatusOK, "ok")
		return nil
	})
	r.Get("/read", func(c *http.Context) error {
		s := FromContext(c.Request.Context())
		color, _ := s.Get("color").(string)
		c.String(http.StatusOK, color)
		return nil
	})

	req1 := httptest.NewRequest(http.MethodGet, "/write", nil)
	rec1 := httptest.NewRecorder()
	r.ServeHTTP(rec1, req1)
	cookies := rec1.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected session cookie")
	}

	req2 := httptest.NewRequest(http.MethodGet, "/read", nil)
	req2.AddCookie(cookies[0])
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	if rec2.Body.String() != "blue" {
		t.Errorf("expected 'blue', got %q", rec2.Body.String())
	}
}
