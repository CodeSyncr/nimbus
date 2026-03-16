package commands

import (
	"os"
	"path/filepath"
)

// scaffoldAuth writes auth-related config and guard files based on the
// selected guard type: "session", "access_token", or "basic".
func scaffoldAuth(dir, appName, guard string) error {
	// Create config/auth.go
	_ = os.WriteFile(filepath.Join(dir, "config", "auth.go"), []byte(authConfigContent(guard)), 0644)

	// Create app/middleware/auth.go
	_ = os.MkdirAll(filepath.Join(dir, "app", "middleware"), 0755)
	_ = os.WriteFile(filepath.Join(dir, "app", "middleware", "auth.go"), []byte(authMiddlewareContent(appName, guard)), 0644)

	// Add env vars for the selected guard
	envPath := filepath.Join(dir, ".env.example")
	envVars := authEnvVars(guard)
	if len(envVars) > 0 {
		_ = appendEnvVars(envPath, envVars, appName)
	}

	return nil
}

func authConfigContent(guard string) string {
	switch guard {
	case "session":
		return `/*
|--------------------------------------------------------------------------
| Authentication Configuration
|--------------------------------------------------------------------------
|
| Guard: Session
|
| The session guard uses cookies to track logged-in users. After a
| successful login, the user's identifier is stored in the session and
| a session cookie is sent to the browser. Subsequent requests include
| this cookie, allowing the server to restore the user's authenticated
| state.
|
| Sessions and cookies have been the standard for web authentication
| for decades. They work well when your client can accept and send
| cookies — server-rendered apps and SPAs on the same top-level domain.
|
*/

package config

// AuthGuard defines the authentication guard type.
var AuthGuard = "session"
`

	case "access_token":
		return `/*
|--------------------------------------------------------------------------
| Authentication Configuration
|--------------------------------------------------------------------------
|
| Guard: Access Token
|
| Access tokens are cryptographically secure random strings issued to
| users after login. The client stores the token and includes it in
| the Authorization header of subsequent requests. Nimbus uses opaque
| access tokens (not JWTs) that are stored as hashes in your database.
|
| Use access tokens when your client cannot work with cookies:
|   - Native mobile applications
|   - Desktop applications
|   - Web applications on a different domain than your API
|   - Third-party integrations that need programmatic API access
|
| The client application is responsible for storing tokens securely.
|
*/

package config

// AuthGuard defines the authentication guard type.
var AuthGuard = "access_token"
`

	case "basic":
		return `/*
|--------------------------------------------------------------------------
| Authentication Configuration
|--------------------------------------------------------------------------
|
| Guard: Basic Auth
|
| The basic auth guard implements the HTTP authentication framework.
| The client sends credentials as a base64-encoded string in the
| Authorization header with each request. If credentials are invalid,
| the browser displays a native login prompt.
|
| Basic authentication is NOT recommended for production applications
| because credentials are sent with every request and the user
| experience is limited to the browser's built-in prompt. However, it
| can be useful during early development or for internal tools.
|
*/

package config

// AuthGuard defines the authentication guard type.
var AuthGuard = "basic"

// BasicAuthRealm is the realm name shown in the browser's login dialog.
var BasicAuthRealm = "Restricted"
`

	default:
		return `package config

var AuthGuard = "none"
`
	}
}

