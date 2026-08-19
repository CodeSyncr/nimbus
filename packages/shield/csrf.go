package shield

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

const (
	tokenBytes = 32  // 256-bit random tokens
	separator  = "." // raw.signature in cookie
)

// CSRFGuard returns a middleware that protects against Cross-Site
// Request Forgery using a signed double-submit cookie pattern.
//
// On every request it ensures a CSRF token exists (generating one if
// needed) and stores it in the context so handlers and templates can
// access it via shield.Token(ctx) or shield.TokenField(ctx).
//
// For unsafe HTTP methods (POST, PUT, PATCH, DELETE) it validates
// that the token supplied in the request (header or form field)
// matches the token in the cookie.
func CSRFGuard(cfg CSRFConfig) router.Middleware {
	if !cfg.Enabled {
		return func(next router.HandlerFunc) router.HandlerFunc { return next }
	}

	if cfg.Secret == "" {
		cfg.Secret = mustRandom(32)
	}
	if cfg.CookieName == "" {
		cfg.CookieName = "__nimbus_csrf"
	}
	if cfg.SessionCookieName == "" {
		cfg.SessionCookieName = "nimbus_session"
	}
	if cfg.HeaderName == "" {
		cfg.HeaderName = "X-CSRF-Token"
	}
	if cfg.FieldName == "" {
		cfg.FieldName = "_csrf"
	}
	if cfg.MaxAge == 0 {
		cfg.MaxAge = 86400
	}
	if cfg.Path == "" {
		cfg.Path = "/"
	}
	if cfg.SameSite == 0 {
		cfg.SameSite = http.SameSiteLaxMode
	}

	secret := []byte(cfg.Secret)

	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *http.Context) error {
			sid := sessionID(c, cfg.SessionCookieName)
			rawToken := tokenFromCookie(c, cfg.CookieName, sid, secret)

			if rawToken == "" {
				rawToken = mustRandom(tokenBytes)
				setCSRFCookie(c, cfg, rawToken, sid, secret)
			}

			c.Set("_csrf_token", rawToken)

			if isSafeMethod(c.Request.Method) {
				return next(c)
			}

			if isExceptPath(c.Request.URL.Path, cfg.ExceptPaths) {
				return next(c)
			}

			submitted := extractSubmittedToken(c, cfg.HeaderName, cfg.FieldName)
			if !tokenValid(submitted, rawToken) {
				if cfg.ErrorHandler != nil {
					return cfg.ErrorHandler(c)
				}
				return csrfForbidden(c)
			}

			if cfg.RotateToken {
				newToken := mustRandom(tokenBytes)
				setCSRFCookie(c, cfg, newToken, sid, secret)
				c.Set("_csrf_token", newToken)
			}

			return next(c)
		}
	}
}

// sessionID reads the caller's session identifier straight from the request
// cookie so CSRF binding works regardless of whether the session middleware
// has run yet. Returns "" when binding is disabled (cookieName == "-") or no
// session cookie is present (anonymous request).
func sessionID(c *http.Context, cookieName string) string {
	if cookieName == "" || cookieName == "-" {
		return ""
	}
	ck, err := c.Request.Cookie(cookieName)
	if err != nil || ck == nil {
		return ""
	}
	return ck.Value
}

// requestIsHTTPS reports whether the request was served over TLS, directly or
// via a trusted proxy that set X-Forwarded-Proto.
func requestIsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// --------------------------------------------------------------------------
// Token helpers
// --------------------------------------------------------------------------

// signToken binds the token to the session id: the signature covers both the
// random value and the session id, so a token minted for one session fails
// validation under any other (or absent) session.
func signToken(raw, sid string, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(raw))
	mac.Write([]byte{0}) // domain separator between raw and sid
	mac.Write([]byte(sid))
	return hex.EncodeToString(mac.Sum(nil))
}

func tokenFromCookie(c *http.Context, name, sid string, secret []byte) string {
	cookie, err := c.Request.Cookie(name)
	if err != nil || cookie.Value == "" {
		return ""
	}
	parts := strings.SplitN(cookie.Value, separator, 2)
	if len(parts) != 2 {
		return ""
	}
	raw, sig := parts[0], parts[1]
	expected := signToken(raw, sid, secret)
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return ""
	}
	return raw
}

