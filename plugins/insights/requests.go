package insights

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"net/http"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"

	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

// RouteStat is how one route has been answering.
type RouteStat struct {
	Route   string  `json:"route"` // "GET /posts/:id"
	Count   int     `json:"count"`
	Errors  int     `json:"errors"` // answered 5xx
	AvgMS   float64 `json:"avg_ms"`
	P95MS   float64 `json:"p95_ms"`
	MaxMS   float64 `json:"max_ms"`
	TotalMS float64 `json:"total_ms"`
	// what its requests asked of the database, on average
	Queries float64   `json:"queries"`
	QueryMS float64   `json:"query_ms"`
	LastAt  time.Time `json:"last_at"`
}

// SlowRequest is one request that took long.
type SlowRequest struct {
	At      time.Time `json:"at"`
	Method  string    `json:"method"`
	Path    string    `json:"path"`
	Route   string    `json:"route"`
	Status  int       `json:"status"`
	MS      float64   `json:"ms"`
	Queries int       `json:"queries"`
	QueryMS float64   `json:"query_ms"`
}

// Repeated is one query run many times while answering one request: the
// mark of a loop that asks the database once per row (N+1).
type Repeated struct {
	Route  string    `json:"route"`
	Shape  string    `json:"shape"`  // the query with its values taken out
	Times  int       `json:"times"`  // the most it ran in one request
	Seen   int       `json:"seen"`   // in how many requests
	Caller string    `json:"caller"` // where in the app it is run from
	LastAt time.Time `json:"last_at"`
}

// RequestReport is the routes, the slow requests and the repeated queries.
type RequestReport struct {
	Total    int            `json:"total"`
	Routes   []RouteStat    `json:"routes"` // most time spent first
	Slow     []SlowRequest  `json:"slow"`   // newest first
	Status   map[string]int `json:"status"` // "2xx", "3xx", "4xx", "5xx"
	Repeated []Repeated     `json:"repeated"`
	SlowMS   float64        `json:"slow_ms"` // what counts as slow
}

// ErrorEntry is an error a handler returned, a panic, or a 5xx answer.
type ErrorEntry struct {
	At      time.Time `json:"at"`
	Method  string    `json:"method"`
	Path    string    `json:"path"`
	Route   string    `json:"route"`
	Status  int       `json:"status"`
	Kind    string    `json:"kind"` // error | panic | status
	Message string    `json:"message"`
	Stack   string    `json:"stack,omitempty"`
}

type routeAgg struct {
	count, errors int
	total, max    float64
	recent        []float64 // the last durations, for the 95th
	next          int
	queries       int
	queryMS       float64
	last          time.Time
}

// track is one request while it is being answered.
type track struct {
	queries int
	queryMS float64
	shapes  map[string]*shapeHit
}

type shapeHit struct {
	times  int
	caller string
}

type requestLog struct {
	mu       sync.Mutex
	routes   map[string]*routeAgg
	status   map[string]int
	total    int
	slow     *ring[SlowRequest]
	repeated map[string]*Repeated
	slowAt   time.Duration
	recent   []float64 // the last durations of any route
	rnext    int
	// since the last sample
	wCount, wErrors int
	wDur            []float64
	// the last minutes, a count each
	minutes [15]int
	minute  int64

	active sync.Map // goroutine id -> *track
	flying atomic.Int64
}

func newRequestLog(keep int, slow time.Duration) *requestLog {
	return &requestLog{routes: map[string]*routeAgg{}, status: map[string]int{}, slow: newRing[SlowRequest](keep), repeated: map[string]*Repeated{}, slowAt: slow}
}

// goid is the id of the goroutine it is called on. A query is run on the
// goroutine of the request that asked for it, which is how the two are
// brought together without the app passing anything along.
func goid() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	// "goroutine 123 [running]:"
	var id uint64
	for _, c := range buf[10:n] {
		if c < '0' || c > '9' {
			break
		}
		id = id*10 + uint64(c-'0')
	}
	return id
}

func (l *requestLog) inFlight() int { return int(l.flying.Load()) }

// current is the request being answered on this goroutine, if any.
func (l *requestLog) current() *track {
	if l.flying.Load() == 0 {
		return nil
	}
	if v, ok := l.active.Load(goid()); ok {
		return v.(*track)
	}
	return nil
}

func percentile(vals []float64, p float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	s := append([]float64(nil), vals...)
	sort.Float64s(s)
	i := int(float64(len(s))*p + 0.5)
	if i >= len(s) {
		i = len(s) - 1
	}
	if i < 0 {
		i = 0
	}
	return s[i]
}

