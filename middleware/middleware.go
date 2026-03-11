package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/logger"
	"github.com/CodeSyncr/nimbus/router"
)

// Logger logs each request (AdonisJS middleware style) using the Nimbus
// structured logger package. Applications can override the underlying logger
// via logger.Set for custom formatting or destinations.
func Logger() router.Middleware {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *http.Context) error {
			start := time.Now()
			path := c.Request.URL.Path
			method := c.Request.Method
			clientIP := c.Request.RemoteAddr
			err := next(c)
			duration := time.Since(start)
			logger.Info("http_request",
				"method", method,
				"path", path,
				"client_ip", clientIP,
				"duration_ms", duration.Milliseconds(),
				"error", err != nil,
			)
			return err
		}
	}
}

// Recover recovers from panics and returns 500.
func Recover() router.Middleware {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *http.Context) (err error) {
			defer func() {
				if r := recover(); r != nil {
					err = nil
					c.JSON(http.StatusInternalServerError, map[string]string{"error": "Internal server error"})
				}
			}()
			return next(c)
		}
	}
}

// CORS sets basic CORS headers (configurable in real apps via config).
func CORS(origin string) router.Middleware {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *http.Context) error {
			c.Response.Header().Set("Access-Control-Allow-Origin", origin)
			c.Response.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			c.Response.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			if c.Request.Method == http.MethodOptions {
				c.Status(http.StatusNoContent)
				return nil
			}
			return next(c)
		}
	}
}

// CSRF validates a token from header or form (plan: csrf middleware).
const CSRFHeader = "X-CSRF-Token"
const CSRFFormKey = "csrf_token"

// CSRF returns middleware that validates CSRF token for non-GET/HEAD/OPTIONS.
// Token can be in header X-CSRF-Token or form field csrf_token. Use GenerateCSRFToken() to create tokens.
func CSRF(store CSRFStore) router.Middleware {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *http.Context) error {
			if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead || c.Request.Method == http.MethodOptions {
				return next(c)
			}
			token := c.Request.Header.Get(CSRFHeader)
			if token == "" {
				_ = c.Request.ParseForm()
				token = c.Request.FormValue(CSRFFormKey)
			}
			if token == "" || !store.Valid(c.Request.Context(), token) {
				c.JSON(http.StatusForbidden, map[string]string{"error": "invalid csrf token"})
				return nil
			}
			return next(c)
		}
	}
}

// CSRFStore validates and optionally generates tokens (e.g. session-based).
type CSRFStore interface {
	Valid(ctx context.Context, token string) bool
}

// MemoryCSRFStore keeps valid tokens in memory (single-node only).
type MemoryCSRFStore struct {
	mu     sync.RWMutex
	tokens map[string]struct{}
}

func NewMemoryCSRFStore() *MemoryCSRFStore {
	return &MemoryCSRFStore{tokens: make(map[string]struct{})}
}

func (m *MemoryCSRFStore) Valid(ctx context.Context, token string) bool {
	m.mu.RLock()
	_, ok := m.tokens[token]
	m.mu.RUnlock()
	return ok
}

func (m *MemoryCSRFStore) Create() string {
	b := make([]byte, 16)
	rand.Read(b)
	token := hex.EncodeToString(b)
	m.mu.Lock()
	m.tokens[token] = struct{}{}
	m.mu.Unlock()
	return token
}

// GenerateCSRFToken returns a new token (store in session and put in form/header).
func GenerateCSRFToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// rateLimiter holds state for in-memory rate limiting.
type rateLimiter struct {
	mu     sync.Mutex
	counts map[string]*rateEntry
	limit  int
	window time.Duration
}

type rateEntry struct {
	count int
	start time.Time
}

// RateLimit returns middleware that allows limit requests per window per key (keyFn extracts key from request, e.g. IP).
func RateLimit(limit int, window time.Duration, keyFn func(*http.Request) string) router.Middleware {
	rl := &rateLimiter{counts: make(map[string]*rateEntry), limit: limit, window: window}
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *http.Context) error {
			key := keyFn(c.Request)
			if key == "" {
				key = c.Request.RemoteAddr
			}
			if !rl.allow(key) {
				c.JSON(http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded"})
				return nil
			}
			return next(c)
		}
	}
}

func (r *rateLimiter) allow(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	e, ok := r.counts[key]
	if !ok || now.Sub(e.start) > r.window {
		r.counts[key] = &rateEntry{count: 1, start: now}
		return true
	}
	if e.count >= r.limit {
		return false
	}
	e.count++
	return true
}
