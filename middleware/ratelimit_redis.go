package middleware

import (
	"strconv"
	"strings"
	"time"

	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
	"github.com/redis/go-redis/v9"
)

// RateLimitRedis returns middleware that rate-limits using Redis (suitable for multi-instance).
// keyFn extracts a key from the request (e.g. IP). Limit is requests per window.
func RateLimitRedis(rdb *redis.Client, limit int, window time.Duration, keyFn func(*http.Request) string) router.Middleware {
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
				// Redis error - allow request
				return next(c)
			}
			count := incr.Val()
			if count > int64(limit) {
				c.JSON(http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded"})
				return nil
			}
			// Set Retry-After header
			if ttl, err := rdb.TTL(ctx, rkey).Result(); err == nil && ttl > 0 {
				c.Response.Header().Set("Retry-After", strconv.Itoa(int(ttl.Seconds())))
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
