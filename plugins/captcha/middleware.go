package captcha

import (
	"fmt"
	"net/http"

	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

// MiddlewareOptions configures captcha protection middleware behavior.
type MiddlewareOptions struct {
	Provider       string
	TokenFormField string
	TokenHeader    string
}

// DefaultMiddlewareOptions provides sensible default form/header field names.
func DefaultMiddlewareOptions() MiddlewareOptions {
	return MiddlewareOptions{
		Provider:       "turnstile",
		TokenFormField: "cf-turnstile-response",
		TokenHeader:    "X-Captcha-Token",
	}
}

// Protect returns a Nimbus HTTP middleware that verifies incoming captchas.
//
// Usage:
//
//	app.Router.Post("/submit", captcha.Protect(), SubmitHandler)
//	app.Router.Post("/register", captcha.ProtectWithOptions(captcha.MiddlewareOptions{Provider: "recaptcha"}), RegisterHandler)
func Protect() router.Middleware {
	return ProtectWithOptions(DefaultMiddlewareOptions())
}

// ProtectWithOptions returns middleware configured with custom options.
func ProtectWithOptions(opts MiddlewareOptions) router.Middleware {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *nhttp.Context) error {
			manager := GetManager()
			if manager == nil {
				return c.JSON(http.StatusInternalServerError, map[string]string{
					"error": "captcha: plugin not registered or initialized",
				})
			}

			token := extractToken(c, opts)
			if token == "" {
				return c.JSON(http.StatusUnprocessableEntity, map[string]any{
					"error":   "captcha verification failed",
					"details": "missing captcha response token",
				})
			}

			provider := opts.Provider
			if provider == "" {
				provider = manager.config.DefaultProvider
			}

			res, err := manager.Verifier.VerifyToken(c.Request.Context(), provider, token, c.IP())
			if err != nil {
				return c.JSON(http.StatusInternalServerError, map[string]string{
					"error": fmt.Sprintf("captcha error: %v", err),
				})
			}

			if !res.Success {
				return c.JSON(http.StatusForbidden, map[string]any{
					"error":   "captcha verification failed",
					"details": res.ErrorCodes,
				})
			}

			return next(c)
		}
	}
}

func extractToken(c *nhttp.Context, opts MiddlewareOptions) string {
	// 1. Check Form/Input value
	if opts.TokenFormField != "" {
		if val := c.Input(opts.TokenFormField); val != "" {
			return val
		}
	}

	// 2. Check standard fallback form fields
	for _, field := range []string{"cf-turnstile-response", "g-recaptcha-response", "h-captcha-response", "captcha_token"} {
		if val := c.Input(field); val != "" {
			return val
		}
	}

	// 3. Check HTTP Header
	if opts.TokenHeader != "" {
		if val := c.Header(opts.TokenHeader); val != "" {
			return val
		}
	}

	// 4. Check fallback headers
	if val := c.Header("X-Captcha-Token"); val != "" {
		return val
	}

	return ""
}
