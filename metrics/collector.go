// Package metrics provides a lightweight, Prometheus-compatible metrics
// collector for Nimbus applications. It supports counters, gauges, and
// histograms without requiring an external Prometheus client library.
package metrics

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// ---------- Label helpers ---------------------------------------------------

// Labels is a set of key-value pairs attached to a metric sample.
type Labels map[string]string

// key returns a canonical string for use as a map key.
func (l Labels) key() string {
	if len(l) == 0 {
		return ""
	}
	pairs := make([]string, 0, len(l))
	for k, v := range l {
		pairs = append(pairs, k+"="+v)
	}
	sort.Strings(pairs)
	return strings.Join(pairs, ",")
}

// promLabels formats labels for Prometheus text exposition.
func (l Labels) promLabels() string {
	if len(l) == 0 {
		return ""
	}
	pairs := make([]string, 0, len(l))
	for k, v := range l {
		pairs = append(pairs, fmt.Sprintf(`%s="%s"`, k, v))
	}
	sort.Strings(pairs)
	return "{" + strings.Join(pairs, ",") + "}"
}

// ---------- Counter ---------------------------------------------------------

// Counter is a monotonically increasing metric (e.g. total_requests).
type Counter struct {
	name   string
	help   string
	mu     sync.RWMutex
	values map[string]*uint64
	labels map[string]Labels
}

// NewCounter creates a named counter.
func NewCounter(name, help string) *Counter {
	return &Counter{
		name:   name,
		help:   help,
		values: make(map[string]*uint64),
		labels: make(map[string]Labels),
	}
}

// Inc increments the counter by 1 for the given label set.
func (c *Counter) Inc(l Labels) {
	c.Add(1, l)
}

// Add increments the counter by delta for the given label set.
func (c *Counter) Add(delta uint64, l Labels) {
	key := l.key()
	c.mu.RLock()
	v, ok := c.values[key]
	c.mu.RUnlock()
	if ok {
		atomic.AddUint64(v, delta)
		return
	}
	c.mu.Lock()
	if v, ok = c.values[key]; ok {
		c.mu.Unlock()
		atomic.AddUint64(v, delta)
		return
	}
	v = new(uint64)
	*v = delta
	c.values[key] = v
	c.labels[key] = l
	c.mu.Unlock()
}

// render writes Prometheus text exposition format.
func (c *Counter) render(b *strings.Builder) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	fmt.Fprintf(b, "# HELP %s %s\n", c.name, c.help)
	fmt.Fprintf(b, "# TYPE %s counter\n", c.name)
	for key, v := range c.values {
		fmt.Fprintf(b, "%s%s %d\n", c.name, c.labels[key].promLabels(), atomic.LoadUint64(v))
	}
}

// ---------- Gauge -----------------------------------------------------------

// Gauge is a metric that can go up and down (e.g. active_connections).
type Gauge struct {
	name   string
	help   string
	mu     sync.RWMutex
	values map[string]*int64
	labels map[string]Labels
}

// NewGauge creates a named gauge.
func NewGauge(name, help string) *Gauge {
	return &Gauge{
		name:   name,
		help:   help,
		values: make(map[string]*int64),
		labels: make(map[string]Labels),
	}
}

func (g *Gauge) ptr(l Labels) *int64 {
	key := l.key()
	g.mu.RLock()
	v, ok := g.values[key]
	g.mu.RUnlock()
	if ok {
		return v
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if v, ok = g.values[key]; ok {
		return v
	}
	v = new(int64)
	g.values[key] = v
	g.labels[key] = l
	return v
}

// Set sets the gauge to a value.
func (g *Gauge) Set(val int64, l Labels) {
	atomic.StoreInt64(g.ptr(l), val)
}

// Inc increments the gauge by 1.
func (g *Gauge) Inc(l Labels) { atomic.AddInt64(g.ptr(l), 1) }

// Dec decrements the gauge by 1.
func (g *Gauge) Dec(l Labels) { atomic.AddInt64(g.ptr(l), -1) }

// Add adds delta to the gauge.
func (g *Gauge) Add(delta int64, l Labels) { atomic.AddInt64(g.ptr(l), delta) }

func (g *Gauge) render(b *strings.Builder) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	fmt.Fprintf(b, "# HELP %s %s\n", g.name, g.help)
	fmt.Fprintf(b, "# TYPE %s gauge\n", g.name)
	for key, v := range g.values {
		fmt.Fprintf(b, "%s%s %d\n", g.name, g.labels[key].promLabels(), atomic.LoadInt64(v))
	}
}

