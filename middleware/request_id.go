package middleware

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

// RequestIDHeader is the header used for request IDs.
const RequestIDHeader = "X-Request-Id"

// RequestID generates a unique request ID for every request and makes it
// available via the X-Request-Id response header and the context store
// (key: "request_id"). If the incoming request already carries a
// X-Request-Id header, that value is reused (useful behind load balancers).
func RequestID() router.Middleware {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *http.Context) error {
			id := c.Request.Header.Get(RequestIDHeader)
			if id == "" {
				id = generateID()
			}
			c.Response.Header().Set(RequestIDHeader, id)
			c.Set("request_id", id)
			return next(c)
		}
	}
}

// GetRequestID returns the request ID from the context store, or "".
func GetRequestID(c *http.Context) string {
	if v, ok := c.Get("request_id"); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func generateID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
