package auth

import (
	"strings"

	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

// RequireStatelessToken returns middleware that reads the Authorization: Bearer <token>
// header, authenticates the request via the StatelessGuard, and stores the
// user on the request context.
func RequireStatelessToken(guard *StatelessGuard) router.Middleware {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *http.Context) error {
			authHeader := c.Request.Header.Get("Authorization")
			if authHeader == "" {
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"error": "Missing Authorization header",
				})
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"error": "Invalid Authorization header format. Expected: Bearer <token>",
				})
			}

			plainToken := strings.TrimSpace(parts[1])
			if plainToken == "" {
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"error": "Empty bearer token",
				})
			}

			// Put the token on context so StatelessGuard.User() can find it.
			ctx := WithBearerToken(c.Request.Context(), plainToken)
			c.Request = c.Request.WithContext(ctx)

			user, err := guard.User(c.Request.Context())
			if err != nil {
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"error": "Invalid or expired token",
				})
			}
			if user == nil {
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"error": "User not found",
				})
			}

			// Set the authenticated user on context.
			ctx = WithUser(c.Request.Context(), user)
			c.Request = c.Request.WithContext(ctx)

			return next(c)
		}
	}
}
