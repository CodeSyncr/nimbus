// Package cachehook lets the database package use the app's cache without
// importing the cache package, and with it every cache driver (Redis,
// DynamoDB, Memcached and their SDKs). The cache package sets Remember when
// an app imports it; until then Remember is nil.
package cachehook

import "time"

// Remember is cache.Remember once the cache package is linked.
var Remember func(key string, ttl time.Duration, fn func() (any, error)) (any, error)
