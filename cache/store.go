package cache

import "time"

// Store is the cache interface. All backends implement it.
// Values are JSON-serialized for distributed stores (Redis, Memcached, DynamoDB).
type Store interface {
	Set(key string, value any, ttl time.Duration) error
	Get(key string) (any, bool)
	Delete(key string) error
	Remember(key string, ttl time.Duration, fn func() (any, error)) (any, error)
}
