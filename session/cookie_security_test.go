package session

import (
	"context"
	"testing"
	"time"
)

func TestCookieExpiryRevocationAndReplacement(t *testing.T) {
	ctx := context.Background()
	registry := NewMemoryStore()
	key := []byte("01234567890123456789012345678901")
	s := NewCookieStoreWithRegistry(key, registry)
	peer := NewCookieStoreWithRegistry(key, registry)
	expired, err := s.encrypt(cookiePayload{Version: 1, ExpiresAt: time.Now().Add(-time.Second).UnixNano(), Data: map[string]any{"user_id": "u"}})
	if err != nil {
		t.Fatal(err)
	}
	registry.Set(ctx, cookieFingerprint(expired), map[string]any{"active": true}, time.Hour)
	if got, _ := s.Get(ctx, expired); got != nil {
		t.Fatal("expired cookie accepted")
	}
	id, err := s.Set(ctx, "", map[string]any{"user_id": "u"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := peer.Get(ctx, id); err != nil || got["user_id"] != "u" {
		t.Fatalf("shared cookie: %v %v", got, err)
	}
	replacement, err := peer.Set(ctx, id, map[string]any{}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(ctx, id); got != nil {
		t.Fatal("old cookie survived replacement/logout")
	}
	if err := s.Destroy(ctx, replacement); err != nil {
		t.Fatal(err)
	}
	if got, _ := peer.Get(ctx, replacement); got != nil {
		t.Fatal("revoked cookie accepted")
	}
	legacy, err := s.encrypt(map[string]any{"user_id": "u"})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(ctx, legacy); got != nil {
		t.Fatal("legacy unbounded cookie accepted")
	}
}
