package edge

import (
	"fmt"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Edge Cache
// ---------------------------------------------------------------------------

// Cache provides a simple in-memory key-value cache for edge functions.
type Cache struct {
	mu      sync.RWMutex
	data    map[string]cacheEntry
	maxSize int
}

type cacheEntry struct {
	value  []byte
	expiry time.Time
}

// NewCache creates a new edge cache.
func NewCache(maxSize int) *Cache {
	c := &Cache{
		data:    make(map[string]cacheEntry),
		maxSize: maxSize,
	}
	go c.cleanup()
	return c
}

// Get retrieves a value from cache.
func (c *Cache) Get(key string) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.data[key]
	if !ok || time.Now().After(entry.expiry) {
		return nil, false
	}
	return entry.value, true
}

// Set stores a value in cache with TTL.
func (c *Cache) Set(key string, value []byte, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Evict if at capacity.
	if len(c.data) >= c.maxSize {
		oldest := ""
		oldestTime := time.Now().Add(time.Hour)
		for k, v := range c.data {
			if v.expiry.Before(oldestTime) {
				oldest = k
				oldestTime = v.expiry
			}
		}
		if oldest != "" {
			delete(c.data, oldest)
		}
	}
	c.data[key] = cacheEntry{value: value, expiry: time.Now().Add(ttl)}
}

// Delete removes a value from cache.
func (c *Cache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, key)
}

func (c *Cache) cleanup() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		c.mu.Lock()
		now := time.Now()
		for k, v := range c.data {
			if now.After(v.expiry) {
				delete(c.data, k)
			}
		}
		c.mu.Unlock()
	}
}

// Cached creates a response that should be cached.
func Cached(resp *Response, ttl time.Duration) *Response {
	resp.SetHeader("Cache-Control", fmt.Sprintf("public, max-age=%d", int(ttl.Seconds())))
	resp.SetHeader("X-Edge-Cache", "HIT")
	resp.cached = true
	return resp
}
