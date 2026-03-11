package shield

import (
	"fmt"
	"strings"

	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

// Guard returns a middleware that sets all configured security headers
// on every response. It does NOT include CSRF protection — use
// CSRFGuard() separately for that.
//
// Usage in start/kernel.go:
//
//	cfg := shield.DefaultConfig()
//	app.Router.Use(
//	    shield.Guard(cfg),
//	    shield.CSRFGuard(cfg.CSRF),
//	)
func Guard(cfg Config) router.Middleware {
	headers := buildStaticHeaders(cfg)

	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *http.Context) error {
			h := c.Response.Header()
			for k, v := range headers {
				h.Set(k, v)
			}
			return next(c)
		}
	}
}

// buildStaticHeaders pre-computes the header map from config so
// the middleware hot-path is a simple map copy.
func buildStaticHeaders(cfg Config) map[string]string {
	h := make(map[string]string)

	if cfg.ContentTypeNosniff {
		h["X-Content-Type-Options"] = "nosniff"
	}

	if cfg.XSSProtection != "" {
		h["X-XSS-Protection"] = cfg.XSSProtection
	}

	if cfg.FrameGuard != "" {
		h["X-Frame-Options"] = cfg.FrameGuard
	}

	if cfg.ReferrerPolicy != "" {
		h["Referrer-Policy"] = cfg.ReferrerPolicy
	}

	if cfg.DNSPrefetchControl {
		h["X-DNS-Prefetch-Control"] = "on"
	} else {
		h["X-DNS-Prefetch-Control"] = "off"
	}

	if cfg.DownloadOptions {
		h["X-Download-Options"] = "noopen"
	}

	if cfg.PermittedCrossDomainPolicies != "" {
		h["X-Permitted-Cross-Domain-Policies"] = cfg.PermittedCrossDomainPolicies
	}

	if cfg.CrossOriginOpenerPolicy != "" {
		h["Cross-Origin-Opener-Policy"] = cfg.CrossOriginOpenerPolicy
	}

	if cfg.CrossOriginResourcePolicy != "" {
		h["Cross-Origin-Resource-Policy"] = cfg.CrossOriginResourcePolicy
	}

	if cfg.CrossOriginEmbedderPolicy != "" {
		h["Cross-Origin-Embedder-Policy"] = cfg.CrossOriginEmbedderPolicy
	}

	if cfg.HSTS.Enabled {
		hsts := fmt.Sprintf("max-age=%d", int(cfg.HSTS.MaxAge.Seconds()))
		if cfg.HSTS.IncludeSubdomains {
			hsts += "; includeSubDomains"
		}
		if cfg.HSTS.Preload {
			hsts += "; preload"
		}
		h["Strict-Transport-Security"] = hsts
	}

	if cfg.CSP.Enabled && cfg.CSP.Policy != nil {
		policy := cfg.CSP.Policy.String()
		if policy != "" {
			if cfg.CSP.ReportOnly {
				h["Content-Security-Policy-Report-Only"] = policy
			} else {
				h["Content-Security-Policy"] = policy
			}
		}
	}

	return h
}

// RemoveHeader is a convenience middleware that strips response headers
// you never want to leak (e.g. "X-Powered-By", "Server").
//
//	app.Router.Use(shield.RemoveHeader("X-Powered-By", "Server"))
func RemoveHeader(names ...string) router.Middleware {
	lower := make([]string, len(names))
	for i, n := range names {
		lower[i] = strings.ToLower(n)
	}
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *http.Context) error {
			err := next(c)
			h := c.Response.Header()
			for _, n := range lower {
				h.Del(n)
			}
			return err
		}
	}
}
