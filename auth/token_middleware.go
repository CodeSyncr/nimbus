package auth

import (
	"strings"

	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

// RequireToken returns middleware that reads the Authorization: Bearer <token>
// header, authenticates the request via the TokenGuard, and stores both the
// user and the token record on the request context.
//
// If no valid token is found, it returns 401 Unauthorized.
func RequireToken(guard *TokenGuard) router.Middleware {
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

			// Put the plain token on context so TokenGuard.User() can find it.
			ctx := WithBearerToken(c.Request.Context(), plainToken)
			c.Request = c.Request.WithContext(ctx)

			user, err := guard.User(c.Request.Context())
			if err != nil {
				return err
			}
			if user == nil {
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"error": "Invalid or expired token",
				})
			}

			// Look up the token record to store on context for ability checks.
			hash := hashToken(plainToken)
			var pat PersonalAccessToken
			if err := guard.db.WithContext(c.Request.Context()).
				Where("token = ?", hash).
				First(&pat).Error; err == nil {
				ctx = WithTokenRecord(c.Request.Context(), &pat)
				c.Request = c.Request.WithContext(ctx)
			}

			// Set the authenticated user on context.
			ctx = WithUser(c.Request.Context(), user)
			c.Request = c.Request.WithContext(ctx)

			return next(c)
		}
	}
}

// RequireAbility returns middleware that checks if the current token has a
// specific ability (scope). Must be used after RequireToken.
//
// Example:
//
//	api.Use(auth.RequireToken(tokenGuard))
//	api.Use(auth.RequireAbility("read:projects"))
func RequireAbility(ability string) router.Middleware {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *http.Context) error {
			pat := CurrentToken(c.Request.Context())
			if pat == nil {
				return c.JSON(http.StatusForbidden, map[string]string{
					"error": "No token found in request context",
				})
			}
			if !pat.HasAbility(ability) {
				return c.JSON(http.StatusForbidden, map[string]string{
					"error": "Token does not have the required ability: " + ability,
				})
			}
			return next(c)
		}
	}
}

// OptionalToken is like RequireToken but does not return 401 when no token
// is present. It simply passes through. Useful for endpoints that work for
// both authenticated and unauthenticated users.
func OptionalToken(guard *TokenGuard) router.Middleware {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *http.Context) error {
			authHeader := c.Request.Header.Get("Authorization")
			if authHeader == "" {
				return next(c)
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				return next(c)
			}

			plainToken := strings.TrimSpace(parts[1])
			if plainToken == "" {
				return next(c)
			}

			ctx := WithBearerToken(c.Request.Context(), plainToken)
			c.Request = c.Request.WithContext(ctx)

			user, err := guard.User(c.Request.Context())
			if err != nil {
				return next(c) // Silently fail for optional auth
			}
			if user != nil {
				hash := hashToken(plainToken)
				var pat PersonalAccessToken
				if err := guard.db.WithContext(c.Request.Context()).
					Where("token = ?", hash).
					First(&pat).Error; err == nil {
					ctx = WithTokenRecord(c.Request.Context(), &pat)
					c.Request = c.Request.WithContext(ctx)
				}

				ctx = WithUser(c.Request.Context(), user)
				c.Request = c.Request.WithContext(ctx)
			}

			return next(c)
		}
	}
}