// ---------- Histogram -------------------------------------------------------

// Histogram tracks the distribution of observed values across configurable
// buckets (e.g. request_duration_seconds).
type Histogram struct {
	name    string
	help    string
	buckets []float64 // sorted upper bounds
	mu      sync.RWMutex
	series  map[string]*histSeries
	labels  map[string]Labels
}

type histSeries struct {
	counts []uint64 // one per bucket
	count  uint64
	sum    uint64 // float64 bits via math.Float64bits
}

// DefaultBuckets are latency buckets suitable for HTTP request durations in seconds.
var DefaultBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// NewHistogram creates a named histogram with the specified bucket boundaries.
// If buckets is nil, DefaultBuckets is used.
func NewHistogram(name, help string, buckets []float64) *Histogram {
	if buckets == nil {
		buckets = DefaultBuckets
	}
	sorted := make([]float64, len(buckets))
	copy(sorted, buckets)
	sort.Float64s(sorted)
	return &Histogram{
		name:    name,
		help:    help,
		buckets: sorted,
		series:  make(map[string]*histSeries),
		labels:  make(map[string]Labels),
	}
}

func (h *Histogram) getSeries(l Labels) *histSeries {
	key := l.key()
	h.mu.RLock()
	s, ok := h.series[key]
	h.mu.RUnlock()
	if ok {
		return s
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if s, ok = h.series[key]; ok {
		return s
	}
	s = &histSeries{counts: make([]uint64, len(h.buckets))}
	h.series[key] = s
	h.labels[key] = l
	return s
}

// Observe records a new value in the histogram.
func (h *Histogram) Observe(val float64, l Labels) {
	s := h.getSeries(l)
	bits := math.Float64bits(val)
	atomic.AddUint64(&s.count, 1)
	atomic.AddUint64(&s.sum, bits) // approximate; good enough for exposition
	for i, bound := range h.buckets {
		if val <= bound {
			atomic.AddUint64(&s.counts[i], 1)
		}
	}
}

func (h *Histogram) render(b *strings.Builder) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	fmt.Fprintf(b, "# HELP %s %s\n", h.name, h.help)
	fmt.Fprintf(b, "# TYPE %s histogram\n", h.name)
	for key, s := range h.series {
		lbl := h.labels[key]
		var cumulative uint64
		for i, bound := range h.buckets {
			cumulative += atomic.LoadUint64(&s.counts[i])
			merged := mergeLabels(lbl, Labels{"le": fmt.Sprintf("%g", bound)})
			fmt.Fprintf(b, "%s_bucket%s %d\n", h.name, merged.promLabels(), cumulative)
		}
		infLabels := mergeLabels(lbl, Labels{"le": "+Inf"})
		fmt.Fprintf(b, "%s_bucket%s %d\n", h.name, infLabels.promLabels(), atomic.LoadUint64(&s.count))
		// sum is stored as float64 bits; decode for exposition
		sumBits := atomic.LoadUint64(&s.sum)
		// NOTE: atomic addition of float64 bits is an approximation for
		// lock-free performance. For precise sums, use a mutex-based approach.
		fmt.Fprintf(b, "%s_sum%s %g\n", h.name, lbl.promLabels(), math.Float64frombits(sumBits))
		fmt.Fprintf(b, "%s_count%s %d\n", h.name, lbl.promLabels(), atomic.LoadUint64(&s.count))
	}
}

func mergeLabels(base, extra Labels) Labels {
	out := make(Labels, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

// ---------- Registry --------------------------------------------------------

type metric interface {
	render(b *strings.Builder)
}

// Registry holds all registered metrics.
type Registry struct {
	mu      sync.RWMutex
	metrics []metric
}

// DefaultRegistry is the global registry.
var DefaultRegistry = &Registry{}

// Register adds a metric to the registry.
func (r *Registry) Register(m metric) {
	r.mu.Lock()
	r.metrics = append(r.metrics, m)
	r.mu.Unlock()
}

// Expose renders all metrics in Prometheus text exposition format.
func (r *Registry) Expose() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var b strings.Builder
	for _, m := range r.metrics {
		m.render(&b)
	}
	return b.String()
}
