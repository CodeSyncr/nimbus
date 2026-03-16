// Package pulse provides lightweight application monitoring for Nimbus
// applications. It collects metrics on requests, slow queries, exceptions,
// queue throughput, and cache hit rates with minimal overhead.
//
// Unlike Telescope which records every request, Pulse uses aggregated metrics
// and ring buffers suitable for production use.
//
//	p := pulse.New(pulse.Config{MaxEntries: 10000})
//	app.Use(p.Middleware())
//	app.GET("/pulse", p.DashboardHandler())
package pulse

import (
	"encoding/json"
	"math"
	"sync"
	"time"

	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

// ── Config ──────────────────────────────────────────────────────

// Config holds Pulse configuration options.
type Config struct {
	// MaxEntries is the maximum number of entries to keep in each ring buffer.
	MaxEntries int
	// SlowQueryThreshold marks queries slower than this as "slow" (default: 100ms).
	SlowQueryThreshold time.Duration
	// SlowRequestThreshold marks requests slower than this as "slow" (default: 500ms).
	SlowRequestThreshold time.Duration
}

func (c *Config) setDefaults() {
	if c.MaxEntries <= 0 {
		c.MaxEntries = 10000
	}
	if c.SlowQueryThreshold <= 0 {
		c.SlowQueryThreshold = 100 * time.Millisecond
	}
	if c.SlowRequestThreshold <= 0 {
		c.SlowRequestThreshold = 500 * time.Millisecond
	}
}

// ── Entry types ─────────────────────────────────────────────────

// EntryType identifies the kind of monitoring entry.
type EntryType string

const (
	EntryRequest   EntryType = "request"
	EntrySlowQuery EntryType = "slow_query"
	EntryException EntryType = "exception"
	EntryJob       EntryType = "job"
	EntryCache     EntryType = "cache"
	EntryMail      EntryType = "mail"
)

// Entry is a single monitoring record.
type Entry struct {
	Type      EntryType      `json:"type"`
	Timestamp time.Time      `json:"timestamp"`
	Duration  time.Duration  `json:"duration_ms"`
	Data      map[string]any `json:"data"`
}

// ── Aggregated Stats ────────────────────────────────────────────

// RequestStats holds aggregated request statistics.
type RequestStats struct {
	TotalRequests  int64         `json:"total_requests"`
	TotalErrors    int64         `json:"total_errors"`
	AvgDuration    float64       `json:"avg_duration_ms"`
	P50Duration    float64       `json:"p50_duration_ms"`
	P95Duration    float64       `json:"p95_duration_ms"`
	P99Duration    float64       `json:"p99_duration_ms"`
	RequestsPerMin float64       `json:"requests_per_min"`
	StatusCounts   map[int]int64 `json:"status_counts"`
	SlowRequests   int64         `json:"slow_requests"`
	TopPaths       []PathStat    `json:"top_paths"`
}

// PathStat holds per-path request stats.
type PathStat struct {
	Path        string  `json:"path"`
	Count       int64   `json:"count"`
	AvgDuration float64 `json:"avg_duration_ms"`
	ErrorCount  int64   `json:"error_count"`
}

// CacheStats holds cache hit/miss statistics.
type CacheStats struct {
	Hits    int64   `json:"hits"`
	Misses  int64   `json:"misses"`
	Writes  int64   `json:"writes"`
	Deletes int64   `json:"deletes"`
	HitRate float64 `json:"hit_rate"`
}

// QueueStats holds queue job statistics.
type QueueStats struct {
	Processed int64   `json:"processed"`
	Failed    int64   `json:"failed"`
	Pending   int64   `json:"pending"`
	AvgTime   float64 `json:"avg_time_ms"`
}

// ── Ring buffer ─────────────────────────────────────────────────

type ringBuffer struct {
	mu      sync.RWMutex
	entries []Entry
	pos     int
	full    bool
	max     int
}

func newRingBuffer(size int) *ringBuffer {
	return &ringBuffer{
		entries: make([]Entry, size),
		max:     size,
	}
}

func (rb *ringBuffer) Add(e Entry) {
	rb.mu.Lock()
	rb.entries[rb.pos] = e
	rb.pos = (rb.pos + 1) % rb.max
	if rb.pos == 0 {
		rb.full = true
	}
	rb.mu.Unlock()
}

func (rb *ringBuffer) All() []Entry {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if !rb.full {
		result := make([]Entry, rb.pos)
		copy(result, rb.entries[:rb.pos])
		return result
	}

	result := make([]Entry, rb.max)
	// Oldest entries first
	copy(result, rb.entries[rb.pos:])
	copy(result[rb.max-rb.pos:], rb.entries[:rb.pos])
	return result
}

func (rb *ringBuffer) Recent(n int) []Entry {
	all := rb.All()
	if len(all) <= n {
		return all
	}
	// Return most recent n entries (end of slice)
	return all[len(all)-n:]
}

func (rb *ringBuffer) Count() int {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	if rb.full {
		return rb.max
	}
	return rb.pos
}

// ── Pulse ───────────────────────────────────────────────────────

// Pulse is the main monitoring instance.
type Pulse struct {
	config Config

	requests    *ringBuffer
	slowQueries *ringBuffer
	exceptions  *ringBuffer
	jobs        *ringBuffer
	cacheOps    *ringBuffer
	mails       *ringBuffer

	// Aggregation counters
	mu            sync.RWMutex
	totalRequests int64
	totalErrors   int64
	totalDuration float64
	durations     []float64
	statusCounts  map[int]int64
	pathStats     map[string]*pathCounter
	slowReqCount  int64

	cacheHits    int64
	cacheMisses  int64
	cacheWrites  int64
	cacheDeletes int64

	jobsProcessed int64
	jobsFailed    int64
	jobsTotalTime float64

	startTime time.Time
}

type pathCounter struct {
	count    int64
	duration float64
	errors   int64
}

// New creates a new Pulse monitoring instance.
func New(cfg Config) *Pulse {
	cfg.setDefaults()
	return &Pulse{
		config:       cfg,
		requests:     newRingBuffer(cfg.MaxEntries),
		slowQueries:  newRingBuffer(cfg.MaxEntries / 2),
		exceptions:   newRingBuffer(cfg.MaxEntries / 4),
		jobs:         newRingBuffer(cfg.MaxEntries / 2),
		cacheOps:     newRingBuffer(cfg.MaxEntries / 4),
		mails:        newRingBuffer(cfg.MaxEntries / 4),
		statusCounts: make(map[int]int64),
		pathStats:    make(map[string]*pathCounter),
		startTime:    time.Now(),
	}
}

// ── Recording methods ───────────────────────────────────────────

// RecordRequest records an HTTP request.
func (p *Pulse) RecordRequest(method, path string, status int, duration time.Duration, err error) {
	entry := Entry{
		Type:      EntryRequest,
		Timestamp: time.Now(),
		Duration:  duration,
		Data: map[string]any{
			"method": method,
			"path":   path,
			"status": status,
		},
	}
	if err != nil {
		entry.Data["error"] = err.Error()
	}
	p.requests.Add(entry)

	p.mu.Lock()
	p.totalRequests++
	dMs := float64(duration) / float64(time.Millisecond)
	p.totalDuration += dMs
	p.durations = append(p.durations, dMs)
	// Keep durations buffer bounded
	if len(p.durations) > p.config.MaxEntries {
		p.durations = p.durations[len(p.durations)-p.config.MaxEntries:]
	}
	p.statusCounts[status]++
	if status >= 500 {
		p.totalErrors++
	}
	if duration > p.config.SlowRequestThreshold {
		p.slowReqCount++
	}

	pc := p.pathStats[path]
	if pc == nil {
		pc = &pathCounter{}
		p.pathStats[path] = pc
	}
	pc.count++
	pc.duration += dMs
	if status >= 400 {
		pc.errors++
	}
	p.mu.Unlock()
}

// RecordSlowQuery records a slow database query.
func (p *Pulse) RecordSlowQuery(query string, duration time.Duration, args ...any) {
	p.slowQueries.Add(Entry{
		Type:      EntrySlowQuery,
		Timestamp: time.Now(),
		Duration:  duration,
		Data: map[string]any{
			"query": query,
			"args":  args,
		},
	})
}

// RecordException records an application error.
func (p *Pulse) RecordException(err error, context map[string]any) {
	data := map[string]any{"error": err.Error()}
	for k, v := range context {
		data[k] = v
	}
	p.exceptions.Add(Entry{
		Type:      EntryException,
		Timestamp: time.Now(),
		Data:      data,
	})
}

// RecordJob records a queued job execution.
func (p *Pulse) RecordJob(name string, duration time.Duration, err error) {
	data := map[string]any{"name": name}
	if err != nil {
		data["error"] = err.Error()
		data["status"] = "failed"
	} else {
		data["status"] = "completed"
	}

	p.jobs.Add(Entry{
		Type:      EntryJob,
		Timestamp: time.Now(),
		Duration:  duration,
		Data:      data,
	})

	p.mu.Lock()
	p.jobsProcessed++
	p.jobsTotalTime += float64(duration) / float64(time.Millisecond)
	if err != nil {
		p.jobsFailed++
	}
	p.mu.Unlock()
}

// RecordCacheHit records a cache hit.
func (p *Pulse) RecordCacheHit(key string) {
	p.cacheOps.Add(Entry{
		Type:      EntryCache,
		Timestamp: time.Now(),
		Data:      map[string]any{"key": key, "op": "hit"},
	})
	p.mu.Lock()
	p.cacheHits++
	p.mu.Unlock()
}

// RecordCacheMiss records a cache miss.
func (p *Pulse) RecordCacheMiss(key string) {
	p.cacheOps.Add(Entry{
		Type:      EntryCache,
		Timestamp: time.Now(),
		Data:      map[string]any{"key": key, "op": "miss"},
	})
	p.mu.Lock()
	p.cacheMisses++
	p.mu.Unlock()
}

// RecordCacheWrite records a cache write.
func (p *Pulse) RecordCacheWrite(key string) {
	p.mu.Lock()
	p.cacheWrites++
	p.mu.Unlock()
}

// RecordCacheDelete records a cache deletion.
func (p *Pulse) RecordCacheDelete(key string) {
	p.mu.Lock()
	p.cacheDeletes++
	p.mu.Unlock()
}

// RecordMail records a sent email.
func (p *Pulse) RecordMail(to, subject string, success bool) {
	data := map[string]any{
		"to":      to,
		"subject": subject,
		"success": success,
	}
	p.mails.Add(Entry{
		Type:      EntryMail,
		Timestamp: time.Now(),
		Data:      data,
	})
}

// ── Stats computation ───────────────────────────────────────────

// GetRequestStats returns aggregated request statistics.
func (p *Pulse) GetRequestStats() RequestStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := RequestStats{
		TotalRequests: p.totalRequests,
		TotalErrors:   p.totalErrors,
		SlowRequests:  p.slowReqCount,
		StatusCounts:  make(map[int]int64),
	}

	for k, v := range p.statusCounts {
		stats.StatusCounts[k] = v
	}

	if p.totalRequests > 0 {
		stats.AvgDuration = p.totalDuration / float64(p.totalRequests)
	}

	elapsed := time.Since(p.startTime).Minutes()
	if elapsed > 0 {
		stats.RequestsPerMin = float64(p.totalRequests) / elapsed
	}

	// Calculate percentiles
	if len(p.durations) > 0 {
		sorted := make([]float64, len(p.durations))
		copy(sorted, p.durations)
		sortFloat64s(sorted)
		stats.P50Duration = percentile(sorted, 0.50)
		stats.P95Duration = percentile(sorted, 0.95)
		stats.P99Duration = percentile(sorted, 0.99)
	}

	// Top paths by request count
	type ps struct {
		path string
		pc   *pathCounter
	}
	var paths []ps
	for path, pc := range p.pathStats {
		paths = append(paths, ps{path, pc})
	}
	// Sort by count descending (simple insertion sort for small N)
	for i := 1; i < len(paths); i++ {
		for j := i; j > 0 && paths[j].pc.count > paths[j-1].pc.count; j-- {
			paths[j], paths[j-1] = paths[j-1], paths[j]
		}
	}
	limit := 10
	if len(paths) < limit {
		limit = len(paths)
	}
	for _, ps := range paths[:limit] {
		stats.TopPaths = append(stats.TopPaths, PathStat{
			Path:        ps.path,
			Count:       ps.pc.count,
			AvgDuration: ps.pc.duration / float64(ps.pc.count),
			ErrorCount:  ps.pc.errors,
		})
	}

	return stats
}

