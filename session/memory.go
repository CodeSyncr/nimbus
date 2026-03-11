package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// MemoryStore stores sessions in memory. Sessions are lost on restart.
// Use for development or single-instance apps.
type MemoryStore struct {
	mu       sync.RWMutex
	sessions map[string]sessionEntry
}

type sessionEntry struct {
	data   map[string]any
	expiry time.Time
}

// NewMemoryStore creates an in-memory session store.
func NewMemoryStore() *MemoryStore {
	s := &MemoryStore{sessions: make(map[string]sessionEntry)}
	go s.cleanup()
	return s
}

func (s *MemoryStore) cleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for id, e := range s.sessions {
			if e.expiry.Before(now) {
				delete(s.sessions, id)
			}
		}
		s.mu.Unlock()
	}
}

func (s *MemoryStore) Get(ctx context.Context, id string) (map[string]any, error) {
	if id == "" {
		return nil, nil
	}
	s.mu.RLock()
	e, ok := s.sessions[id]
	s.mu.RUnlock()
	if !ok || e.expiry.Before(time.Now()) {
		return nil, nil
	}
	// Return a copy so caller cannot mutate the stored data
	out := make(map[string]any, len(e.data))
	for k, v := range e.data {
		out[k] = v
	}
	return out, nil
}

func (s *MemoryStore) Set(ctx context.Context, id string, data map[string]any, maxAge time.Duration) (string, error) {
	if id == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		id = hex.EncodeToString(b)
	}
	entry := sessionEntry{
		data:   make(map[string]any, len(data)),
		expiry: time.Now().Add(maxAge),
	}
	for k, v := range data {
		entry.data[k] = v
	}
	s.mu.Lock()
	s.sessions[id] = entry
	s.mu.Unlock()
	return id, nil
}

func (s *MemoryStore) Destroy(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
	return nil
}
