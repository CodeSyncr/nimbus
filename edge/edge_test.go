package edge

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/CodeSyncr/nimbus"
	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

// appWith builds a router with the edge middleware and an origin handler that
// echoes method + body, so tests can assert what the downstream handler saw.
func appWith(rt *Runtime) *router.Router {
	r := router.New()
	r.Use(rt.Middleware())
	origin := func(c *nhttp.Context) error {
		body, _ := io.ReadAll(c.Request.Body)
		return c.JSON(200, map[string]string{"origin": "true", "body": string(body)})
	}
	r.Get("/*", origin)
	r.Post("/*", origin)
	return r
}

func do(r *router.Router, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	r.ServeHTTP(rec, httptest.NewRequest(method, path, rdr))
	return rec
}

// ── Regression: the body must survive passthrough (was drained) ────

func TestPassthrough_PreservesRequestBodyForOrigin(t *testing.T) {
	rt := New(Config{MaxExecTime: time.Second})
	var edgeSaw string
	rt.Handle("/api", func(req *Request) *Response {
		edgeSaw = string(req.Body) // edge reads the body...
		return Next()              // ...then passes through
	})

	rec := do(appWith(rt), "POST", "/api", `{"hello":"world"}`)
	if edgeSaw != `{"hello":"world"}` {
		t.Errorf("edge handler body = %q", edgeSaw)
	}
	if !strings.Contains(rec.Body.String(), `"body":"{\"hello\":\"world\"}"`) {
		t.Errorf("origin did NOT receive the body: %s", rec.Body.String())
	}
}

// ── Regression: caching must not corrupt the request body ──────────

func TestCache_PreservesRequestBody(t *testing.T) {
	rt := New(Config{MaxExecTime: time.Second})
	var edgeSaw string
	rt.Handle("/api", func(req *Request) *Response {
		edgeSaw = string(req.Body)
		return JSON(200, map[string]string{"got": edgeSaw})
	}).WithCache(time.Minute)

	rec := do(appWith(rt), "POST", "/api", `{"k":"v"}`)
	if edgeSaw != `{"k":"v"}` {
		t.Errorf("cached-route handler body = %q (want {\"k\":\"v\"})", edgeSaw)
	}
	if !strings.Contains(rec.Body.String(), `"got":"{\"k\":\"v\"}"`) {
		t.Errorf("response = %s", rec.Body.String())
	}
}

// ── Regression: cached text responses (BodyStr) must round-trip ────

func TestCache_RoundTripsBodyAndServesFromCache(t *testing.T) {
	rt := New(Config{MaxExecTime: time.Second})
	calls := 0
	rt.Handle("/hello", func(req *Request) *Response {
		calls++
		return Respond(200, "hello world") // BodyStr, previously lost when cached
	}).WithCache(time.Minute)

	r := appWith(rt)
	rec1 := do(r, "GET", "/hello", "")
	rec2 := do(r, "GET", "/hello", "")

	if rec1.Body.String() != "hello world" {
		t.Fatalf("live body = %q", rec1.Body.String())
	}
	if rec2.Body.String() != "hello world" {
		t.Fatalf("cached body = %q (BodyStr was dropped)", rec2.Body.String())
	}
	if calls != 1 {
		t.Errorf("handler ran %d times; cache should have served the 2nd", calls)
	}
	if rec2.Header().Get("X-Edge-Cache") != "HIT" {
		t.Errorf("2nd response not marked as cache hit")
	}
}

// ── Response behaviors ─────────────────────────────────────────────

func TestRespond_Redirect_And_Rewrite(t *testing.T) {
	rt := New(Config{MaxExecTime: time.Second})
	rt.Handle("/redir", func(*Request) *Response { return Redirect("/there", 302) })
	rt.Handle("/rw", func(*Request) *Response { return Rewrite("/rewritten") })
	r := appWith(rt)

	rd := do(r, "GET", "/redir", "")
	if rd.Code != 302 || rd.Header().Get("Location") != "/there" {
		t.Errorf("redirect: code=%d loc=%q", rd.Code, rd.Header().Get("Location"))
	}
	// Rewrite passes through with a changed path; origin echoes origin:true.
	rw := do(r, "GET", "/rw", "")
	if !strings.Contains(rw.Body.String(), `"origin":"true"`) {
		t.Errorf("rewrite should pass through to origin: %s", rw.Body.String())
	}
}

func TestMethodFilter(t *testing.T) {
	rt := New(Config{MaxExecTime: time.Second})
	rt.Handle("/only-post", func(*Request) *Response { return Respond(200, "edge") }).Methods("POST")
	r := appWith(rt)

	if got := do(r, "POST", "/only-post", "").Body.String(); got != "edge" {
		t.Errorf("POST should hit edge, got %q", got)
	}
	// GET is not in the method list → passes through to origin.
	if got := do(r, "GET", "/only-post", "").Body.String(); !strings.Contains(got, "origin") {
		t.Errorf("GET should pass through, got %q", got)
	}
}

func TestWildcardMatch(t *testing.T) {
	rt := New(Config{MaxExecTime: time.Second})
	rt.Handle("/admin/*", func(*Request) *Response { return Respond(403, "blocked") })
	got := do(appWith(rt), "GET", "/admin/users/1", "").Body.String()
	if got != "blocked" {
		t.Errorf("wildcard should match nested path, got %q", got)
	}
}

