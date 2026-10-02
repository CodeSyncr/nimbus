package cache

import (
	"container/list"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// DefaultMemoryMaxEntries caps NewMemoryStore. The least recently used
// entries are evicted past it.
const DefaultMemoryMaxEntries = 100_000

type item struct {
	key string
	v   any
	exp time.Time
}

// MemoryOptions configures a MemoryStore.
type MemoryOptions struct {
	// MaxEntries evicts the least recently used entries past this size.
	// Zero means DefaultMemoryMaxEntries; negative means unbounded.
	MaxEntries int
	// SweepInterval is how often expired entries are purged, even ones
	// nobody reads again. Zero means one minute.
	SweepInterval time.Duration
}

// MemoryStore is an in-memory LRU cache (driver: memory).
// Single-process only; not shared across instances.
type MemoryStore struct {
	mu            sync.Mutex
	data          map[string]*list.Element // of *item
	lru           *list.List               // front = most recently used
	maxEntries    int
	sweepInterval time.Duration
	lastSweep     time.Time
	group         singleflight.Group
}

// NewMemoryStore returns an in-memory cache capped at DefaultMemoryMaxEntries.
func NewMemoryStore() *MemoryStore {
	return NewMemoryStoreWith(MemoryOptions{})
}

// NewMemoryStoreWith returns an in-memory cache with the given options.
func NewMemoryStoreWith(opts MemoryOptions) *MemoryStore {
	if opts.MaxEntries == 0 {
		opts.MaxEntries = DefaultMemoryMaxEntries
	}
	if opts.SweepInterval <= 0 {
		opts.SweepInterval = time.Minute
	}
	return &MemoryStore{
		data:          make(map[string]*list.Element),
		lru:           list.New(),
		maxEntries:    opts.MaxEntries,
		sweepInterval: opts.SweepInterval,
		lastSweep:     time.Now(),
	}
}

// Set stores a value. Zero TTL = no expiry.
func (m *MemoryStore) Set(key string, value any, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	m.sweepLocked(now)
	exp := time.Time{}
	if ttl > 0 {
		exp = now.Add(ttl)
	}
	if el, ok := m.data[key]; ok {
		it := el.Value.(*item)
		it.v, it.exp = value, exp
		m.lru.MoveToFront(el)
		return nil
	}
	m.data[key] = m.lru.PushFront(&item{key: key, v: value, exp: exp})
	if m.maxEntries > 0 {
		for len(m.data) > m.maxEntries {
			m.removeLocked(m.lru.Back())
		}
	}
	return nil
}

// Get returns the value and true if found and not expired.
func (m *MemoryStore) Get(key string) (any, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	el, ok := m.data[key]
	if !ok {
		return nil, false
	}
	it := el.Value.(*item)
	if !it.exp.IsZero() && time.Now().After(it.exp) {
		m.removeLocked(el)
		return nil, false
	}
	m.lru.MoveToFront(el)
	return it.v, true
}

// Delete removes a key.
func (m *MemoryStore) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if el, ok := m.data[key]; ok {
		m.removeLocked(el)
	}
	return nil
}

// Len returns the number of stored entries, including expired ones not yet swept.
func (m *MemoryStore) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.data)
}

// Remember returns the cached value or calls fn, stores the result, and
// returns it. Concurrent misses for the same key share one call to fn.
func (m *MemoryStore) Remember(key string, ttl time.Duration, fn func() (any, error)) (any, error) {
	if v, ok := m.Get(key); ok {
		return v, nil
	}
	v, err, _ := m.group.Do(key, func() (any, error) {
		if v, ok := m.Get(key); ok {
			return v, nil
		}
		v, err := fn()
		if err != nil {
			return nil, err
		}
		_ = m.Set(key, v, ttl)
		return v, nil
	})
	return v, err
}

// InvalidatePrefix deletes all keys with the given prefix.
func (m *MemoryStore) InvalidatePrefix(prefix string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, el := range m.data {
		if strings.HasPrefix(k, prefix) {
			m.removeLocked(el)
		}
	}
	return nil
}

func (m *MemoryStore) removeLocked(el *list.Element) {
	if el == nil {
		return
	}
	m.lru.Remove(el)
	delete(m.data, el.Value.(*item).key)
}

// sweepLocked purges expired entries once per sweep interval. It runs on
// writes, so a store nobody writes to never grows and needs no goroutine.
func (m *MemoryStore) sweepLocked(now time.Time) {
	if now.Sub(m.lastSweep) < m.sweepInterval {
		return
	}
	m.lastSweep = now
	for _, el := range m.data {
		if it := el.Value.(*item); !it.exp.IsZero() && now.After(it.exp) {
			m.removeLocked(el)
		}
	}
}

var _ Store = (*MemoryStore)(nil)
var _ PrefixInvalidator = (*MemoryStore)(nil)
