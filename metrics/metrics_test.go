package metrics

import (
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func lines(s string) map[string]bool {
	out := map[string]bool{}
	for _, l := range strings.Split(s, "\n") {
		out[l] = true
	}
	return out
}

func TestCounterAndGauge(t *testing.T) {
	r := &Registry{}
	c := NewCounter("requests_total", "Requests")
	g := NewGauge("connections", "Open connections")
	r.Register(c)
	r.Register(g)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Inc(Labels{"method": "GET"})
		}()
	}
	wg.Wait()
	c.Add(5, Labels{"method": "POST"})
	g.Set(10, nil)
	g.Inc(nil)
	g.Dec(nil)
	g.Add(-3, nil)

	out := lines(r.Expose())
	for _, want := range []string{
		"# TYPE requests_total counter",
		`requests_total{method="GET"} 100`,
		`requests_total{method="POST"} 5`,
		"# TYPE connections gauge",
		"connections 7",
	} {
		if !out[want] {
			t.Errorf("missing line %q in:\n%s", want, r.Expose())
		}
	}
}

func TestHistogramBucketsAreCumulativeOnceAndSumIsCorrect(t *testing.T) {
	r := &Registry{}
	h := NewHistogram("latency_seconds", "Latency", []float64{0.1, 1, 0.5}) // unsorted on purpose
	r.Register(h)
	for _, v := range []float64{0.05, 0.3, 0.3, 0.7, 5} {
		h.Observe(v, Labels{"route": "/"})
	}
	out := lines(r.Expose())
	for _, want := range []string{
		`latency_seconds_bucket{le="0.1",route="/"} 1`,
		`latency_seconds_bucket{le="0.5",route="/"} 3`,
		`latency_seconds_bucket{le="1",route="/"} 4`,
		`latency_seconds_bucket{le="+Inf",route="/"} 5`,
		`latency_seconds_sum{route="/"} 6.35`,
		`latency_seconds_count{route="/"} 5`,
	} {
		if !out[want] {
			t.Errorf("missing line %q in:\n%s", want, r.Expose())
		}
	}
}

func TestHistogramConcurrentSum(t *testing.T) {
	h := NewHistogram("x", "x", nil)
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.Observe(0.5, nil)
		}()
	}
	wg.Wait()
	r := &Registry{}
	r.Register(h)
	if !lines(r.Expose())["x_sum 500"] {
		t.Fatalf("concurrent sum wrong:\n%s", r.Expose())
	}
}

func TestLabelValuesAreEscaped(t *testing.T) {
	r := &Registry{}
	c := NewCounter("errors_total", "Errors")
	r.Register(c)
	c.Inc(Labels{"msg": "say \"hi\"\\\nbye"})
	if !strings.Contains(r.Expose(), `errors_total{msg="say \"hi\"\\\nbye"} 1`) {
		t.Fatalf("label not escaped:\n%s", r.Expose())
	}
}

func TestHandler(t *testing.T) {
	r := &Registry{}
	c := NewCounter("hits_total", "Hits")
	r.Register(c)
	c.Inc(nil)
	rec := httptest.NewRecorder()
	RegistryHandler(r).ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain; version=0.0.4") {
		t.Fatalf("status=%d content-type=%q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Body.String(), "hits_total 1") {
		t.Fatalf("body = %s", rec.Body.String())
	}
	rec = httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	if rec.Code != 200 {
		t.Fatalf("default handler status = %d", rec.Code)
	}
}

func TestReadRuntimeStats(t *testing.T) {
	s := ReadRuntimeStats()
	if s.Goroutines < 1 || s.HeapAlloc == 0 || s.HeapSys == 0 {
		t.Fatalf("implausible runtime stats: %+v", s)
	}
}
