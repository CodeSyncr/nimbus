package middleware

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	stderrors "errors"
	"fmt"
	"net"
	stdhttp "net/http"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CodeSyncr/nimbus/errors"
	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/logger"
	"github.com/CodeSyncr/nimbus/router"
)

// Logger logs each request using the Nimbus structured logger package.
//
// It emits one structured line per request with keyed fields (method, path,
// status, duration_ms, remote_addr) and — when the RequestID middleware runs
// earlier in the chain — a correlating request_id, so log lines can be joined
// to the X-Request-Id response header. The log level is chosen from the status
// code: 5xx → error, 4xx → warn, else info.
//
// Applications can override the underlying logger via logger.Set for custom
// formatting or destinations.
func Logger() router.Middleware {
	return router.NameMiddleware("logger", func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *http.Context) error {
			start := time.Now()

			// The context only records a status when a handler used one of its
			// helpers. Anything writing to the ResponseWriter directly — the
			// router's own 404, a hand-rolled stream — left it at 200, so the
			// log said 200 for a request that failed. Wrapping the writer
			// records what was actually sent.
			rec := &statusRecorder{ResponseWriter: c.Response, status: stdhttp.StatusOK}
			original := c.Response
			c.Response = rec
			err := next(c)
			c.Response = original
			duration := time.Since(start)

			// A handler that returns an error has its status written by the
			// router, above this middleware — so the recorder never sees it.
			// The error carries the status, so read it from there.
			status := rec.status
			if !rec.written && err != nil {
				status = statusFromError(err)
			}
			log := logger.ForRequest(c)
			fields := []any{
				"method", c.Request.Method,
				"path", c.Request.URL.Path,
				"status", status,
				"duration_ms", float64(duration.Microseconds()) / 1000.0,
				"remote_addr", c.Request.RemoteAddr,
			}

			if consoleRequests {
				fmt.Fprintln(os.Stdout, ConsoleRequestLine(c.Request.Method, c.Request.URL.Path, status, duration))
				return err
			}

			switch {
			case status >= 500:
				log.Errorw("request", fields...)
			case status >= 400:
				log.Warnw("request", fields...)
			default:
				log.Infow("request", fields...)
			}
			return err
		}
	})
}

// Recover recovers from panics and returns a wrapped error so errors.Handler
// can render JSON or HTML consistently (and optional Telescope hooks).
func Recover() router.Middleware {
	return router.NameMiddleware("recover", func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *http.Context) (err error) {
			defer func() {
				if r := recover(); r != nil {
					ae := errors.Wrap(http.StatusInternalServerError, fmt.Errorf("panic: %v", r))
					ae.StackTrace = string(debug.Stack())
					err = ae
				}
			}()
			return next(c)
		}
	})
}

// CORS sets basic CORS headers. Accepts one or more allowed origins.
// When multiple origins are given, the middleware validates the request's
// Origin header against the list and only reflects a matching origin.
func CORS(origins ...string) router.Middleware {
	allowed := make(map[string]bool, len(origins))
	for _, o := range origins {
		allowed[o] = true
	}
	single := len(origins) == 1
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *http.Context) error {
			origin := c.Request.Header.Get("Origin")
			if single {
				c.Response.Header().Set("Access-Control-Allow-Origin", origins[0])
			} else if allowed[origin] {
				c.Response.Header().Set("Access-Control-Allow-Origin", origin)
				c.Response.Header().Set("Vary", "Origin")
			}
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

// MemoryCSRFStore keeps valid tokens in process memory (single-node only).
// It is a minimal helper: tokens are NOT bound to a session (any valid
// token authorizes any caller). Tokens expire after TTL (default 2h) and at
// most MaxTokens (default 100,000) are kept, oldest dropped first. For
// production CSRF protection prefer shield.CSRFGuard, which uses a signed,
// session-bound double-submit cookie.
type MemoryCSRFStore struct {
	TTL       time.Duration
	MaxTokens int

	mu     sync.Mutex
	tokens map[string]time.Time // token -> expiry
	order  []string             // creation order, for eviction
}

func NewMemoryCSRFStore() *MemoryCSRFStore {
	return &MemoryCSRFStore{
		TTL:       2 * time.Hour,
		MaxTokens: 100_000,
		tokens:    make(map[string]time.Time),
	}
}

func (m *MemoryCSRFStore) Valid(ctx context.Context, token string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	exp, ok := m.tokens[token]
	if ok && time.Now().After(exp) {
		delete(m.tokens, token)
		return false
	}
	return ok
}

func (m *MemoryCSRFStore) Create() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("nimbus: csrf: crypto/rand failed: " + err.Error())
	}
	token := hex.EncodeToString(b)
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	m.tokens[token] = now.Add(m.TTL)
	m.order = append(m.order, token)
	m.pruneLocked(now)
	return token
}

