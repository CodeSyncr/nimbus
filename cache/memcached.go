package cache

import (
	"encoding/json"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
)

const memcachedPrefix = "nimbus:cache:"

// MemcachedStore uses Memcached for distributed caching.
type MemcachedStore struct {
	client *memcache.Client
	prefix string
}

// NewMemcachedStore creates a Memcached cache store.
// servers: comma-separated list like "localhost:11211" or "10.0.0.1:11211,10.0.0.2:11211"
func NewMemcachedStore(servers ...string) *MemcachedStore {
	client := memcache.New(servers...)
	return &MemcachedStore{client: client, prefix: memcachedPrefix}
}

// NewMemcachedStoreWithPrefix creates a Memcached store with a custom key prefix.
func NewMemcachedStoreWithPrefix(servers []string, prefix string) *MemcachedStore {
	client := memcache.New(servers...)
	return &MemcachedStore{client: client, prefix: prefix}
}

// Set stores a value. Values are JSON-serialized. Memcached key limit is 250 bytes.
func (m *MemcachedStore) Set(key string, value any, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	k := m.prefix + key
	if len(k) > 250 {
		k = k[:250] // Memcached key limit
	}
	exp := int32(0)
	if ttl > 0 {
		exp = int32(ttl.Seconds())
		if exp <= 0 {
			exp = 1
		}
	}
	return m.client.Set(&memcache.Item{Key: k, Value: data, Expiration: exp})
}

// Get returns the value and true if found.
func (m *MemcachedStore) Get(key string) (any, bool) {
	k := m.prefix + key
	if len(k) > 250 {
		k = k[:250]
	}
	it, err := m.client.Get(k)
	if err == memcache.ErrCacheMiss {
		return nil, false
	}
	if err != nil {
		return nil, false
	}
	var v any
	if err := json.Unmarshal(it.Value, &v); err != nil {
		return nil, false
	}
	return v, true
}

// Delete removes a key.
func (m *MemcachedStore) Delete(key string) error {
	k := m.prefix + key
	if len(k) > 250 {
		k = k[:250]
	}
	return m.client.Delete(k)
}

// Remember returns the cached value or calls fn, stores the result, and returns it.
func (m *MemcachedStore) Remember(key string, ttl time.Duration, fn func() (any, error)) (any, error) {
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

var _ Store = (*MemcachedStore)(nil)
