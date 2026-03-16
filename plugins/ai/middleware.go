/*
|--------------------------------------------------------------------------
| AI SDK — Middleware & Observability
|--------------------------------------------------------------------------
|
| Framework-integrated middleware for AI endpoints: rate limiting,
| cost guards, logging. Plus an event-based observability system for
| monitoring AI usage.
|
| Usage:
|
|   // Middleware
|   router.Post("/ai/chat",
|       ai.RateLimit(100, time.Minute),
|       ai.CostGuard(1.00),
|       ai.Logger(),
|       chatHandler,
|   )
|
|   // Observability hooks
|   ai.OnRequest(func(e ai.RequestEvent) {
|       log.Printf("model=%s tokens=%d latency=%s", e.Model, e.Usage.TotalTokens, e.Latency)
|   })
|
|   ai.OnError(func(e ai.RequestEvent) {
|       sentry.CaptureException(e.Error)
|   })
|
*/

package ai

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// ---------------------------------------------------------------------------
// Observability — event hooks
// ---------------------------------------------------------------------------

// RequestHook is a callback for AI request events.
type RequestHook func(RequestEvent)

var (
	hooksMu         sync.RWMutex
	requestHooks    []RequestHook
	errorHooks      []RequestHook
	completionHooks []RequestHook
)

// OnRequest registers a hook called for every AI API request.
func OnRequest(h RequestHook) {
	hooksMu.Lock()
	defer hooksMu.Unlock()
	requestHooks = append(requestHooks, h)
}

// OnError registers a hook called when an AI request fails.
func OnError(h RequestHook) {
	hooksMu.Lock()
	defer hooksMu.Unlock()
	errorHooks = append(errorHooks, h)
}

// OnCompletion registers a hook called on successful completion.
func OnCompletion(h RequestHook) {
	hooksMu.Lock()
	defer hooksMu.Unlock()
	completionHooks = append(completionHooks, h)
}

// EmitRequestEvent fires all registered hooks for a request event.
// Called internally by the client after each API call.
func EmitRequestEvent(e RequestEvent) {
	hooksMu.RLock()
	defer hooksMu.RUnlock()

	for _, h := range requestHooks {
		h(e)
	}

	if e.Error != nil {
		for _, h := range errorHooks {
			h(e)
		}
	} else {
		for _, h := range completionHooks {
			h(e)
		}
	}
}

// ---------------------------------------------------------------------------
// Metrics collector
// ---------------------------------------------------------------------------

// Metrics holds aggregate AI usage statistics.
type Metrics struct {
	TotalRequests   int64         `json:"total_requests"`
	TotalTokens     int64         `json:"total_tokens"`
	TotalErrors     int64         `json:"total_errors"`
	TotalLatency    time.Duration `json:"total_latency_ms"`
	RequestsByModel sync.Map      `json:"-"`
}

var globalMetrics = &Metrics{}

// GetMetrics returns the global AI metrics.
func GetMetrics() *Metrics {
	return globalMetrics
}

// initMetricsCollector installs a hook that collects aggregate metrics.
func initMetricsCollector() {
	OnRequest(func(e RequestEvent) {
		atomic.AddInt64(&globalMetrics.TotalRequests, 1)
		if e.Usage != nil {
			atomic.AddInt64(&globalMetrics.TotalTokens, int64(e.Usage.TotalTokens))
		}
		if e.Error != nil {
			atomic.AddInt64(&globalMetrics.TotalErrors, 1)
		}
		// Note: Duration accumulation is approximate under high concurrency.
		atomic.AddInt64((*int64)(&globalMetrics.TotalLatency), int64(e.Latency))
	})
}

// ---------------------------------------------------------------------------
// HTTP Middleware (Nimbus router compatible)
// ---------------------------------------------------------------------------

// HandlerFunc matches Nimbus router.HandlerFunc. We use http types
// here to avoid import cycles; the plugin adapter wraps these.
type httpMiddleware func(http.Handler) http.Handler

// RateLimit returns middleware that limits AI requests per window.
func RateLimit(maxRequests int, window time.Duration) httpMiddleware {
	type bucket struct {
		count int
		reset time.Time
	}
	var mu sync.Mutex
	buckets := make(map[string]*bucket)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.RemoteAddr
			mu.Lock()
			b, ok := buckets[key]
			if !ok || time.Now().After(b.reset) {
				b = &bucket{count: 0, reset: time.Now().Add(window)}
				buckets[key] = b
			}
			b.count++
			if b.count > maxRequests {
				mu.Unlock()
				http.Error(w, `{"error":"AI rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}
			mu.Unlock()
			next.ServeHTTP(w, r)
		})
	}
}

// CostGuard returns middleware that blocks requests when estimated
// cost exceeds the budget. Uses a simple token-cost model.
func CostGuard(maxCostUSD float64) httpMiddleware {
	var totalCost float64
	var mu sync.Mutex

	// Track cost via observability hook.
	OnCompletion(func(e RequestEvent) {
		if e.Usage == nil {
			return
		}
		// Rough estimate: $0.01 per 1K tokens (override per-model for accuracy).
		cost := float64(e.Usage.TotalTokens) * 0.00001
		mu.Lock()
		totalCost += cost
		mu.Unlock()
	})

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			exceeded := totalCost > maxCostUSD
			mu.Unlock()
			if exceeded {
				http.Error(w, `{"error":"AI cost budget exceeded"}`, http.StatusPaymentRequired)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Logger returns middleware that logs AI requests.
func Logger() httpMiddleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			next.ServeHTTP(w, r)
			log.Printf("[ai] %s %s %s", r.Method, r.URL.Path, time.Since(start))
		})
	}
}

// ---------------------------------------------------------------------------
// Usage reporter
// ---------------------------------------------------------------------------

// UsageReport returns a formatted string of current AI usage metrics.
func UsageReport() string {
	m := GetMetrics()
	return fmt.Sprintf(
		"AI Usage: requests=%d tokens=%d errors=%d avg_latency=%s",
		atomic.LoadInt64(&m.TotalRequests),
		atomic.LoadInt64(&m.TotalTokens),
		atomic.LoadInt64(&m.TotalErrors),
		avgLatency(m),
	)
}

func avgLatency(m *Metrics) time.Duration {
	reqs := atomic.LoadInt64(&m.TotalRequests)
	if reqs == 0 {
		return 0
	}
	total := time.Duration(atomic.LoadInt64((*int64)(&m.TotalLatency)))
	return total / time.Duration(reqs)
}
