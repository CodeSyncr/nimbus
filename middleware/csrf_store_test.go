package middleware

import (
	"context"
	"testing"
	"time"
)

func TestMemoryCSRFStoreExpiresTokens(t *testing.T) {
	s := NewMemoryCSRFStore()
	s.TTL = 20 * time.Millisecond
	tok := s.Create()
	if !s.Valid(context.Background(), tok) {
		t.Fatal("fresh token rejected")
	}
	time.Sleep(30 * time.Millisecond)
	if s.Valid(context.Background(), tok) {
		t.Fatal("expired token accepted")
	}
	// Expired tokens are pruned as new ones are created.
	for i := 0; i < 100; i++ {
		s.Create()
	}
	time.Sleep(30 * time.Millisecond)
	s.Create()
	if n := s.Len(); n != 1 {
		t.Fatalf("expired tokens not pruned: %d left", n)
	}
}

func TestMemoryCSRFStoreIsBounded(t *testing.T) {
	s := NewMemoryCSRFStore()
	s.MaxTokens = 10
	first := s.Create()
	var last string
	for i := 0; i < 50; i++ {
		last = s.Create()
	}
	if n := s.Len(); n != 10 {
		t.Fatalf("Len = %d, want 10", n)
	}
	if s.Valid(context.Background(), first) {
		t.Fatal("oldest token should have been evicted")
	}
	if !s.Valid(context.Background(), last) {
		t.Fatal("newest token should be valid")
	}
}
