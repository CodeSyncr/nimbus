package middleware

import (
	stdlib "net/http"

	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

// BodyLimit restricts the maximum size of the request body. If the body
// exceeds maxBytes, the server returns 413 Request Entity Too Large.
//
// Usage:
//
//	r.Use(middleware.BodyLimit(10 << 20)) // 10 MB
func BodyLimit(maxBytes int64) router.Middleware {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *http.Context) error {
			c.Request.Body = stdlib.MaxBytesReader(c.Response, c.Request.Body, maxBytes)
			return next(c)
		}
	}
}
