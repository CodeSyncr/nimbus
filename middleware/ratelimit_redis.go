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
// rateLimitScript counts a request in a fixed window. The expiry is set
// only when the window opens; re-arming it on every request would keep a
// client that never stops hitting the limit blocked forever.
var rateLimitScript = redis.NewScript(`
local n = redis.call('INCR', KEYS[1])
local ttl = redis.call('PTTL', KEYS[1])
if ttl < 0 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
  ttl = tonumber(ARGV[1])
end
return {n, ttl}
`)

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
			res, err := rateLimitScript.Run(ctx, rdb, []string{rkey}, window.Milliseconds()).Int64Slice()
			if err != nil || len(res) != 2 {
				if open {
					return next(c)
				}
				c.Response.Header().Set("Retry-After", "5")
				return c.JSON(http.StatusServiceUnavailable, map[string]string{
					"error": "service temporarily unavailable",
				})
			}
			count, ttlMs := res[0], res[1]
			remaining := int64(limit) - count
			if remaining < 0 {
				remaining = 0
			}
			c.Response.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
			c.Response.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(remaining, 10))

			if count > int64(limit) {
				c.Response.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds(time.Duration(ttlMs)*time.Millisecond)))
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
