package passport

import (
	"context"
	"strings"

	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/plugins/passport/models"
	"github.com/CodeSyncr/nimbus/router"
)

type tokenCtxKey struct{}

// WithAccessToken stores the validated access token on a context.
func WithAccessToken(ctx context.Context, at *models.OAuthAccessToken) context.Context {
	return context.WithValue(ctx, tokenCtxKey{}, at)
}

// AccessTokenFrom returns the validated access token from a context, or nil.
func AccessTokenFrom(ctx context.Context) *models.OAuthAccessToken {
	t, _ := ctx.Value(tokenCtxKey{}).(*models.OAuthAccessToken)
	return t
}

// RequireAccessToken returns middleware that authenticates the request via an
// OAuth2 Bearer access token and stores the token record on the context.
// Returns 401 with a WWW-Authenticate header when the token is missing or invalid.
func RequireAccessToken(s *Server) router.Middleware {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *nhttp.Context) error {
			plain := bearer(c)
			if plain == "" {
				return unauthorized(c, "missing bearer token")
			}
			at, err := s.ValidateAccessToken(c.Ctx(), plain)
			if err != nil {
				return unauthorized(c, "invalid or expired token")
			}
			c.Request = c.Request.WithContext(WithAccessToken(c.Ctx(), at))
			return next(c)
		}
	}
}

// RequireScope returns middleware that enforces the current access token was
// granted a scope. Must be chained after RequireAccessToken. Returns 403 otherwise.
func RequireScope(scope string) router.Middleware {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *nhttp.Context) error {
			at := AccessTokenFrom(c.Ctx())
			if at == nil {
				return c.JSON(nhttp.StatusForbidden, map[string]string{"error": "no_token"})
			}
			if !at.HasScope(scope) {
				return c.JSON(nhttp.StatusForbidden, map[string]string{
					"error": "insufficient_scope", "scope": scope,
				})
			}
			return next(c)
		}
	}
}

func bearer(c *nhttp.Context) string {
	h := c.Request.Header.Get("Authorization")
	parts := strings.SplitN(h, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func unauthorized(c *nhttp.Context, desc string) error {
	c.Response.Header().Set("WWW-Authenticate", `Bearer error="invalid_token", error_description="`+desc+`"`)
	return c.JSON(nhttp.StatusUnauthorized, map[string]string{"error": "invalid_token", "error_description": desc})
}
