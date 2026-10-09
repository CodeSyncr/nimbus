package insights

import (
	"os"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"
)

func goVersion() string { return runtime.Version() }

// Sample is memory and the collector at one moment.
type Sample struct {
	At            time.Time `json:"at"`
	HeapAlloc     uint64    `json:"heap_alloc"`
	HeapInuse     uint64    `json:"heap_inuse"`
	HeapIdle      uint64    `json:"heap_idle"`
	HeapReleased  uint64    `json:"heap_released"`
	HeapSys       uint64    `json:"heap_sys"`
	HeapObjects   uint64    `json:"heap_objects"`
	StackInuse    uint64    `json:"stack_inuse"`
	Sys           uint64    `json:"sys"`
	NextGC        uint64    `json:"next_gc"`
	NumGC         uint32    `json:"num_gc"`
	PauseTotalNs  uint64    `json:"pause_total_ns"`
	GCCPUFraction float64   `json:"gc_cpu_fraction"`
	Mallocs       uint64    `json:"mallocs"`
	Frees         uint64    `json:"frees"`
	TotalAlloc    uint64    `json:"total_alloc"`
	Goroutines    int       `json:"goroutines"`
	// since the sample before: collections, time paused in them, bytes
	// allocated, requests answered and their slowest
	GCs       uint32  `json:"gcs"`
	PauseNs   uint64  `json:"pause_ns"`
	AllocRate uint64  `json:"alloc_rate"` // bytes a second
	Requests  int     `json:"requests"`
	Errors    int     `json:"errors"`
	P95MS     float64 `json:"p95_ms"`
}

// RuntimeReport is where memory and the collector stand, and how they got there.
type RuntimeReport struct {
	Now          Sample   `json:"now"`
	History      []Sample `json:"history"` // oldest first
	RecentPauses []uint64 `json:"recent_pauses_ns"`
	MaxPauseNs   uint64   `json:"max_pause_ns"`
	GOMAXPROCS   int      `json:"gomaxprocs"`
	NumCPU       int      `json:"num_cpu"`
	GOGC         string   `json:"gogc"`
	MemoryLimit  int64    `json:"memory_limit"` // bytes; 0 when none is set
	// ContainerLimit is the memory the container may use (its cgroup), 0 when unknown.
	ContainerLimit uint64 `json:"container_limit"`
}

type runtimeLog struct {
	mu      sync.Mutex
	samples []Sample
	size    int
	pauses  []uint64
}

func newRuntimeLog(size int) *runtimeLog { return &runtimeLog{size: size} }

func (l *runtimeLog) sample(rq *requestLog) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	s := Sample{At: time.Now(), HeapAlloc: m.HeapAlloc, HeapInuse: m.HeapInuse, HeapIdle: m.HeapIdle, HeapReleased: m.HeapReleased, HeapSys: m.HeapSys,
		HeapObjects: m.HeapObjects, StackInuse: m.StackInuse, Sys: m.Sys, NextGC: m.NextGC, NumGC: m.NumGC, PauseTotalNs: m.PauseTotalNs,
		GCCPUFraction: m.GCCPUFraction, Mallocs: m.Mallocs, Frees: m.Frees, TotalAlloc: m.TotalAlloc, Goroutines: runtime.NumGoroutine()}
	if rq != nil {
		s.Requests, s.Errors, s.P95MS = rq.window()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if n := len(l.samples); n > 0 {
		prev := l.samples[n-1]
		s.GCs = s.NumGC - prev.NumGC
		s.PauseNs = s.PauseTotalNs - prev.PauseTotalNs
		if secs := s.At.Sub(prev.At).Seconds(); secs > 0 && s.TotalAlloc >= prev.TotalAlloc {
			s.AllocRate = uint64(float64(s.TotalAlloc-prev.TotalAlloc) / secs)
		}
	}
	// the pauses of the collections since the last sample, newest last
	if s.GCs > 0 {
		n := int(s.GCs)
		if n > len(m.PauseNs) {
			n = len(m.PauseNs)
		}
		for i := n; i >= 1; i-- {
			l.pauses = append(l.pauses, m.PauseNs[(int(m.NumGC)-i+len(m.PauseNs))%len(m.PauseNs)])
		}
		if len(l.pauses) > 100 {
			l.pauses = l.pauses[len(l.pauses)-100:]
		}
	}
	l.samples = append(l.samples, s)
	if len(l.samples) > l.size {
		l.samples = l.samples[len(l.samples)-l.size:]
	}
}

