package insights

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CodeSyncr/nimbus/database"
	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

type post struct {
	ID    uint
	Title string
}

const browserUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36"

// app is a small app with the collector in front of it.
func app(t *testing.T, cfg Config) (*Insights, *router.Router) {
	t.Helper()
	if cfg.Token == "" {
		cfg.Token = "secret-for-tests"
	}
	if cfg.TrafficDir == "" {
		cfg.TrafficDir = t.TempDir()
	}
	in := New(cfg)
	t.Cleanup(in.Close)
	r := router.New()
	// the app's own recovery, outside everything
	r.Use(func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *nhttp.Context) (err error) {
			defer func() {
				if rec := recover(); rec != nil {
					c.Response.WriteHeader(http.StatusInternalServerError)
				}
			}()
			return next(c)
		}
	})
	r.Use(in.Middleware())
	r.Get(in.cfg.Path, in.Handler())
	return in, r
}

func page(c *nhttp.Context, body string) error {
	c.Response.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, err := c.Response.Write([]byte(body))
	return err
}

func get(r http.Handler, path string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("User-Agent", browserUA)
	req.RemoteAddr = "203.0.113.7:4000"
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}

func read(t *testing.T, r http.Handler, in *Insights, section string) *Report {
	t.Helper()
	rr := get(r, in.cfg.Path+"?section="+section, "Authorization", "Bearer "+in.cfg.Token)
	if rr.Code != 200 {
		t.Fatalf("the endpoint answered %d: %s", rr.Code, rr.Body.String())
	}
	var rep Report
	if err := json.Unmarshal(rr.Body.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	return &rep
}

func TestWithNoSecretNothingIsInstalled(t *testing.T) {
	t.Setenv("NIMBUS_INSIGHTS_TOKEN", "")
	if in := New(Config{}); in != nil {
		t.Fatal("a collector was started with no secret to read it by")
	}
	var in *Insights
	in.Close() // nothing to close, and no harm in asking
	next := func(c *nhttp.Context) error { return nil }
	if in.Middleware()(next) == nil {
		t.Error("with no collector the middleware should pass a request straight on")
	}
	t.Setenv("NIMBUS_INSIGHTS_TOKEN", "from-the-environment")
	in = New(Config{TrafficDir: "-"})
	if in == nil || in.cfg.Token != "from-the-environment" || in.cfg.Path != "/_nimbus/insights" {
		t.Fatalf("the secret from the environment: %+v", in)
	}
	in.Close()
}

func TestTheEndpointAnswersOnlyTheSecret(t *testing.T) {
	in, r := app(t, Config{})
	for _, auth := range []string{"", "Bearer wrong", "secret-for-tests", "Bearer "} {
		if rr := get(r, in.cfg.Path, "Authorization", auth); rr.Code != http.StatusNotFound || strings.Contains(rr.Body.String(), "heap") {
			t.Errorf("with %q the endpoint answered %d: %.80s", auth, rr.Code, rr.Body.String())
		}
	}
	rep := read(t, r, in, "overview")
	if rep.Overview == nil || rep.Runtime != nil || rep.App.Go == "" || rep.App.Version != Version {
		t.Errorf("one section asked for: %+v", rep)
	}
	// reading the numbers is not one of them
	if all := read(t, r, in, "requests"); all.Requests.Total != 0 {
		t.Errorf("%d requests counted, and none was made of the app", all.Requests.Total)
	}
	// everything at once leaves the profiles out: they are asked for by name
	if all := read(t, r, in, "all"); all.Heap != nil || all.Goroutines != nil || all.Runtime == nil || all.Traffic == nil || all.Queries == nil {
		t.Errorf("everything at once: heap %v goroutines %v", all.Heap != nil, all.Goroutines != nil)
	}
}

func TestRoutesAreMeasuredByTheirPattern(t *testing.T) {
	in, r := app(t, Config{SlowRequest: 30 * time.Millisecond})
	r.Get("/posts/:id", func(c *nhttp.Context) error { return page(c, "a post") })
	r.Get("/slow", func(c *nhttp.Context) error { time.Sleep(45 * time.Millisecond); return page(c, "slow") })
	r.Get("/fails", func(c *nhttp.Context) error { return errors.New("the mailer is down") })
	r.Get("/panics", func(c *nhttp.Context) error { panic("nil map in the cart") })
	r.Get("/broken", func(c *nhttp.Context) error { c.Response.WriteHeader(http.StatusBadGateway); return nil })
	for i := 1; i <= 7; i++ {
		get(r, fmt.Sprintf("/posts/%d", i))
	}
	get(r, "/slow")
	get(r, "/fails")
	get(r, "/panics")
	get(r, "/broken")
	get(r, "/nothing-here")

	rep := read(t, r, in, "requests,errors,overview")
	by := map[string]RouteStat{}
	for _, s := range rep.Requests.Routes {
		by[s.Route] = s
	}
	// seven posts are one route, not seven
	posts, ok := by["GET /posts/{id}"]
	if !ok {
		posts, ok = by["GET /posts/:id"]
	}
	if !ok || posts.Count != 7 || posts.Errors != 0 {
		t.Fatalf("the posts route: %+v of %v", posts, rep.Requests.Routes)
	}
	if s := by["GET /slow"]; s.Count != 1 || s.MaxMS < 40 || s.P95MS < 40 {
		t.Errorf("the slow route: %+v", s)
	}
	if len(rep.Requests.Slow) != 1 || rep.Requests.Slow[0].Path != "/slow" || rep.Requests.Slow[0].MS < 40 {
		t.Errorf("the slow requests: %+v", rep.Requests.Slow)
	}
	// an address the app has no route for never reaches a route's middleware
	if rep.Requests.Status["2xx"] != 8 || rep.Requests.Status["5xx"] != 3 || rep.Requests.Total != 11 {
		t.Errorf("by status: %v", rep.Requests.Status)
	}
	kinds := map[string]ErrorEntry{}
	for _, e := range rep.Errors {
		kinds[e.Kind] = e
	}
	if e := kinds["error"]; e.Message != "the mailer is down" || e.Path != "/fails" {
		t.Errorf("a handler's error: %+v", e)
	}
	if e := kinds["panic"]; e.Message != "nil map in the cart" || !strings.Contains(e.Stack, "insights_test.go") || e.Status != 500 {
		t.Errorf("a panic: %+v", e)
	}
	if e := kinds["status"]; e.Status != http.StatusBadGateway || e.Path != "/broken" {
		t.Errorf("a 5xx answer: %+v", e)
	}
	if rep.Overview.Errors != 3 || rep.Overview.ErrorRate < 0.2 || rep.Overview.P99MS < 40 || rep.Overview.InFlight != 0 {
		t.Errorf("the overview: %+v", rep.Overview)
	}
}

func TestQueriesAreTimedAndARepeatedOneIsCaught(t *testing.T) {
	db, err := database.ConnectWithConfig(database.ConnectConfig{Driver: "sqlite", DSN: "file:" + t.Name() + "?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&post{}); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 8; i++ {
		db.Create(&post{Title: fmt.Sprintf("Post %d", i)})
	}
	in, r := app(t, Config{SlowQuery: time.Nanosecond}) // every query counts as slow here
	// a list that asks for each row on its own: the N+1
	r.Get("/posts", func(c *nhttp.Context) error {
		var ids []uint
		db.Model(&post{}).Pluck("id", &ids)
		for _, id := range ids {
			var p post
			db.First(&p, id)
		}
		return page(c, "posts")
	})
	r.Get("/post", func(c *nhttp.Context) error {
		var p post
		db.Where("title = ?", "Post 3's secret").First(&p) // not there
		db.Exec("SELECT nothing FROM nowhere")             // not SQL the database knows
		return page(c, "one")
	})
	get(r, "/posts")
	get(r, "/posts")
	get(r, "/post")

	rep := read(t, r, in, "queries,requests")
	if len(rep.Requests.Repeated) != 1 {
		t.Logf("queries: %d in all; routes: %+v", rep.Queries.Total, rep.Requests.Routes)
		t.Fatalf("%d repeated queries, want the one: %+v", len(rep.Requests.Repeated), rep.Requests.Repeated)
	}
	rp := rep.Requests.Repeated[0]
	if rp.Times != 8 || rp.Seen != 2 || !strings.Contains(rp.Route, "/posts") || !strings.Contains(rp.Shape, "?") || strings.Contains(rp.Shape, " 7") || !strings.Contains(rp.Caller, "insights_test.go:") {
		t.Errorf("the repeated query: %+v", rp)
	}
	var list RouteStat
	for _, s := range rep.Requests.Routes {
		if strings.HasSuffix(s.Route, " /posts") {
			list = s
		}
	}
	if list.Queries != 9 { // the list, and one for each of eight rows
		t.Errorf("the list route asks %.1f queries a request, want 9", list.Queries)
	}
	// every run of a query is one line, whatever it was run with
	var one *QueryStat
	for i, q := range rep.Queries.Top {
		if q.Count == 16 {
			one = &rep.Queries.Top[i]
		}
	}
	if one == nil || !strings.Contains(one.Caller, "insights_test.go:") || one.Rows != 16 {
		t.Errorf("the per-row query as one line: %+v", rep.Queries.Top)
	}
	// a slow one is kept as it was run, values and all, with where from
	full := false
	for _, q := range rep.Queries.Slow {
		full = full || (strings.Contains(q.SQL, "Post 3's secret") || strings.Contains(q.SQL, "Post 3''s secret")) && !strings.Contains(q.Shape, "secret")
	}
	if !full {
		t.Errorf("no slow query kept with its values: %+v", rep.Queries.Slow)
	}
	// a row that is not there is an answer; SQL the database refuses is a failure
	if len(rep.Queries.Failed) != 1 || !strings.Contains(rep.Queries.Failed[0].SQL, "nowhere") || rep.Queries.Failed[0].Error == "" {
		t.Errorf("failed queries: %+v", rep.Queries.Failed)
	}
	if rep.Queries.Pool == nil || rep.Queries.Driver == "" || rep.Queries.Total < 20 {
		t.Errorf("the pool %+v, the driver %q, %d queries", rep.Queries.Pool, rep.Queries.Driver, rep.Queries.Total)
	}
	// a query run outside any request is counted and belongs to none
	db.First(&post{}, 1)
	if got := read(t, r, in, "queries").Queries.Total; got != rep.Queries.Total+1 {
		t.Errorf("a query outside a request: %d, want %d", got, rep.Queries.Total+1)
	}
}

func TestShape(t *testing.T) {
	for in, want := range map[string]string{
		"SELECT * FROM `posts` WHERE `posts`.`id` = 7 LIMIT 1":            "SELECT * FROM `posts` WHERE `posts`.`id` = ? LIMIT ?",
		"SELECT * FROM users WHERE email = 'a@b.co' AND name = 'O''Neil'": "SELECT * FROM users WHERE email = ? AND name = ?",
		"SELECT * FROM t WHERE id IN (1,2, 3)":                            "SELECT * FROM t WHERE id IN (?…)",
		"UPDATE t\n  SET  price = 9.50\n WHERE id = 3":                    "UPDATE t SET price = ? WHERE id = ?",
		"SELECT col2 FROM table1":                                         "SELECT col2 FROM table1",
	} {
		if got := Shape(in); got != want {
			t.Errorf("%q:\n got %q\nwant %q", in, got, want)
		}
	}
}

func TestVisitsAreCountedForPeopleAndKept(t *testing.T) {
	dir := t.TempDir()
	in, r := app(t, Config{TrafficDir: dir})
	r.Get("/", func(c *nhttp.Context) error { return page(c, "home") })
	r.Get("/pricing", func(c *nhttp.Context) error { return page(c, "pricing") })
	r.Get("/api/items", func(c *nhttp.Context) error {
		c.Response.Header().Set("Content-Type", "application/json")
		_, err := c.Response.Write([]byte("[]"))
		return err
	})
	r.Get("/gone", func(c *nhttp.Context) error { c.Response.WriteHeader(http.StatusNotFound); return nil })

	get(r, "/", "Referer", "https://www.google.com/search?q=x", "CF-IPCountry", "IN")
	get(r, "/pricing", "Referer", "http://example.com/")                         // the same person, a second page
	get(r, "/", "CF-Connecting-IP", "198.51.100.9", "CF-IPCountry", "DE")        // somebody else
	get(r, "/", "User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0) Mobile") // and a phone
	get(r, "/pricing", "X-Inertia", "true")                                      // a link followed inside the app
	// not visits: a crawler, an API call, a page that is not there, a part of a page, a prefetch
	get(r, "/", "User-Agent", "Googlebot/2.1 (+http://www.google.com/bot.html)")
	get(r, "/api/items")
	get(r, "/gone")
	get(r, "/pricing", "X-Inertia", "true", "X-Inertia-Partial-Data", "items")
	get(r, "/", "Sec-Purpose", "prefetch")

	tr := read(t, r, in, "traffic").Traffic
	if tr.Views != 5 || tr.Visitors != 3 || tr.Today.Views != 5 || len(tr.Days) != 30 || !tr.Kept {
		t.Fatalf("views %d, visitors %d, today %+v, %d days", tr.Views, tr.Visitors, tr.Today, len(tr.Days))
	}
	want := func(list []Count, name string, n int) {
		t.Helper()
		for _, c := range list {
			if c.Name == name {
				if c.Count != n {
					t.Errorf("%s: %d, want %d", name, c.Count, n)
				}
				return
			}
		}
		t.Errorf("%s is not in %+v", name, list)
	}
	want(tr.Paths, "/", 3)
	want(tr.Paths, "/pricing", 2)
	want(tr.Referrers, "google.com", 1) // where a visitor first came from, once
	want(tr.Referrers, "(direct)", 2)
	want(tr.Countries, "IN", 1)
	want(tr.Countries, "DE", 1)
	want(tr.Devices, "mobile", 1)
	want(tr.Devices, "desktop", 2)
	want(tr.Browsers, "Chrome", 2)
	hours := 0
	for _, h := range tr.Hours {
		hours += h
	}
	if hours != 5 {
		t.Errorf("today by the hour adds up to %d", hours)
	}

	// written down: another start of the app has the day, and the same
	// person back again is still one visitor
	in.Close()
	files, _ := filepath.Glob(filepath.Join(dir, "traffic-*.json"))
	if len(files) != 1 {
		t.Fatalf("the day was not written: %v", files)
	}
	data, _ := os.ReadFile(files[0])
	if strings.Contains(string(data), "203.0.113.7") || strings.Contains(string(data), "Macintosh") {
		t.Error("an address or a browser's own words were written to the day's file")
	}
	in2, r2 := app(t, Config{TrafficDir: dir})
	r2.Get("/", func(c *nhttp.Context) error { return page(c, "home") })
	get(r2, "/")
	tr = read(t, r2, in2, "traffic").Traffic
	if tr.Views != 6 || tr.Visitors != 3 {
		t.Errorf("after a restart: views %d, visitors %d; want 6 and 3", tr.Views, tr.Visitors)
	}
	// another secret tells the same person apart differently: nothing carries over
	in3 := New(Config{Token: "another-app", TrafficDir: "-"})
	defer in3.Close()
	if in3.traffic.report(1).Kept {
		t.Error("a log kept in memory says it is written down")
	}
}

func TestMemoryAndTheProfiles(t *testing.T) {
	in, r := app(t, Config{Sample: 20 * time.Millisecond})
	r.Get("/", func(c *nhttp.Context) error { return page(c, "home") })
	held := make([][]byte, 0, 64)
	for i := 0; i < 64; i++ {
		held = append(held, make([]byte, 1<<20)) // 64 MB, held to the end of the test
	}
	get(r, "/")
	time.Sleep(120 * time.Millisecond)
	rt := read(t, r, in, "runtime").Runtime
	if len(rt.History) < 3 || rt.Now.HeapInuse < 60<<20 || rt.Now.Goroutines < 2 || rt.NumCPU < 1 || rt.GOGC == "" || rt.Now.Sys < rt.Now.HeapInuse {
		t.Errorf("the runtime: %d samples, heap in use %d MB, %d goroutines", len(rt.History), rt.Now.HeapInuse>>20, rt.Now.Goroutines)
	}
	if !rt.History[len(rt.History)-1].At.After(rt.History[0].At) {
		t.Error("the history is not oldest first")
	}
	hp := read(t, r, in, "heap").Heap
	if hp == nil || len(hp.Sites) == 0 || hp.TotalInuseBytes < 30<<20 {
		t.Fatalf("the heap profile: %+v", hp)
	}
	if top := hp.Sites[0]; !strings.Contains(top.At.Func, "TestMemoryAndTheProfiles") || !strings.Contains(top.At.File, "insights_test.go") || top.InuseBytes < 30<<20 {
		t.Errorf("what holds the most memory: %+v", top)
	}
	// goroutines parked in the same place are one group
	stop := make(chan struct{})
	for i := 0; i < 25; i++ {
		go func() { <-stop }()
	}
	time.Sleep(20 * time.Millisecond)
	gr := read(t, r, in, "goroutines").Goroutines
	close(stop)
	if gr.Total < 25 || len(gr.Groups) == 0 || gr.Groups[0].Count < 25 || !strings.Contains(gr.Groups[0].At.File, "insights_test.go") {
		t.Errorf("the goroutines: %d in all, the largest group %+v", gr.Total, gr.Groups)
	}
	_ = held[0][0]
}

// A streamed answer and a taken-over connection pass through the collector.
func TestStreamingPassesThrough(t *testing.T) {
	_, r := app(t, Config{})
	flushed := false
	r.Get("/stream", func(c *nhttp.Context) error {
		f, ok := c.Response.(http.Flusher)
		if !ok {
			return errors.New("the response can no longer be flushed")
		}
		c.Response.Header().Set("Content-Type", "text/event-stream")
		_, _ = c.Response.Write([]byte("data: 1\n\n"))
		f.Flush()
		flushed = true
		if _, ok := c.Response.(http.Hijacker); !ok {
			return errors.New("the response can no longer be hijacked")
		}
		return nil
	})
	if rr := get(r, "/stream"); rr.Code != 200 || !flushed || !rr.Flushed {
		t.Errorf("a streamed answer: %d, flushed %v", rr.Code, rr.Flushed)
	}
}
