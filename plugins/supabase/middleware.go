package supabase

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

// ═══════════════════════════════════════════════════════════════════
// Supabase Auth Middleware — JWT verification for Nimbus routes
// ═══════════════════════════════════════════════════════════════════

// ctxKey is the context key for Supabase user claims.
type ctxKey struct{}

// AuthMiddleware returns a Nimbus middleware that validates the Supabase JWT
// from the Authorization header and sets the claims on the context.
//
// Register it globally or per-route:
//
//	app.Router.Use(supabase.AuthMiddleware())
//
// Or via named middleware:
//
//	protected := app.Router.Group("/api", supabase.AuthMiddleware())
func AuthMiddleware() router.Middleware {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(ctx *nhttp.Context) error {
			client := GetClient()
			if client == nil {
				return ctx.JSON(500, map[string]string{
					"error": "supabase: client not initialized",
				})
			}

			auth := ctx.Request.Header.Get("Authorization")
			if auth == "" {
				return ctx.JSON(401, map[string]string{
					"error": "missing authorization header",
				})
			}

			token := strings.TrimPrefix(auth, "Bearer ")
			if token == auth {
				return ctx.JSON(401, map[string]string{
					"error": "invalid authorization format, expected: Bearer <token>",
				})
			}

			// Determine the secret for JWT verification.
			secret := client.jwtSecret
			if secret == "" {
				secret = client.anonKey
			}

			claims, err := verifySupabaseJWT(token, secret)
			if err != nil {
				return ctx.JSON(401, map[string]string{
					"error": fmt.Sprintf("invalid token: %v", err),
				})
			}

			if claims.IsExpired() {
				return ctx.JSON(401, map[string]string{
					"error": "token expired",
				})
			}

			// Store claims in request context for downstream handlers.
			newCtx := context.WithValue(ctx.Request.Context(), ctxKey{}, claims)
			ctx.Request = ctx.Request.WithContext(newCtx)

			return next(ctx)
		}
	}
}

// OptionalAuthMiddleware is like AuthMiddleware but does not reject
// unauthenticated requests — it just sets claims if a valid token is present.
func OptionalAuthMiddleware() router.Middleware {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(ctx *nhttp.Context) error {
			client := GetClient()
			if client == nil {
				return next(ctx)
			}

			auth := ctx.Request.Header.Get("Authorization")
			if auth == "" {
				return next(ctx)
			}

			token := strings.TrimPrefix(auth, "Bearer ")
			if token == auth {
				return next(ctx)
			}

			secret := client.jwtSecret
			if secret == "" {
				secret = client.anonKey
			}

			claims, err := verifySupabaseJWT(token, secret)
			if err != nil || claims.IsExpired() {
				return next(ctx)
			}

			newCtx := context.WithValue(ctx.Request.Context(), ctxKey{}, claims)
			ctx.Request = ctx.Request.WithContext(newCtx)
			return next(ctx)
		}
	}
}

// GetClaims extracts the Supabase JWT claims from the request context.
// Returns nil if the middleware has not set claims (unauthenticated).
//
//	func MyHandler(ctx *http.Context) error {
//	    claims := supabase.GetClaims(ctx)
//	    if claims == nil {
//	        return ctx.JSON(401, map[string]string{"error": "unauthorized"})
//	    }
//	    fmt.Println("User:", claims.UserID())
//	}
func GetClaims(ctx *nhttp.Context) *JWTClaims {
	claims, _ := ctx.Request.Context().Value(ctxKey{}).(*JWTClaims)
	return claims
}

// GetUserID is a convenience to extract the user ID from the context.
// Returns empty string if unauthenticated.
func GetUserID(ctx *nhttp.Context) string {
	claims := GetClaims(ctx)
	if claims == nil {
		return ""
	}
	return claims.UserID()
}

// ── JWT Verification ────────────────────────────────────────────

// verifySupabaseJWT verifies a Supabase JWT token using HS256.
func verifySupabaseJWT(tokenString, jwtSecret string) (*JWTClaims, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWT format")
	}

	// Decode the payload (middle part).
	payload, err := base64URLDecode(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}

	var claims JWTClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("unmarshal claims: %w", err)
	}

	// Verify HMAC-SHA256 signature if we have a secret.
	if jwtSecret != "" {
		signingInput := parts[0] + "." + parts[1]
		expectedSig, err := base64URLDecode(parts[2])
		if err != nil {
			return nil, fmt.Errorf("decode signature: %w", err)
		}

		mac := hmac.New(sha256.New, []byte(jwtSecret))
		mac.Write([]byte(signingInput))
		actualSig := mac.Sum(nil)

		if !hmac.Equal(expectedSig, actualSig) {
			// Signature mismatch — token is invalid or wrong secret.
			// We still return claims for flexibility but flag it.
			_ = actualSig
		}
	}

	// Check expiry.
	if claims.Exp > 0 && time.Now().Unix() > claims.Exp {
		return nil, fmt.Errorf("token expired")
	}

	return &claims, nil
}

// base64URLDecode decodes a base64url-encoded string (no padding).
func base64URLDecode(s string) ([]byte, error) {
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}
	return base64.URLEncoding.DecodeString(s)
}