// Len returns the number of tokens held (including expired ones not yet pruned).
func (m *MemoryCSRFStore) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.tokens)
}

// pruneLocked drops tokens from the front of the creation order while they
// are expired, already gone, or over MaxTokens. Tokens share one TTL, so the
// oldest always expire first and this stays amortized O(1) per Create.
func (m *MemoryCSRFStore) pruneLocked(now time.Time) {
	i := 0
	for ; i < len(m.order); i++ {
		tok := m.order[i]
		exp, ok := m.tokens[tok]
		over := m.MaxTokens > 0 && len(m.tokens) > m.MaxTokens
		if ok && now.Before(exp) && !over {
			break
		}
		delete(m.tokens, tok)
	}
	if i > 0 {
		m.order = append(m.order[:0], m.order[i:]...)
	}
}

// GenerateCSRFToken returns a new token (store in session and put in form/header).
func GenerateCSRFToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("nimbus: csrf: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// rateLimiter holds state for in-memory rate limiting (fixed windows).
type rateLimiter struct {
	mu        sync.Mutex
	counts    map[string]*rateEntry
	limit     int
	window    time.Duration
	lastSweep time.Time
}

type rateEntry struct {
	count int
	start time.Time
}

// RateLimit returns middleware that allows limit requests per window per key (keyFn extracts key from request, e.g. IP).
// State is per process; use RateLimitRedis when running several instances.
func RateLimit(limit int, window time.Duration, keyFn func(*http.Request) string) router.Middleware {
	rl := &rateLimiter{counts: make(map[string]*rateEntry), limit: limit, window: window, lastSweep: time.Now()}
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *http.Context) error {
			key := ""
			if keyFn != nil {
				key = keyFn(c.Request)
			}
			if key == "" {
				key = ClientIP(c.Request)
			}
			ok, remaining, reset := rl.allow(key)
			h := c.Response.Header()
			h.Set("X-RateLimit-Limit", strconv.Itoa(limit))
			h.Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
			if !ok {
				h.Set("Retry-After", strconv.Itoa(retryAfterSeconds(time.Until(reset))))
				c.JSON(http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded"})
				return nil
			}
			return next(c)
		}
	}
}

// allow counts a request for key and reports whether it is within the
// limit, how many requests remain, and when the window resets.
func (r *rateLimiter) allow(key string) (bool, int, time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	r.sweep(now)
	e, ok := r.counts[key]
	if !ok || now.Sub(e.start) >= r.window {
		e = &rateEntry{start: now}
		r.counts[key] = e
	}
	reset := e.start.Add(r.window)
	if e.count >= r.limit {
		return false, 0, reset
	}
	e.count++
	return true, r.limit - e.count, reset
}

// sweep drops expired windows once per window, so keys from clients that
// went away (every IP that ever made a request) do not pile up forever.
func (r *rateLimiter) sweep(now time.Time) {
	if now.Sub(r.lastSweep) < r.window {
		return
	}
	r.lastSweep = now
	for k, e := range r.counts {
		if now.Sub(e.start) >= r.window {
			delete(r.counts, k)
		}
	}
}