func authMiddlewareContent(appName, guard string) string {
	switch guard {
	case "session":
		return `/*
|--------------------------------------------------------------------------
| Auth Middleware — Session Guard
|--------------------------------------------------------------------------
|
| This middleware uses session-based authentication. It reads the user
| ID from the session cookie and loads the user from the database.
|
| Usage in start/routes.go:
|
|   app.Router.Group(func(r *router.Router) {
|       r.Use(authmw.Authenticate())
|       r.Get("/dashboard", dashboardHandler)
|   })
|
*/

package middleware

import (
	"github.com/CodeSyncr/nimbus/auth"
	"github.com/CodeSyncr/nimbus/router"
)

// Guard is the session-based auth guard. Set the UserLoader during boot
// to load users from your database.
//
//   middleware.Guard = auth.NewSessionGuardWithLoader(
//       auth.UserLoaderFunc(func(ctx context.Context, id string) (auth.User, error) {
//           var user models.User
//           err := db.First(&user, id).Error
//           return &user, err
//       }),
//   )
var Guard *auth.SessionGuard

func init() {
	Guard = auth.NewSessionGuard()
}

// Authenticate returns middleware that requires a valid session.
// Unauthenticated requests are redirected to /login.
func Authenticate() router.Middleware {
	return auth.RequireAuth(Guard, "/login")
}
`

	case "access_token":
		return `/*
|--------------------------------------------------------------------------
| Auth Middleware — Access Token Guard
|--------------------------------------------------------------------------
|
| This middleware uses opaque access tokens (Bearer tokens). The client
| sends the token in the Authorization header:
|
|   Authorization: Bearer <token>
|
| Tokens are SHA-256 hashed and verified against the personal_access_tokens
| table. This is ideal for APIs, mobile apps, and third-party integrations.
|
| Usage in start/routes.go:
|
|   app.Router.Group(func(r *router.Router) {
|       r.Use(authmw.Authenticate())
|       r.Get("/api/me", meHandler)
|   })
|
*/

package middleware

import (
	"github.com/CodeSyncr/nimbus/auth"
	"github.com/CodeSyncr/nimbus/router"
)

// TokenGuard is the access-token-based auth guard. Set the DB and
// UserLoader during boot.
//
//   middleware.TokenGuard = auth.NewTokenGuard(db, userLoader)
var TokenGuard *auth.TokenGuard

// Authenticate returns middleware that requires a valid Bearer token.
// Returns 401 Unauthorized if the token is missing or invalid.
func Authenticate() router.Middleware {
	return auth.RequireToken(TokenGuard)
}

// RequireAbility returns middleware that checks a specific token ability.
//
//   r.Use(authmw.RequireAbility("posts:write"))
func RequireAbility(ability string) router.Middleware {
	return auth.RequireAbility(ability)
}
`

	case "basic":
		return `/*
|--------------------------------------------------------------------------
| Auth Middleware — Basic Auth Guard
|--------------------------------------------------------------------------
|
| This middleware uses HTTP Basic authentication. The browser will show
| a native login prompt when credentials are required.
|
| NOT recommended for production — credentials are sent with every
| request. Useful for development and internal tools.
|
| Usage in start/routes.go:
|
|   app.Router.Group(func(r *router.Router) {
|       r.Use(authmw.Authenticate())
|       r.Get("/admin", adminHandler)
|   })
|
*/

package middleware

import (
	"context"

	"github.com/CodeSyncr/nimbus/auth"
	"github.com/CodeSyncr/nimbus/router"
)

// BasicGuard is the HTTP basic auth guard. Set the validator during boot.
//
//   middleware.BasicGuard = auth.NewBasicAuthGuard("Restricted",
//       func(ctx context.Context, user, pass string) (auth.User, error) {
//           // verify against database
//           return myUser, nil
//       },
//   )
var BasicGuard *auth.BasicAuthGuard

func init() {
	BasicGuard = auth.NewBasicAuthGuard("Restricted", func(ctx context.Context, user, pass string) (auth.User, error) {
		// TODO: Replace with your user verification logic
		return nil, nil
	})
}

// Authenticate returns middleware that requires valid HTTP basic credentials.
// Returns 401 with WWW-Authenticate header if credentials are invalid.
func Authenticate() router.Middleware {
	return auth.RequireBasicAuth(BasicGuard)
}
`

	default:
		return `package middleware

import "github.com/CodeSyncr/nimbus/router"

func Authenticate() router.Middleware {
	return func(next router.HandlerFunc) router.HandlerFunc { return next }
}
`
	}
}

func authEnvVars(guard string) []string {
	switch guard {
	case "session":
		return []string{
			"SESSION_DRIVER=cookie",
			"SESSION_SECRET=please-change-this-secret",
		}
	case "access_token":
		return []string{
			"TOKEN_EXPIRY=24h",
		}
	case "basic":
		return []string{
			"BASIC_AUTH_REALM=Restricted",
		}
	default:
		return nil
	}
}
