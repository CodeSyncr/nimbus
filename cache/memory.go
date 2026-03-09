package cache

import (
	"strings"
	"sync"
	"time"
)

type item struct {
	v   any
	exp time.Time
}

// MemoryStore is an in-memory cache (driver: memory).
// Single-process only; not shared across instances.
type MemoryStore struct {
	mu   sync.RWMutex
	data map[string]item
}

// NewMemoryStore returns a new in-memory cache.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{data: make(map[string]item)}
}

// Set stores a value. Zero TTL = no expiry.
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

// InvalidatePrefix deletes all keys with the given prefix.
func (m *MemoryStore) InvalidatePrefix(prefix string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k := range m.data {
		if strings.HasPrefix(k, prefix) {
			delete(m.data, k)
		}
	}
	return nil
}

var _ Store = (*MemoryStore)(nil)
var _ PrefixInvalidator = (*MemoryStore)(nil)
