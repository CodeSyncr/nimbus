package middleware

import (
	"strconv"
	"strings"
	"time"

	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/redis"
	"github.com/CodeSyncr/nimbus/router"
)

// RateLimitRedis returns middleware that rate-limits using Redis (suitable for multi-instance).
// keyFn extracts a key from the request (e.g. IP). Limit is requests per window.
// FailOpen controls behavior on Redis errors: true allows requests through,
// false (default) returns 503 Service Unavailable.
func RateLimitRedis(rdb *redis.Client, limit int, window time.Duration, keyFn func(*http.Request) string, failOpen ...bool) router.Middleware {
	open := false
	if len(failOpen) > 0 {
		open = failOpen[0]
	}
	keyPrefix := "rl:"
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *http.Context) error {
			key := keyFn(c.Request)
			if key == "" {
				key = c.Request.RemoteAddr
			}
			rkey := keyPrefix + key
			ctx := c.Request.Context()
			pipe := rdb.Pipeline()
			incr := pipe.Incr(ctx, rkey)
			pipe.Expire(ctx, rkey, window)
			if _, err := pipe.Exec(ctx); err != nil {
				if open {
					return next(c)
				}
				c.Response.Header().Set("Retry-After", "5")
				return c.JSON(http.StatusServiceUnavailable, map[string]string{
					"error": "service temporarily unavailable",
				})
			}
			count := incr.Val()
			remaining := int64(limit) - count
			if remaining < 0 {
				remaining = 0
			}
			c.Response.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
			c.Response.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(remaining, 10))

			if count > int64(limit) {
				// Set Retry-After header only on 429
				if ttl, err := rdb.TTL(ctx, rkey).Result(); err == nil && ttl > 0 {
					c.Response.Header().Set("Retry-After", strconv.Itoa(int(ttl.Seconds())))
				}
				c.JSON(http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded"})
				return nil
			}
			return next(c)
		}
	}
}

// DefaultKeyFn returns the client IP for rate limiting.
func DefaultKeyFn(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	return r.RemoteAddr
}