func (l *requestLog) record(method, path, route string, status int, d time.Duration, t *track) {
	ms := float64(d.Microseconds()) / 1000
	now := time.Now()
	key := method + " " + route
	l.mu.Lock()
	a := l.routes[key]
	if a == nil {
		if len(l.routes) >= 400 { // an app with no end of routes: the rest are one
			key = method + " (other)"
			a = l.routes[key]
		}
		if a == nil {
			a = &routeAgg{recent: make([]float64, 0, 128)}
			l.routes[key] = a
		}
	}
	a.count++
	a.total += ms
	if ms > a.max {
		a.max = ms
	}
	if len(a.recent) < 128 {
		a.recent = append(a.recent, ms)
	} else {
		a.recent[a.next%128] = ms
		a.next++
	}
	a.last = now
	if t != nil {
		a.queries += t.queries
		a.queryMS += t.queryMS
	}
	if status >= 500 {
		a.errors++
		l.wErrors++
	}
	l.total++
	l.status[fmt.Sprintf("%dxx", status/100)]++
	if len(l.recent) < 1000 {
		l.recent = append(l.recent, ms)
	} else {
		l.recent[l.rnext%1000] = ms
		l.rnext++
	}
	l.wCount++
	if len(l.wDur) < 2000 {
		l.wDur = append(l.wDur, ms)
	}
	if m := now.Unix() / 60; m != l.minute {
		for k := l.minute + 1; k <= m && k-l.minute <= int64(len(l.minutes)); k++ {
			l.minutes[k%int64(len(l.minutes))] = 0
		}
		l.minute = m
	}
	l.minutes[l.minute%int64(len(l.minutes))]++
	// a query run again and again in this one request
	if t != nil {
		for shape, hit := range t.shapes {
			if hit.times < 5 {
				continue
			}
			rk := key + "\x00" + shape
			r := l.repeated[rk]
			if r == nil {
				if len(l.repeated) >= 200 {
					continue
				}
				r = &Repeated{Route: key, Shape: shape, Caller: hit.caller}
				l.repeated[rk] = r
			}
			if hit.times > r.Times {
				r.Times = hit.times
			}
			r.Seen++
			r.LastAt = now
		}
	}
	l.mu.Unlock()
	if d >= l.slowAt {
		s := SlowRequest{At: now, Method: method, Path: path, Route: route, Status: status, MS: ms}
		if t != nil {
			s.Queries, s.QueryMS = t.queries, t.queryMS
		}
		l.slow.add(s)
	}
}

// window answers what happened since it was last asked, for a sample.
func (l *requestLog) window() (count, errs int, p95 float64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	count, errs, p95 = l.wCount, l.wErrors, percentile(l.wDur, 0.95)
	l.wCount, l.wErrors, l.wDur = 0, 0, l.wDur[:0]
	return
}

type totals struct {
	perMin, errorRate, p50, p95, p99 float64
}

func (l *requestLog) totals() totals {
	l.mu.Lock()
	defer l.mu.Unlock()
	var t totals
	// the five whole minutes before this one
	now := time.Now().Unix() / 60
	sum, n := 0, 0
	for k := int64(1); k <= 5; k++ {
		m := now - k
		if l.minute-m < int64(len(l.minutes)) && m <= l.minute {
			sum += l.minutes[m%int64(len(l.minutes))]
		}
		n++
	}
	t.perMin = float64(sum) / float64(n)
	if l.total > 0 {
		t.errorRate = float64(l.status["5xx"]) / float64(l.total)
	}
	t.p50, t.p95, t.p99 = percentile(l.recent, 0.50), percentile(l.recent, 0.95), percentile(l.recent, 0.99)
	return t
}