func setCSRFCookie(c *http.Context, cfg CSRFConfig, raw, sid string, secret []byte) {
	sig := signToken(raw, sid, secret)
	http.SetCookie(c.Response, &http.Cookie{
		Name:   cfg.CookieName,
		Value:  raw + separator + sig,
		Path:   cfg.Path,
		Domain: cfg.Domain,
		MaxAge: cfg.MaxAge,
		// Secure whenever the request is HTTPS, even if not explicitly
		// configured — a plaintext CSRF cookie is trivially injectable.
		Secure:   cfg.Secure || requestIsHTTPS(c.Request),
		HttpOnly: cfg.HttpOnly,
		SameSite: cfg.SameSite,
	})
}

func extractSubmittedToken(c *http.Context, headerName, fieldName string) string {
	if t := c.Request.Header.Get(headerName); t != "" {
		return t
	}
	if err := c.Request.ParseForm(); err == nil {
		if t := c.Request.FormValue(fieldName); t != "" {
			return t
		}
	}
	return ""
}

func tokenValid(submitted, expected string) bool {
	if submitted == "" || expected == "" {
		return false
	}
	return hmac.Equal([]byte(submitted), []byte(expected))
}

// --------------------------------------------------------------------------
// Utilities
// --------------------------------------------------------------------------

func isSafeMethod(method string) bool {
	return method == http.MethodGet ||
		method == http.MethodHead ||
		method == http.MethodOptions ||
		method == http.MethodTrace
}

func isExceptPath(path string, except []string) bool {
	for _, prefix := range except {
		p := strings.TrimSuffix(prefix, "*")
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

func csrfForbidden(c *http.Context) error {
	c.Response.Header().Set("Content-Type", "application/json")
	c.Response.WriteHeader(http.StatusForbidden)
	_, _ = c.Response.Write([]byte(`{"error":"CSRF token mismatch"}`))
	return nil
}

func mustRandom(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("shield: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// VerifyOrigin returns a middleware that validates the Origin and
// Referer headers against a list of allowed hosts. This provides an
// additional layer of CSRF defence.
//
//	app.Router.Use(shield.VerifyOrigin("example.com", "www.example.com"))
func VerifyOrigin(allowedHosts ...string) router.Middleware {
	allowed := make(map[string]bool, len(allowedHosts))
	for _, h := range allowedHosts {
		allowed[strings.ToLower(h)] = true
	}

	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *http.Context) error {
			if isSafeMethod(c.Request.Method) {
				return next(c)
			}
			if len(allowed) == 0 {
				return next(c)
			}

			origin := c.Request.Header.Get("Origin")
			if origin == "" {
				origin = c.Request.Header.Get("Referer")
			}
			if origin == "" {
				// Fail closed: an unsafe request that supplies neither Origin
				// nor Referer must not silently bypass origin verification.
				c.Response.Header().Set("Content-Type", "application/json")
				c.Response.WriteHeader(http.StatusForbidden)
				_, _ = c.Response.Write([]byte(`{"error":"missing origin or referer"}`))
				return nil
			}

			host := extractHost(origin)
			if !allowed[strings.ToLower(host)] {
				c.Response.Header().Set("Content-Type", "application/json")
				c.Response.WriteHeader(http.StatusForbidden)
				_, _ = c.Response.Write([]byte(`{"error":"origin not allowed"}`))
				return nil
			}
			return next(c)
		}
	}
}

func extractHost(raw string) string {
	if i := strings.Index(raw, "://"); i >= 0 {
		raw = raw[i+3:]
	}
	if i := strings.Index(raw, "/"); i >= 0 {
		raw = raw[:i]
	}
	if i := strings.LastIndex(raw, ":"); i >= 0 {
		raw = raw[:i]
	}
	return raw
}

// NoTimingLeak sets a constant-time response delay to reduce timing
// side-channel information leakage. Useful on login / token endpoints.
//
//	app.Router.Post("/login", handler, shield.NoTimingLeak(500*time.Millisecond))
func NoTimingLeak(duration time.Duration) router.Middleware {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *http.Context) error {
			start := time.Now()
			err := next(c)
			elapsed := time.Since(start)
			if remaining := duration - elapsed; remaining > 0 {
				time.Sleep(remaining)
			}
			return err
		}
	}
}