func (l *runtimeLog) latest() Sample {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.samples) == 0 {
		return Sample{}
	}
	return l.samples[len(l.samples)-1]
}

func (l *runtimeLog) report() *RuntimeReport {
	l.mu.Lock()
	defer l.mu.Unlock()
	r := &RuntimeReport{History: append([]Sample(nil), l.samples...), RecentPauses: append([]uint64(nil), l.pauses...),
		GOMAXPROCS: runtime.GOMAXPROCS(0), NumCPU: runtime.NumCPU(), GOGC: os.Getenv("GOGC"), ContainerLimit: containerLimit()}
	if r.GOGC == "" {
		r.GOGC = "100"
	}
	if lim := debug.SetMemoryLimit(-1); lim > 0 && lim < 1<<62 {
		r.MemoryLimit = lim
	}
	if n := len(l.samples); n > 0 {
		r.Now = l.samples[n-1]
	}
	for _, p := range l.pauses {
		if p > r.MaxPauseNs {
			r.MaxPauseNs = p
		}
	}
	return r
}

// containerLimit reads the memory limit of the cgroup the program runs in.
func containerLimit() uint64 {
	for _, p := range []string{"/sys/fs/cgroup/memory.max", "/sys/fs/cgroup/memory/memory.limit_in_bytes"} {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		s := strings.TrimSpace(string(data))
		if s == "" || s == "max" {
			return 0
		}
		var n uint64
		for _, c := range s {
			if c < '0' || c > '9' {
				return 0
			}
			n = n*10 + uint64(c-'0')
		}
		if n > 1<<50 { // "no limit", as cgroup v1 writes it
			return 0
		}
		return n
	}
	return 0
}

/* ── what holds the heap, and what the goroutines wait on ─────────────── */

// Frame is a place in the program.
type Frame struct {
	Func string `json:"func"`
	File string `json:"file"`
	Line int    `json:"line"`
}

// HeapSite is memory held by what one place in the program allocated.
type HeapSite struct {
	InuseBytes   int64   `json:"inuse_bytes"`
	InuseObjects int64   `json:"inuse_objects"`
	AllocBytes   int64   `json:"alloc_bytes"` // since the program started
	At           Frame   `json:"at"`          // the first frame that is not the runtime's
	Stack        []Frame `json:"stack"`       // a few frames up from there
}

// HeapReport is the heap by who allocated it. The runtime samples
// allocations (one in about every 512 KB), so small holders are estimates.
type HeapReport struct {
	TotalInuseBytes int64      `json:"total_inuse_bytes"`
	Sites           []HeapSite `json:"sites"`
}

// ownFrame says a frame is the program's, not the runtime's and not this
// collector's own.
func ownFrame(fn, file string) bool {
	if fn == "" || strings.HasPrefix(fn, "runtime.") || strings.HasPrefix(fn, "runtime/") || strings.HasPrefix(fn, "internal/") || strings.HasPrefix(fn, "sync.") || strings.HasPrefix(fn, "syscall.") {
		return false
	}
	return !strings.Contains(fn, "plugins/insights.") || strings.HasSuffix(file, "_test.go")
}

func frames(stk []uintptr, max int) []Frame {
	var out []Frame
	it := runtime.CallersFrames(stk)
	for {
		f, more := it.Next()
		if ownFrame(f.Function, f.File) {
			out = append(out, Frame{Func: f.Function, File: shortFile(f.File), Line: f.Line})
			if len(out) >= max {
				break
			}
		}
		if !more {
			break
		}
	}
	return out
}