func (l *requestLog) report() *RequestReport {
	l.mu.Lock()
	rep := &RequestReport{Total: l.total, Status: map[string]int{}, Routes: make([]RouteStat, 0, len(l.routes)), Repeated: make([]Repeated, 0, len(l.repeated)), SlowMS: float64(l.slowAt.Milliseconds())}
	for k, v := range l.status {
		rep.Status[k] = v
	}
	for key, a := range l.routes {
		s := RouteStat{Route: key, Count: a.count, Errors: a.errors, TotalMS: a.total, MaxMS: a.max, P95MS: percentile(a.recent, 0.95), LastAt: a.last}
		if a.count > 0 {
			s.AvgMS = a.total / float64(a.count)
			s.Queries = float64(a.queries) / float64(a.count)
			s.QueryMS = a.queryMS / float64(a.count)
		}
		rep.Routes = append(rep.Routes, s)
	}
	for _, r := range l.repeated {
		rep.Repeated = append(rep.Repeated, *r)
	}
	l.mu.Unlock()
	sort.Slice(rep.Routes, func(i, j int) bool { return rep.Routes[i].TotalMS > rep.Routes[j].TotalMS })
	if len(rep.Routes) > 60 {
		rep.Routes = rep.Routes[:60]
	}
	sort.Slice(rep.Repeated, func(i, j int) bool {
		if rep.Repeated[i].Times != rep.Repeated[j].Times {
			return rep.Repeated[i].Times > rep.Repeated[j].Times
		}
		return rep.Repeated[i].Shape < rep.Repeated[j].Shape
	})
	if len(rep.Repeated) > 40 {
		rep.Repeated = rep.Repeated[:40]
	}
	rep.Slow = rep.slowOrEmpty(l.slow.newest(60))
	return rep
}

func (r *RequestReport) slowOrEmpty(s []SlowRequest) []SlowRequest {
	if s == nil {
		return []SlowRequest{}
	}
	return s
}

/* ── the middleware ───────────────────────────────────────────────────── */

// answer wraps what a handler writes to, to learn the status and the
// kind of what was written. It stays a Flusher and a Hijacker, so
// streaming answers and WebSockets pass through it untouched.
type answer struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (a *answer) WriteHeader(code int) {
	if !a.wrote {
		a.status, a.wrote = code, true
	}
	a.ResponseWriter.WriteHeader(code)
}

func (a *answer) Write(b []byte) (int, error) {
	if !a.wrote {
		a.status, a.wrote = http.StatusOK, true
	}
	return a.ResponseWriter.Write(b)
}

func (a *answer) Flush() {
	if f, ok := a.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (a *answer) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := a.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, errors.New("insights: the connection cannot be hijacked")
}

func (a *answer) Unwrap() http.ResponseWriter { return a.ResponseWriter }

// Middleware measures every request that passes through it.
func (in *Insights) Middleware() router.Middleware {
	return func(next router.HandlerFunc) router.HandlerFunc {
		if in == nil {
			return next
		}
		return func(c *nhttp.Context) (err error) {
			req := c.Request
			if req.URL.Path == in.cfg.Path {
				return next(c) // reading the numbers is not one of them
			}
			if in.watched.Load() == nil {
				in.watchDatabase()
			}
			start := time.Now()
			w := &answer{ResponseWriter: c.Response, status: http.StatusOK}
			c.Response = w
			t := &track{shapes: map[string]*shapeHit{}}
			id := goid()
			in.requests.active.Store(id, t)
			in.requests.flying.Add(1)
			defer func() {
				in.requests.active.Delete(id)
				in.requests.flying.Add(-1)
				route := ""
				if rc := chi.RouteContext(req.Context()); rc != nil {
					route = rc.RoutePattern()
				}
				rec := recover()
				status := w.status
				if route == "" {
					// no route of the app's: every address nobody answers is one line, not its own
					route = req.URL.Path
					if status == http.StatusNotFound {
						route = "(no route)"
					}
				}
				switch {
				case rec != nil:
					status = http.StatusInternalServerError
					in.errors.add(ErrorEntry{At: time.Now(), Method: req.Method, Path: req.URL.Path, Route: route, Status: status, Kind: "panic",
						Message: clipText(fmt.Sprint(rec), 500), Stack: clipText(string(debug.Stack()), 6000)})
				case err != nil:
					if !w.wrote {
						status = http.StatusInternalServerError
					}
					in.errors.add(ErrorEntry{At: time.Now(), Method: req.Method, Path: req.URL.Path, Route: route, Status: status, Kind: "error", Message: clipText(err.Error(), 500)})
				case status >= 500:
					in.errors.add(ErrorEntry{At: time.Now(), Method: req.Method, Path: req.URL.Path, Route: route, Status: status, Kind: "status", Message: http.StatusText(status)})
				}
				in.requests.record(req.Method, req.URL.Path, route, status, time.Since(start), t)
				in.traffic.visit(req, status, w.Header().Get("Content-Type"))
				if rec != nil {
					panic(rec) // the app's own recovery still answers it
				}
			}()
			return next(c)
		}
	}
}

func clipText(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