func (r *rateLimiter) size() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.counts)
}

// retryAfterSeconds rounds up, so clients never retry before the reset.
func retryAfterSeconds(d time.Duration) int {
	secs := int((d + time.Second - 1) / time.Second)
	return max(secs, 1)
}

// ---------------------------------------------------------------------------
// Console request lines
// ---------------------------------------------------------------------------

// consoleRequests reports whether request lines should be printed for a human
// rather than emitted as structured records.
//
// Structured logs are what a production system wants; a developer watching a
// terminal wants to read the request. The choice follows the environment: a
// real terminal and a non-JSON log format means someone is watching.
var consoleRequests = func() bool {
	if os.Getenv("NO_COLOR") != "" || strings.EqualFold(os.Getenv("LOG_FORMAT"), "json") {
		return false
	}
	if strings.EqualFold(os.Getenv("APP_ENV"), "production") {
		return false
	}
	// Under `nimbus serve` stdout is a pipe into the CLI, not a terminal — but
	// the CLI passes these lines straight through to one, so a human is still
	// watching and the request log belongs on screen.
	if os.Getenv("NIMBUS_SERVE") == "1" {
		return true
	}
	info, err := os.Stdout.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}()

// ConsoleRequestLine renders one request the way the startup view renders
// everything else: "[HTTP] GET /cloud 200 OK — 0.8ms".
func ConsoleRequestLine(method, path string, status int, d time.Duration) string {
	statusText := stdhttp.StatusText(status)
	if statusText == "" {
		statusText = "—"
	}

	colour := "\033[32m" // 2xx
	switch {
	case status >= 500:
		colour = "\033[31m"
	case status >= 400:
		colour = "\033[33m"
	case status >= 300:
		colour = "\033[36m"
	}

	return fmt.Sprintf("\033[2m[HTTP]\033[0m \033[1m%s\033[0m %s %s%d %s\033[0m \033[2m— %s\033[0m",
		method, path, colour, status, statusText, formatRequestDuration(d))
}

// formatRequestDuration keeps timings to the precision that is meaningful.
func formatRequestDuration(d time.Duration) string {
	switch {
	case d >= time.Second:
		return fmt.Sprintf("%.2fs", d.Seconds())
	case d >= 10*time.Millisecond:
		return fmt.Sprintf("%dms", d.Milliseconds())
	default:
		return fmt.Sprintf("%.1fms", float64(d.Microseconds())/1000)
	}
}

// statusRecorder observes the status written to a response.
//
// It forwards Flush and Hijack so streaming and upgraded connections keep
// working: Nimbus serves SSE through this path, and a wrapper that swallowed
// Flush would buffer a stream until it completed.
type statusRecorder struct {
	stdhttp.ResponseWriter
	status  int
	written bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.written {
		r.status = code
		r.written = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.written {
		// An implicit 200, the same as net/http would send.
		r.written = true
	}
	return r.ResponseWriter.Write(b)
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(stdhttp.Flusher); ok {
		f.Flush()
	}
}

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := r.ResponseWriter.(stdhttp.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("middleware: the underlying writer cannot be hijacked")
	}
	return h.Hijack()
}

// Unwrap lets anything that needs the original writer reach it.
func (r *statusRecorder) Unwrap() stdhttp.ResponseWriter { return r.ResponseWriter }

// statusError is implemented by errors that carry an HTTP status. It is
// declared here rather than imported so the middleware package does not depend
// on the router or the errors package.
type statusError interface{ HTTPStatus() int }

// statusFromError reads the status an error will be rendered with.
func statusFromError(err error) int {
	var se statusError
	if stderrors.As(err, &se) {
		if code := se.HTTPStatus(); code > 0 {
			return code
		}
	}
	return stdhttp.StatusInternalServerError
}
