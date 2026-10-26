package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"
)

// MemoryStore stores JSON session snapshots in memory. Sessions are lost on
// restart. Use for development or single-instance apps. No background goroutine
// is started: expired records are pruned on reads and periodically on writes.
type MemoryStore struct {
	mu        sync.RWMutex
	sessions  map[string]sessionEntry
	lastSweep time.Time
}

type sessionEntry struct {
	payload []byte // immutable after publication
	expiry  time.Time
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{sessions: make(map[string]sessionEntry), lastSweep: time.Now()}
}

func (s *MemoryStore) sweepLocked(now time.Time) {
	if now.Sub(s.lastSweep) < time.Minute {
		return
	}
	for id, e := range s.sessions {
		if !now.Before(e.expiry) {
			delete(s.sessions, id)
		}
	}
	s.lastSweep = now
}

func (s *MemoryStore) Get(ctx context.Context, id string) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if id == "" {
		return nil, nil
	}
	s.mu.RLock()
	e, ok := s.sessions[id]
	s.mu.RUnlock()
	if !ok {
		return nil, nil
	}
	if !time.Now().Before(e.expiry) {
		s.mu.Lock()
		if latest, ok := s.sessions[id]; ok && !time.Now().Before(latest.expiry) {
			delete(s.sessions, id)
		}
		s.mu.Unlock()
		return nil, nil
	}
	var out map[string]any
	if err := json.Unmarshal(e.payload, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *MemoryStore) Set(ctx context.Context, id string, data map[string]any, maxAge time.Duration) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if id == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		id = hex.EncodeToString(b)
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.sweepLocked(now)
	s.sessions[id] = sessionEntry{payload: payload, expiry: now.Add(maxAge)}
	return id, nil
}

func (s *MemoryStore) Update(ctx context.Context, id string, data map[string]any, maxAge time.Duration) (string, error) {
	if err := s.Replace(ctx, id, id, data, maxAge); err != nil {
		return "", err
	}
	return id, nil
}

func (s *MemoryStore) Replace(ctx context.Context, oldID, newID string, data map[string]any, maxAge time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.sweepLocked(now)
	e, ok := s.sessions[oldID]
	if !ok || !now.Before(e.expiry) {
		return ErrExpired
	}
	if newID == "" {
		return ErrExpired
	}
	s.sessions[newID] = sessionEntry{payload: payload, expiry: now.Add(maxAge)}
	if oldID != newID {
		delete(s.sessions, oldID)
	}
	return nil
}

func (s *MemoryStore) Destroy(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
	return nil
}

func (s *MemoryStore) Rotate(ctx context.Context, id string, data map[string]any, maxAge time.Duration) (string, error) {
	return rotateRecord(ctx, s, id, data, maxAge)
}
