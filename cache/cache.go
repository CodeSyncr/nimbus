package cache

import (
	"sync"
	"time"
)

// Store is the cache interface (plan: cache.Set, cache.Get, cache.Remember).
type Store interface {
	Set(key string, value any, ttl time.Duration) error
	Get(key string) (any, bool)
	Delete(key string) error
	Remember(key string, ttl time.Duration, fn func() (any, error)) (any, error)
}

type item struct {
	v   any
	exp time.Time
}

// MemoryStore is an in-memory cache (driver: memory).
type MemoryStore struct {
	mu   sync.RWMutex
	data map[string]item
}

// NewMemoryStore returns a new in-memory cache.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{data: make(map[string]item)}
}

// Set stores a value with optional TTL. Zero TTL = no expiry.
func (m *MemoryStore) Set(key string, value any, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	exp := time.Time{}
	if ttl > 0 {
		exp = time.Now().Add(ttl)
	}
	m.data[key] = item{v: value, exp: exp}
	return nil
}

// Get returns the value and true if found and not expired.
func (m *MemoryStore) Get(key string) (any, bool) {
	m.mu.RLock()
	it, ok := m.data[key]
	m.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if !it.exp.IsZero() && time.Now().After(it.exp) {
		m.Delete(key)
		return nil, false
	}
	return it.v, true
}

// Delete removes a key.
func (m *MemoryStore) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

// Remember returns the cached value or calls fn, stores the result, and returns it.
func (m *MemoryStore) Remember(key string, ttl time.Duration, fn func() (any, error)) (any, error) {
	if v, ok := m.Get(key); ok {
		return v, nil
	}
	v, err := fn()
	if err != nil {
		return nil, err
	}
	_ = m.Set(key, v, ttl)
	return v, nil
}

// Default is the default in-memory store (for apps that don't need Redis).
var Default Store = NewMemoryStore()
