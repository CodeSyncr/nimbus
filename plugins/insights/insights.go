// Package insights lets whoever runs a Nimbus app see how it is doing
// from the inside: memory and the garbage collector, each route's
// timings, slow and repeated database queries, errors with their stacks,
// what holds the heap, what the goroutines wait on, and who visits.
//
// One line turns it on, before the routes are registered:
//
//	insights.Install(app)
//
// What it collects is read over one endpoint (/_nimbus/insights by
// default) by a caller that knows the app's secret, given in
// NIMBUS_INSIGHTS_TOKEN. Without a secret nothing is collected and the
// endpoint does not exist: an app nobody watches pays nothing.
//
// It is made to be read by a control plane (Nimbus Cloud reads it for its
// Insights panel and for its assistant), not opened in a browser.
package insights

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/database"
	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/lucid"
)

// Config is how the collector is set up. The zero value is right for an
// app on Nimbus Cloud.
type Config struct {
	// Token is the secret a reader sends as "Authorization: Bearer …".
	// Empty takes NIMBUS_INSIGHTS_TOKEN; with neither, nothing is installed.
	Token string
	// Path is the endpoint. Default /_nimbus/insights.
	Path string
	// SlowQuery and SlowRequest are what counts as slow. Defaults 100ms
	// and 500ms.
	SlowQuery   time.Duration
	SlowRequest time.Duration
	// Sample is how often memory is read. Default 10s. Reading it stops
	// the program for a moment, so it is not done on every request.
	Sample time.Duration
	// History is how many samples are kept. Default 180 (half an hour).
	History int
	// Keep is how many slow requests, slow queries and errors are kept.
	// Default 200 of each.
	Keep int
	// TrafficDir is where each day's visits are written, so they outlive a
	// restart. Default storage/insights. "-" keeps them in memory only.
	TrafficDir string
}

func (c *Config) defaults() {
	if c.Token == "" {
		c.Token = strings.TrimSpace(os.Getenv("NIMBUS_INSIGHTS_TOKEN"))
	}
	if c.Path == "" {
		c.Path = "/_nimbus/insights"
	}
	if c.SlowQuery <= 0 {
		c.SlowQuery = 100 * time.Millisecond
	}
	if c.SlowRequest <= 0 {
		c.SlowRequest = 500 * time.Millisecond
	}
	if c.Sample <= 0 {
		c.Sample = 10 * time.Second
	}
	if c.History <= 0 {
		c.History = 180
	}
	if c.Keep <= 0 {
		c.Keep = 200
	}
	if c.TrafficDir == "" {
		c.TrafficDir = "storage/insights"
	}
}

// Insights is a running collector.
type Insights struct {
	cfg     Config
	started time.Time

	runtime  *runtimeLog
	requests *requestLog
	queries  *queryLog
	errors   *ring[ErrorEntry]
	traffic  *trafficLog

	stop     chan struct{}
	stopOnce sync.Once

	watched atomic.Pointer[lucid.DB]
	dbMu    sync.Mutex
}

// Install starts collecting and mounts the endpoint. Call it once, before
// the app's own middleware and routes are registered, so every request
// passes through it. It answers nil when there is no secret.
func Install(app *nimbus.App, cfg ...Config) *Insights {
	var c Config
	if len(cfg) > 0 {
		c = cfg[0]
	}
	in := New(c)
	if in == nil || app == nil || app.Router == nil {
		return in
	}
	app.Router.Use(in.Middleware())
	app.Router.Get(in.cfg.Path, in.Handler())
	return in
}

// New starts a collector without mounting anything: for an app that
// wires the middleware and the handler itself. Nil when there is no secret.
func New(cfg Config) *Insights {
	cfg.defaults()
	if cfg.Token == "" {
		return nil
	}
	in := &Insights{cfg: cfg, started: time.Now(), stop: make(chan struct{})}
	in.runtime = newRuntimeLog(cfg.History)
	in.requests = newRequestLog(cfg.Keep, cfg.SlowRequest)
	in.queries = newQueryLog(cfg.Keep, cfg.SlowQuery)
	in.errors = newRing[ErrorEntry](cfg.Keep)
	in.traffic = newTrafficLog(cfg.TrafficDir, cfg.Token)
	watch(in)
	in.watchDatabase()
	go in.loop()
	return in
}

// watchDatabase puts the query callbacks on the app's database, once it
// is connected. An app connects after it installs this, so it is tried
// again with each sample and each request until there is one; a database
// connected again later (another one) is watched too.
func (in *Insights) watchDatabase() {
	db := currentDB()
	if db == nil || in.watched.Load() == db {
		return
	}
	in.dbMu.Lock()
	defer in.dbMu.Unlock()
	if in.watched.Load() == db {
		return
	}
	hook(db)
	in.watched.Store(db)
}

func currentDB() (db *lucid.DB) {
	defer func() { _ = recover() }()
	return database.Get()
}

// Close stops the sampler and writes what the traffic log still holds.
func (in *Insights) Close() {
	if in == nil {
		return
	}
	in.stopOnce.Do(func() {
		close(in.stop)
		unwatch(in)
		in.traffic.flush()
	})
}

func (in *Insights) loop() {
	in.runtime.sample(in.requests)
	sample := time.NewTicker(in.cfg.Sample)
	// often: an app is stopped without being asked to say goodbye
	flush := time.NewTicker(15 * time.Second)
	defer sample.Stop()
	defer flush.Stop()
	for {
		select {
		case <-in.stop:
			return
		case <-sample.C:
			in.watchDatabase()
			in.runtime.sample(in.requests)
		case <-flush.C:
			in.traffic.flush()
		}
	}
}

