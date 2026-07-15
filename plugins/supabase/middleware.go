package supabase

import (
	"context"
	"errors"
	"fmt"
	"strings"

	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
	"github.com/golang-jwt/jwt/v5"
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

			// Require JWT secret for verification — anonKey is NOT a signing secret.
			secret := client.jwtSecret
			if secret == "" {
				return ctx.JSON(500, map[string]string{
					"error": "supabase: SUPABASE_JWT_SECRET is not configured",
				})
			}

			claims, err := verifySupabaseJWT(token, secret, client.url+"/auth/v1")
			if err != nil {
				return ctx.JSON(401, map[string]string{
					"error": fmt.Sprintf("invalid token: %v", err),
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
				// No JWT secret configured — can't verify, skip auth.
				return next(ctx)
			}

			claims, err := verifySupabaseJWT(token, secret, client.url+"/auth/v1")
			if err != nil {
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

// supabaseClaims is the internal claims type used during verification. It
// embeds jwt.RegisteredClaims so the library validates exp/aud/iss/nbf, while
// carrying the Supabase-specific fields we expose to handlers.
type supabaseClaims struct {
	Email        string         `json:"email"`
	Phone        string         `json:"phone,omitempty"`
	Role         string         `json:"role"`
	AppMetadata  map[string]any `json:"app_metadata,omitempty"`
	UserMetadata map[string]any `json:"user_metadata,omitempty"`
	jwt.RegisteredClaims
}

// verifySupabaseJWT verifies a Supabase access token. Security properties:
//
//   - Pins the signing algorithm to HS256, preventing alg-confusion and the
//     "alg:none" bypass (the header is no longer trusted blindly).
//   - Requires and validates expiry (no token without exp is accepted).
//   - Requires the "authenticated" audience.
//   - Enforces the issuer when expectedIss is provided (defense-in-depth).
//   - Rejects the anon/service_role tokens: those Supabase API keys are JWTs
//     signed with the same secret and must never authorize an end-user request.
//
// expectedIss may be empty to skip the issuer check (e.g. in unit tests).
func verifySupabaseJWT(tokenString, jwtSecret, expectedIss string) (*JWTClaims, error) {
	if jwtSecret == "" {
		return nil, fmt.Errorf("jwt secret not configured")
	}

	opts := []jwt.ParserOption{
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithExpirationRequired(),
		jwt.WithAudience("authenticated"),
	}
	if expectedIss != "" {
		opts = append(opts, jwt.WithIssuer(expectedIss))
	}

	var sc supabaseClaims
	_, err := jwt.NewParser(opts...).ParseWithClaims(tokenString, &sc, func(t *jwt.Token) (any, error) {
		return []byte(jwtSecret), nil
	})
	if err != nil {
		switch {
		case errors.Is(err, jwt.ErrTokenExpired):
			return nil, fmt.Errorf("token expired")
		case errors.Is(err, jwt.ErrTokenSignatureInvalid):
			return nil, fmt.Errorf("signature verification failed")
		default:
			return nil, fmt.Errorf("invalid token: %w", err)
		}
	}

	switch sc.Role {
	case "anon", "service_role":
		return nil, fmt.Errorf("non-user token role %q rejected", sc.Role)
	}

	claims := &JWTClaims{
		Sub:          sc.Subject,
		Email:        sc.Email,
		Phone:        sc.Phone,
		Role:         sc.Role,
		Iss:          sc.Issuer,
		AppMetadata:  sc.AppMetadata,
		UserMetadata: sc.UserMetadata,
	}
	if len(sc.Audience) > 0 {
		claims.Aud = sc.Audience[0]
	}
	if sc.ExpiresAt != nil {
		claims.Exp = sc.ExpiresAt.Unix()
	}
	if sc.IssuedAt != nil {
		claims.Iat = sc.IssuedAt.Unix()
	}
	return claims, nil
}