// shortFile is a source path without the machine it was built on: from
// the module's folder for a dependency, from the app's own for its code.
func shortFile(p string) string {
	if i := strings.Index(p, "/pkg/mod/"); i >= 0 {
		return p[i+len("/pkg/mod/"):]
	}
	if i := strings.Index(p, "/go/src/"); i >= 0 {
		return p[i+len("/go/src/"):]
	}
	if wd, err := os.Getwd(); err == nil && strings.HasPrefix(p, wd+"/") {
		return p[len(wd)+1:]
	}
	parts := strings.Split(p, "/")
	if len(parts) > 3 {
		return strings.Join(parts[len(parts)-3:], "/")
	}
	return p
}

func heapReport(top int) *HeapReport {
	// The profile is as of the last collection; one is run so that it is
	// as of now. That is why the heap is only read when it is asked for.
	runtime.GC()
	n, _ := runtime.MemProfile(nil, true)
	recs := make([]runtime.MemProfileRecord, n+50)
	n, ok := runtime.MemProfile(recs, true)
	if !ok {
		return &HeapReport{Sites: []HeapSite{}}
	}
	recs = recs[:n]
	type agg struct {
		site HeapSite
	}
	by := map[string]*agg{}
	rep := &HeapReport{Sites: []HeapSite{}}
	for i := range recs {
		r := &recs[i]
		inuse := r.InUseBytes()
		rep.TotalInuseBytes += inuse
		fs := frames(r.Stack(), 4)
		if len(fs) == 0 {
			continue
		}
		key := fs[0].Func + ":" + fs[0].File
		a := by[key]
		if a == nil {
			a = &agg{site: HeapSite{At: fs[0], Stack: fs}}
			by[key] = a
		}
		a.site.InuseBytes += inuse
		a.site.InuseObjects += r.InUseObjects()
		a.site.AllocBytes += r.AllocBytes
	}
	for _, a := range by {
		if a.site.InuseBytes > 0 || a.site.AllocBytes > 0 {
			rep.Sites = append(rep.Sites, a.site)
		}
	}
	sort.Slice(rep.Sites, func(i, j int) bool {
		if rep.Sites[i].InuseBytes != rep.Sites[j].InuseBytes {
			return rep.Sites[i].InuseBytes > rep.Sites[j].InuseBytes
		}
		return rep.Sites[i].AllocBytes > rep.Sites[j].AllocBytes
	})
	if len(rep.Sites) > top {
		rep.Sites = rep.Sites[:top]
	}
	return rep
}

// GoroutineGroup is goroutines that are in the same place.
type GoroutineGroup struct {
	Count int     `json:"count"`
	At    Frame   `json:"at"`
	Stack []Frame `json:"stack"`
}

// GoroutineReport is the goroutines by where they are: a group that keeps
// growing is a leak.
type GoroutineReport struct {
	Total  int              `json:"total"`
	Groups []GoroutineGroup `json:"groups"`
}

func goroutineReport(top int) *GoroutineReport {
	n := runtime.NumGoroutine()
	recs := make([]runtime.StackRecord, n+50)
	n, ok := runtime.GoroutineProfile(recs)
	rep := &GoroutineReport{Total: runtime.NumGoroutine(), Groups: []GoroutineGroup{}}
	if !ok {
		return rep
	}
	by := map[string]*GoroutineGroup{}
	for i := 0; i < n; i++ {
		stk := recs[i].Stack()
		fs := frames(stk, 5)
		if len(fs) == 0 {
			// all of it in the runtime: say where
			it := runtime.CallersFrames(stk)
			f, _ := it.Next()
			fs = []Frame{{Func: f.Function, File: shortFile(f.File), Line: f.Line}}
		}
		key := fs[0].Func + ":" + fs[0].File + ":" + itoa(fs[0].Line)
		g := by[key]
		if g == nil {
			g = &GoroutineGroup{At: fs[0], Stack: fs}
			by[key] = g
		}
		g.Count++
	}
	for _, g := range by {
		rep.Groups = append(rep.Groups, *g)
	}
	sort.Slice(rep.Groups, func(i, j int) bool {
		if rep.Groups[i].Count != rep.Groups[j].Count {
			return rep.Groups[i].Count > rep.Groups[j].Count
		}
		return rep.Groups[i].At.Func < rep.Groups[j].At.Func
	})
	if len(rep.Groups) > top {
		rep.Groups = rep.Groups[:top]
	}
	return rep
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
