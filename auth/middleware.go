package auth

import (
	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

// RequireAuth returns middleware that loads the user from the guard and sets it on the request context.
// If no user and redirectTo is non-empty, redirects; else returns 401.
func RequireAuth(guard Guard, redirectTo string) router.Middleware {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *http.Context) error {
			req := c.Request
			user, err := guard.User(req.Context())
			if err != nil {
				return err
			}
			if user != nil {
				req = req.WithContext(WithUser(req.Context(), user))
				c.Request = req
				return next(c)
			}
			if redirectTo != "" {
				c.Redirect(http.StatusFound, redirectTo)
				return nil
			}
			c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return nil
		}
	}
}