// GetCacheStats returns cache hit/miss statistics.
func (p *Pulse) GetCacheStats() CacheStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := CacheStats{
		Hits:    p.cacheHits,
		Misses:  p.cacheMisses,
		Writes:  p.cacheWrites,
		Deletes: p.cacheDeletes,
	}
	total := p.cacheHits + p.cacheMisses
	if total > 0 {
		stats.HitRate = float64(p.cacheHits) / float64(total)
	}
	return stats
}

// GetQueueStats returns queue job statistics.
func (p *Pulse) GetQueueStats() QueueStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := QueueStats{
		Processed: p.jobsProcessed,
		Failed:    p.jobsFailed,
	}
	if p.jobsProcessed > 0 {
		stats.AvgTime = p.jobsTotalTime / float64(p.jobsProcessed)
	}
	return stats
}

// RecentExceptions returns the most recent exceptions.
func (p *Pulse) RecentExceptions(n int) []Entry {
	return p.exceptions.Recent(n)
}

// RecentSlowQueries returns the most recent slow queries.
func (p *Pulse) RecentSlowQueries(n int) []Entry {
	return p.slowQueries.Recent(n)
}

// RecentRequests returns the most recent requests.
func (p *Pulse) RecentRequests(n int) []Entry {
	return p.requests.Recent(n)
}

