package middleware

import (
	"context"
	"time"

	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

// Timeout wraps each request with a context deadline. If the handler
// does not complete within the given duration, the request context is
// cancelled and the handler can detect it via c.Ctx().Err().
//
// Usage:
//
//	r.Use(middleware.Timeout(30 * time.Second))
func Timeout(d time.Duration) router.Middleware {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *http.Context) error {
			ctx, cancel := context.WithTimeout(c.Request.Context(), d)
			defer cancel()
			c.Request = c.Request.WithContext(ctx)
			return next(c)
		}
	}
}
