package auth

import (
	"context"
	"encoding/base64"
	"strings"

	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

// ── Basic Auth Guard ────────────────────────────────────────────

// BasicAuthValidator validates username/password and returns the
// authenticated user or nil if invalid.
type BasicAuthValidator func(ctx context.Context, username, password string) (User, error)

// BasicAuthGuard implements the HTTP Basic authentication framework.
// The client sends credentials as a base64-encoded string in the
// Authorization header with each request.
//
// Basic authentication is not recommended for production applications
// because credentials are sent with every request and the user
// experience is limited to the browser's built-in prompt. However, it
// can be useful during early development or for internal tools.
type BasicAuthGuard struct {
	Realm    string
	Validate BasicAuthValidator
}

// NewBasicAuthGuard creates a new basic auth guard.
//
//	guard := auth.NewBasicAuthGuard("Restricted", func(ctx context.Context, user, pass string) (auth.User, error) {
//	    return myUserLoader(ctx, user, pass)
//	})
func NewBasicAuthGuard(realm string, validate BasicAuthValidator) *BasicAuthGuard {
	if realm == "" {
		realm = "Restricted"
	}
	return &BasicAuthGuard{
		Realm:    realm,
		Validate: validate,
	}
}

// User extracts credentials from the Authorization header and validates them.
func (g *BasicAuthGuard) User(ctx context.Context) (User, error) {
	// The basic auth header is not stored in a standard context key.
	// The middleware below injects the user. This method returns nil
	// if no valid header is present.
	return UserFromContext(ctx), nil
}

// Login is a no-op for basic auth (credentials are sent per-request).
func (g *BasicAuthGuard) Login(_ context.Context, _ User) error {
	return nil
}

// Logout is a no-op for basic auth (no server-side session).
func (g *BasicAuthGuard) Logout(_ context.Context) error {
	return nil
}

// RequireBasicAuth returns middleware that enforces HTTP basic authentication.
// If credentials are invalid, a 401 response with WWW-Authenticate header
// is sent, prompting the browser to show its built-in login dialog.
func RequireBasicAuth(guard *BasicAuthGuard) router.Middleware {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *nhttp.Context) error {
			username, password, ok := parseBasicAuth(c.Request.Header.Get("Authorization"))
			if !ok {
				return sendBasicAuthChallenge(c, guard.Realm)
			}

			user, err := guard.Validate(c.Request.Context(), username, password)
			if err != nil || user == nil {
				return sendBasicAuthChallenge(c, guard.Realm)
			}

			// Store authenticated user in context.
			ctx := WithUser(c.Request.Context(), user)
			c.Request = c.Request.WithContext(ctx)
			return next(c)
		}
	}
}

// parseBasicAuth decodes the "Basic <base64>" Authorization header value.
func parseBasicAuth(header string) (username, password string, ok bool) {
	if header == "" {
		return "", "", false
	}
	const prefix = "Basic "
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", "", false
	}
	decoded, err := base64.StdEncoding.DecodeString(header[len(prefix):])
	if err != nil {
		return "", "", false
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func sendBasicAuthChallenge(c *nhttp.Context, realm string) error {
	c.Response.Header().Set("WWW-Authenticate", `Basic realm="`+realm+`"`)
	return c.JSON(nhttp.StatusUnauthorized, map[string]string{
		"error": "Unauthorized",
	})
}