// ── Middleware ───────────────────────────────────────────────────

// Middleware returns a router middleware that automatically records request metrics.
func (p *Pulse) Middleware() router.Middleware {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *nhttp.Context) error {
			start := time.Now()

			// Wrap response writer to capture status code
			sw := &statusWriter{ResponseWriter: c.Response, status: 200}
			c.Response = sw

			err := next(c)

			duration := time.Since(start)
			p.RecordRequest(
				c.Request.Method,
				c.Request.URL.Path,
				sw.status,
				duration,
				err,
			)

			if err != nil {
				p.RecordException(err, map[string]any{
					"method": c.Request.Method,
					"path":   c.Request.URL.Path,
				})
			}

			return err
		}
	}
}

type statusWriter struct {
	nhttp.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.status = code
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.wroteHeader = true
	}
	return w.ResponseWriter.Write(b)
}

// ── Dashboard Handler ───────────────────────────────────────────

// DashboardHandler returns a handler that serves the Pulse monitoring dashboard
// as a JSON API. Frontend dashboards can consume this.
func (p *Pulse) DashboardHandler() router.HandlerFunc {
	return func(c *nhttp.Context) error {
		data := map[string]any{
			"requests":     p.GetRequestStats(),
			"cache":        p.GetCacheStats(),
			"queue":        p.GetQueueStats(),
			"exceptions":   p.RecentExceptions(20),
			"slow_queries": p.RecentSlowQueries(20),
			"uptime":       time.Since(p.startTime).String(),
		}
		out, _ := json.MarshalIndent(data, "", "  ")
		c.Response.Header().Set("Content-Type", "application/json")
		c.Response.WriteHeader(200)
		_, err := c.Response.Write(out)
		return err
	}
}

// ── Helpers ─────────────────────────────────────────────────────

func sortFloat64s(a []float64) {
	// Simple quicksort — avoids importing sort for a hot-path utility
	if len(a) < 2 {
		return
	}
	pivot := a[len(a)/2]
	left, right := 0, len(a)-1
	for left <= right {
		for a[left] < pivot {
			left++
		}
		for a[right] > pivot {
			right--
		}
		if left <= right {
			a[left], a[right] = a[right], a[left]
			left++
			right--
		}
	}
	if right > 0 {
		sortFloat64s(a[:right+1])
	}
	if left < len(a) {
		sortFloat64s(a[left:])
	}
}

func percentile(sorted []float64, pct float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := pct * float64(len(sorted)-1)
	lower := int(math.Floor(idx))
	upper := int(math.Ceil(idx))
	if lower == upper {
		return sorted[lower]
	}
	frac := idx - float64(lower)
	return sorted[lower]*(1-frac) + sorted[upper]*frac
}
