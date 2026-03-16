package middleware

import (
	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

// SecureHeadersConfig controls which security headers are set.
type SecureHeadersConfig struct {
	// HSTS sets Strict-Transport-Security. Default: "max-age=63072000; includeSubDomains".
	HSTS string

	// ContentTypeNoSniff sets X-Content-Type-Options: nosniff. Default: true.
	ContentTypeNoSniff bool

	// FrameOptions sets X-Frame-Options. Default: "DENY".
	FrameOptions string

	// XSSProtection sets X-XSS-Protection. Default: "1; mode=block".
	XSSProtection string

	// ReferrerPolicy sets Referrer-Policy. Default: "strict-origin-when-cross-origin".
	ReferrerPolicy string

	// ContentSecurityPolicy sets Content-Security-Policy. Default: "" (not set).
	ContentSecurityPolicy string

	// PermissionsPolicy sets Permissions-Policy. Default: "" (not set).
	PermissionsPolicy string
}

// DefaultSecureHeadersConfig returns sensible production defaults.
func DefaultSecureHeadersConfig() SecureHeadersConfig {
	return SecureHeadersConfig{
		HSTS:               "max-age=63072000; includeSubDomains",
		ContentTypeNoSniff: true,
		FrameOptions:       "DENY",
		XSSProtection:      "1; mode=block",
		ReferrerPolicy:     "strict-origin-when-cross-origin",
	}
}

// SecureHeaders sets production-grade security headers on every response.
//
// Usage:
//
//	r.Use(middleware.SecureHeaders(middleware.DefaultSecureHeadersConfig()))
func SecureHeaders(cfg SecureHeadersConfig) router.Middleware {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *http.Context) error {
			h := c.Response.Header()
			if cfg.HSTS != "" {
				h.Set("Strict-Transport-Security", cfg.HSTS)
			}
			if cfg.ContentTypeNoSniff {
				h.Set("X-Content-Type-Options", "nosniff")
			}
			if cfg.FrameOptions != "" {
				h.Set("X-Frame-Options", cfg.FrameOptions)
			}
			if cfg.XSSProtection != "" {
				h.Set("X-XSS-Protection", cfg.XSSProtection)
			}
			if cfg.ReferrerPolicy != "" {
				h.Set("Referrer-Policy", cfg.ReferrerPolicy)
			}
			if cfg.ContentSecurityPolicy != "" {
				h.Set("Content-Security-Policy", cfg.ContentSecurityPolicy)
			}
			if cfg.PermissionsPolicy != "" {
				h.Set("Permissions-Policy", cfg.PermissionsPolicy)
			}
			return next(c)
		}
	}
}