// ── Fallback modes ─────────────────────────────────────────────────

func TestFallback_ErrorOnPanic(t *testing.T) {
	rt := New(Config{MaxExecTime: time.Second, Fallback: FallbackError})
	rt.Handle("/boom", func(*Request) *Response { panic("kaboom") })
	rec := do(appWith(rt), "GET", "/boom", "")
	if rec.Code != 502 {
		t.Errorf("panic with FallbackError should 502, got %d", rec.Code)
	}
}

func TestFallback_NextOnTimeout(t *testing.T) {
	rt := New(Config{MaxExecTime: 5 * time.Millisecond, Fallback: FallbackNext})
	rt.Handle("/slow", func(*Request) *Response {
		time.Sleep(50 * time.Millisecond)
		return Respond(200, "too late")
	})
	rec := do(appWith(rt), "GET", "/slow", "")
	if !strings.Contains(rec.Body.String(), "origin") {
		t.Errorf("timeout with FallbackNext should pass through, got %q", rec.Body.String())
	}
}

func TestFallback_CachedServesLastGood(t *testing.T) {
	rt := New(Config{MaxExecTime: 20 * time.Millisecond, Fallback: FallbackCached})
	n := 0
	rt.Handle("/flaky", func(*Request) *Response {
		n++
		if n == 1 {
			return Respond(200, "good") // first call succeeds → recorded as fallback
		}
		time.Sleep(60 * time.Millisecond) // subsequent calls time out
		return Respond(200, "late")
	})
	r := appWith(rt)
	if got := do(r, "GET", "/flaky", "").Body.String(); got != "good" {
		t.Fatalf("first call = %q", got)
	}
	rec := do(r, "GET", "/flaky", "")
	if rec.Body.String() != "good" || rec.Header().Get("X-Edge-Fallback") != "cached" {
		t.Errorf("timeout should serve cached fallback, got %q hdr=%q", rec.Body.String(), rec.Header().Get("X-Edge-Fallback"))
	}
}

// ── Metrics ────────────────────────────────────────────────────────

func TestMetrics_CountsAndAverage(t *testing.T) {
	rt := New(Config{MaxExecTime: time.Second})
	rt.Handle("/m", func(*Request) *Response { return Respond(200, "ok") })
	r := appWith(rt)
	do(r, "GET", "/m", "")
	do(r, "GET", "/m", "")

	m := rt.Metrics()
	if m["total_invocations"].(uint64) != 2 {
		t.Errorf("invocations = %v", m["total_invocations"])
	}
	if m["avg_latency_ns"].(uint64) == 0 {
		t.Errorf("avg latency should be > 0")
	}
	if m["routes"].(int) != 1 {
		t.Errorf("routes = %v", m["routes"])
	}
}

// ── Plugin wiring (the previously non-compiling path) ──────────────

func TestPlugin_AppliesMiddlewareAndMountsMetrics(t *testing.T) {
	var _ nimbus.Plugin = (*EdgePlugin)(nil) // compile-time: satisfies the interface

	rt := New(Config{MaxExecTime: time.Second})
	rt.Handle("/gate", func(*Request) *Response { return Respond(200, "edge-gated") })

	app := nimbus.New()
	ep := rt.Plugin()
	if err := ep.Register(app); err != nil { // applies middleware to app.Router
		t.Fatal(err)
	}
	ep.RegisterRoutes(app.Router) // mounts /_edge/metrics
	app.Router.Get("/*", func(c *nhttp.Context) error { return c.JSON(200, map[string]string{"o": "1"}) })

	// Edge middleware intercepts.
	rec := httptest.NewRecorder()
	app.Router.ServeHTTP(rec, httptest.NewRequest("GET", "/gate", nil))
	if rec.Body.String() != "edge-gated" {
		t.Errorf("plugin did not apply edge middleware: %q", rec.Body.String())
	}

	// Metrics endpoint responds with JSON counters.
	mrec := httptest.NewRecorder()
	app.Router.ServeHTTP(mrec, httptest.NewRequest("GET", "/_edge/metrics", nil))
	var metrics map[string]any
	if err := json.Unmarshal(mrec.Body.Bytes(), &metrics); err != nil {
		t.Fatalf("metrics endpoint: %v (body=%s)", err, mrec.Body.String())
	}
	if _, ok := metrics["total_invocations"]; !ok {
		t.Errorf("metrics missing counters: %v", metrics)
	}
}

// ── A couple of patterns.go helpers ────────────────────────────────

func TestPattern_SecurityHeadersAndBasicAuth(t *testing.T) {
	rt := New(Config{MaxExecTime: time.Second})
	rt.Handle("/sec", SecurityHeaders())
	rt.Handle("/private", BasicAuth("area", map[string]string{"admin": "secret"}))
	r := appWith(rt)

	sec := do(r, "GET", "/sec", "")
	if sec.Header().Get("X-Frame-Options") != "DENY" {
		t.Errorf("security headers not applied: %v", sec.Header())
	}

	// No credentials → 401.
	if code := do(r, "GET", "/private", "").Code; code != 401 {
		t.Errorf("basic auth without creds = %d, want 401", code)
	}
}
