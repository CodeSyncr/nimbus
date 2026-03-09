package cache

import (
	"encoding/json"
	"time"
)

// Set stores a value in the global cache. Uses default store if Boot was not called.
func Set(key string, value any, ttl time.Duration) error {
	s := GetGlobal()
	if s == nil {
		s = Default
	}
	return s.Set(key, value, ttl)
}

// Get returns a value from the global cache.
func Get(key string) (any, bool) {
	s := GetGlobal()
	if s == nil {
		s = Default
	}
	return s.Get(key)
}

// Delete removes a key from the global cache.
func Delete(key string) error {
	s := GetGlobal()
	if s == nil {
		s = Default
	}
	return s.Delete(key)
}

// Has returns true if the key exists in the cache.
func Has(key string) bool {
	_, ok := Get(key)
	return ok
}

// Missing returns true if the key does not exist in the cache.
func Missing(key string) bool {
	return !Has(key)
}

// Pull retrieves a value and immediately deletes it. Useful for one-time-use data (e.g. flash messages).
func Pull(key string) (any, bool) {
	v, ok := Get(key)
	if ok {
		_ = Delete(key)
	}
	return v, ok
}

// SetForever stores a value that never expires (TTL = 0 is treated as no expiry for memory store).
func SetForever(key string, value any) error {
	return Set(key, value, 0)
}

// Remember gets from cache or calls fn, stores the result, and returns it.
func Remember(key string, ttl time.Duration, fn func() (any, error)) (any, error) {
	s := GetGlobal()
	if s == nil {
		s = Default
	}
	return s.Remember(key, ttl, fn)
}

// RememberT is a type-safe Remember. The callback returns T; the value is JSON-serialized for distributed stores.
func RememberT[T any](key string, ttl time.Duration, fn func() (T, error)) (T, error) {
	var zero T
	v, err := Remember(key, ttl, func() (any, error) {
		return fn()
	})
	if err != nil {
		return zero, err
	}
	if t, ok := v.(T); ok {
		return t, nil
	}
	// For distributed stores, v may be map[string]any from JSON; try to unmarshal
	if data, err := json.Marshal(v); err == nil {
		var out T
		if err := json.Unmarshal(data, &out); err == nil {
			return out, nil
		}
	}
	return zero, nil
}

// Default is the fallback in-memory store when Boot was not called.
var Default Store = NewMemoryStore()
