package shield

import (
	"time"

	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

// Config holds all Shield security settings. Zero values are safe
// defaults — call DefaultConfig() for a production-ready baseline.
type Config struct {
	// ContentTypeNosniff sets X-Content-Type-Options: nosniff to
	// prevent MIME-type sniffing attacks.
	ContentTypeNosniff bool

	// XSSProtection controls the X-XSS-Protection header.
	// Modern recommendation: "0" (disable browser's built-in XSS
	// auditor; rely on CSP instead). Set to "1; mode=block" for
	// legacy browsers.
	XSSProtection string

	// FrameGuard controls the X-Frame-Options header to prevent
	// clickjacking. Values: "DENY", "SAMEORIGIN".
	FrameGuard string

	// HSTS configures HTTP Strict Transport Security.
	HSTS HSTSConfig

	// ReferrerPolicy sets the Referrer-Policy header.
	// Common values: "no-referrer", "strict-origin-when-cross-origin",
	// "same-origin", "origin".
	ReferrerPolicy string

	// DNSPrefetchControl sets X-DNS-Prefetch-Control.
	// true = "on", false = "off".
	DNSPrefetchControl bool

	// DownloadOptions sets X-Download-Options: noopen to prevent
	// old IE from executing downloads in the site's context.
	DownloadOptions bool

	// PermittedCrossDomainPolicies sets X-Permitted-Cross-Domain-Policies.
	// Values: "none", "master-only", "by-content-type", "all".
	PermittedCrossDomainPolicies string

	// CrossOriginOpenerPolicy sets Cross-Origin-Opener-Policy.
	// Values: "same-origin", "same-origin-allow-popups", "unsafe-none".
	CrossOriginOpenerPolicy string

	// CrossOriginResourcePolicy sets Cross-Origin-Resource-Policy.
	// Values: "same-origin", "same-site", "cross-origin".
	CrossOriginResourcePolicy string

	// CrossOriginEmbedderPolicy sets Cross-Origin-Embedder-Policy.
	// Values: "require-corp", "credentialless", "unsafe-none".
	CrossOriginEmbedderPolicy string

	// CSP configures Content-Security-Policy headers.
	CSP CSPConfig

	// CSRF configures Cross-Site Request Forgery protection.
	CSRF CSRFConfig
}

// HSTSConfig controls HTTP Strict Transport Security.
type HSTSConfig struct {
	Enabled           bool
	MaxAge            time.Duration
	IncludeSubdomains bool
	Preload           bool
}

// CSPConfig holds Content Security Policy settings.
type CSPConfig struct {
	Enabled    bool
	ReportOnly bool
	Policy     *CSPBuilder
}

// CSRFConfig holds CSRF protection settings.
type CSRFConfig struct {
	Enabled bool

	// Secret used for HMAC signing of tokens. Must be at least 32 bytes.
	// If empty, a random secret is generated at startup.
	Secret string

	// CookieName is the name of the CSRF cookie (default: "__nimbus_csrf").
	CookieName string

	// HeaderName is the HTTP header checked for the token (default: "X-CSRF-Token").
	HeaderName string

	// FieldName is the form field checked for the token (default: "_csrf").
	FieldName string

	// MaxAge sets cookie lifetime in seconds (default: 86400 = 24h).
	MaxAge int

	// Secure marks the cookie as Secure (HTTPS only).
	Secure bool

	// SameSite sets the SameSite cookie attribute.
	SameSite http.SameSite

	// Path sets the cookie path (default: "/").
	Path string

	// Domain sets the cookie domain.
	Domain string

	// HttpOnly marks the cookie HttpOnly. When true, client-side JS
	// cannot read the cookie — the token must be embedded in forms
	// via a template helper. When false, JS can read the cookie and
	// send it in a custom header.
	HttpOnly bool

	// ExceptPaths is a list of path prefixes to skip CSRF validation.
	ExceptPaths []string

	// RotateToken rotates the CSRF token after each successful validation.
	// When true, tokens are one-time use — the form must be re-rendered
	// with the new token for the next request. Set false for Unpoly/AJAX
	// flows where forms are not replaced after submit (default: false).
	RotateToken bool

	// ErrorHandler is called when CSRF validation fails. If nil, a
	// default 403 JSON response is sent.
	ErrorHandler router.HandlerFunc
}

// DefaultConfig returns a production-ready Shield configuration.
func DefaultConfig() Config {
	return Config{
		ContentTypeNosniff: true,
		XSSProtection:      "0",
		FrameGuard:         "SAMEORIGIN",
		HSTS: HSTSConfig{
			Enabled:           false,
			MaxAge:            365 * 24 * time.Hour,
			IncludeSubdomains: true,
			Preload:           false,
		},
		ReferrerPolicy:               "strict-origin-when-cross-origin",
		DNSPrefetchControl:           false,
		DownloadOptions:              true,
		PermittedCrossDomainPolicies: "none",
		CrossOriginOpenerPolicy:      "same-origin",
		CrossOriginResourcePolicy:    "same-origin",
		CrossOriginEmbedderPolicy:    "",
		CSP: CSPConfig{
			Enabled:    false,
			ReportOnly: false,
			Policy:     nil,
		},
		CSRF: CSRFConfig{
			Enabled:    true,
			CookieName: "__nimbus_csrf",
			HeaderName: "X-CSRF-Token",
			FieldName:  "_csrf",
			MaxAge:     86400,
			Secure:     false,
			SameSite:   http.SameSiteLaxMode,
			Path:       "/",
			HttpOnly:   true,
		},
	}
}

// csrfTokenKey is the context key used to store the CSRF token so
// handlers and templates can access it via shield.Token(ctx).
type csrfTokenKey struct{}

// Token returns the CSRF token for the current request. Use this when
// you need the raw token (e.g. for Ajax headers). For forms, use {{ .csrfField }}
// in templates — it is auto-injected by context.View when Shield CSRF is enabled.
func Token(c *http.Context) string {
	if v, ok := c.Get("_csrf_token"); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// TokenField returns a full HTML hidden input for embedding in forms.
// Prefer {{ .csrfField }} in templates — it is auto-injected by context.View.
func TokenField(c *http.Context) string {
	return `<input type="hidden" name="_csrf" value="` + Token(c) + `">`
}