// Sections are what the endpoint answers, by ?section=.
var Sections = []string{"overview", "runtime", "heap", "goroutines", "requests", "queries", "errors", "traffic"}

// Report is the endpoint's answer. Only the sections asked for are set.
type Report struct {
	App        AppInfo          `json:"app"`
	Overview   *Overview        `json:"overview,omitempty"`
	Runtime    *RuntimeReport   `json:"runtime,omitempty"`
	Heap       *HeapReport      `json:"heap,omitempty"`
	Goroutines *GoroutineReport `json:"goroutines,omitempty"`
	Requests   *RequestReport   `json:"requests,omitempty"`
	Queries    *QueryReport     `json:"queries,omitempty"`
	Errors     []ErrorEntry     `json:"errors,omitempty"`
	Traffic    *TrafficReport   `json:"traffic,omitempty"`
}

// AppInfo says which program answered.
type AppInfo struct {
	Version   string    `json:"insights"`
	Go        string    `json:"go"`
	Env       string    `json:"env"`
	StartedAt time.Time `json:"started_at"`
	UptimeSec int64     `json:"uptime_sec"`
	Now       time.Time `json:"now"`
}

// Overview is the few numbers that say how the app is doing.
type Overview struct {
	RequestsPerMin float64 `json:"requests_per_min"`
	ErrorRate      float64 `json:"error_rate"` // share of requests answered 5xx, 0 to 1
	P50MS          float64 `json:"p50_ms"`
	P95MS          float64 `json:"p95_ms"`
	P99MS          float64 `json:"p99_ms"`
	HeapInuse      uint64  `json:"heap_inuse"`
	Sys            uint64  `json:"sys"`
	Goroutines     int     `json:"goroutines"`
	GCCPUFraction  float64 `json:"gc_cpu_fraction"`
	SlowQueries    int     `json:"slow_queries"`
	Errors         int     `json:"errors"`
	InFlight       int     `json:"in_flight"`
}

// Collect gathers the sections named (all of them for "all" or none).
func (in *Insights) Collect(sections ...string) *Report {
	want := map[string]bool{}
	for _, s := range sections {
		for _, one := range strings.Split(s, ",") {
			if one = strings.TrimSpace(strings.ToLower(one)); one != "" {
				want[one] = true
			}
		}
	}
	all := len(want) == 0 || want["all"]
	has := func(s string) bool { return all || want[s] }
	now := time.Now()
	r := &Report{App: AppInfo{Version: Version, Go: goVersion(), Env: os.Getenv("APP_ENV"), StartedAt: in.started, UptimeSec: int64(now.Sub(in.started).Seconds()), Now: now}}
	if has("overview") {
		rt := in.runtime.latest()
		rq := in.requests.totals()
		r.Overview = &Overview{RequestsPerMin: rq.perMin, ErrorRate: rq.errorRate, P50MS: rq.p50, P95MS: rq.p95, P99MS: rq.p99,
			HeapInuse: rt.HeapInuse, Sys: rt.Sys, Goroutines: rt.Goroutines, GCCPUFraction: rt.GCCPUFraction,
			SlowQueries: in.queries.slow.total(), Errors: in.errors.total(), InFlight: in.requests.inFlight()}
	}
	if has("runtime") {
		r.Runtime = in.runtime.report()
	}
	if has("heap") && !all { // a profile is asked for by name: it is not free
		r.Heap = heapReport(30)
	}
	if has("goroutines") && !all {
		r.Goroutines = goroutineReport(30)
	}
	if has("requests") {
		r.Requests = in.requests.report()
	}
	if has("queries") {
		r.Queries = in.queries.report()
	}
	if has("errors") {
		r.Errors = in.errors.newest(100)
	}
	if has("traffic") {
		days := 30
		r.Traffic = in.traffic.report(days)
	}
	return r
}

// Handler answers the endpoint: a Report as JSON, to a caller with the secret.
func (in *Insights) Handler() func(c *nhttp.Context) error {
	return func(c *nhttp.Context) error {
		w := c.Response
		given, bearer := strings.CutPrefix(c.Request.Header.Get("Authorization"), "Bearer ")
		if in == nil || !bearer || subtle.ConstantTimeCompare([]byte(given), []byte(in.cfg.Token)) != 1 {
			// as if there were nothing here
			http.NotFound(w, c.Request)
			return nil
		}
		q := c.Request.URL.Query()
		rep := in.Collect(q.Get("section"))
		if rep.Traffic != nil {
			if d, err := strconv.Atoi(q.Get("days")); err == nil && d > 0 && d <= 90 {
				rep.Traffic = in.traffic.report(d)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		return json.NewEncoder(w).Encode(rep)
	}
}

// Version is the shape of the report.
const Version = "1"

/* ── a ring of the newest entries ─────────────────────────────────────── */

type ring[T any] struct {
	mu    sync.Mutex
	items []T
	next  int
	full  bool
	count int
}

func newRing[T any](size int) *ring[T] { return &ring[T]{items: make([]T, size)} }

func (r *ring[T]) add(v T) {
	r.mu.Lock()
	r.items[r.next] = v
	r.next++
	r.count++
	if r.next == len(r.items) {
		r.next, r.full = 0, true
	}
	r.mu.Unlock()
}

// newest is up to n entries, newest first.
func (r *ring[T]) newest(n int) []T {
	r.mu.Lock()
	defer r.mu.Unlock()
	size := r.next
	if r.full {
		size = len(r.items)
	}
	if n > size {
		n = size
	}
	out := make([]T, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, r.items[(r.next-i+len(r.items))%len(r.items)])
	}
	return out
}

// total is how many were ever added.
func (r *ring[T]) total() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.count
}
