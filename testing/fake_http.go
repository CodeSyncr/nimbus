package testing

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// ── Outgoing HTTP ───────────────────────────────────────────────

// RecordedRequest is one outgoing request seen by FakeHTTP.
type RecordedRequest struct {
	Method string
	URL    *url.URL
	Header http.Header
	Body   []byte
}

// JSON decodes the request body into v.
func (r RecordedRequest) JSON(v any) error { return json.Unmarshal(r.Body, v) }

// FakeTransport answers outgoing HTTP requests from canned responses.
type FakeTransport struct {
	t     testing.TB
	prev  http.RoundTripper
	mu    sync.Mutex
	stubs []*FakeRoute
	sent  []RecordedRequest
	stray bool
}

// FakeHTTP replaces http.DefaultTransport for the rest of the test, so every
// client without a transport of its own (http.DefaultClient, &http.Client{...})
// gets answers from On stubs instead of the network. A request no stub matches
// fails the test and returns an error, unless AllowStray was called.
//
// Clients that set their own Transport are not intercepted, nor are clients
// that copied DefaultTransport before FakeHTTP ran. For the AI providers use
// ai.Fake instead.
//
//	api := nimbustesting.FakeHTTP(t)
//	api.On("POST", "https://api.stripe.com/v1/charges").RespondJSON(200, map[string]any{"id": "ch_1"})
//	client.PostJSON("/checkout", body)
//	api.AssertSent(t, func(r nimbustesting.RecordedRequest) bool { return r.URL.Path == "/v1/charges" })
func FakeHTTP(t testing.TB) *FakeTransport {
	t.Helper()
	claim(t, "http")
	f := &FakeTransport{t: t, prev: http.DefaultTransport}
	http.DefaultTransport = f
	t.Cleanup(func() { http.DefaultTransport = f.prev })
	return f
}

// On stubs requests whose method and URL match. method "" or "*" matches any
// method. In pattern, * matches any run of characters; a pattern without a
// scheme matches host and path ("api.stripe.com/v1/*"), and the query string
// is ignored unless the pattern has one. When several stubs match, the most
// recently added wins. A stub answers 200 with an empty body until told otherwise.
func (f *FakeTransport) On(method, pattern string) *FakeRoute {
	r := &FakeRoute{
		method:  strings.ToUpper(method),
		pattern: pattern,
		re:      globRegexp(pattern),
		respond: func(req *http.Request) (*http.Response, error) {
			return fakeResponse(req, http.StatusOK, nil, nil), nil
		},
	}
	f.mu.Lock()
	f.stubs = append(f.stubs, r)
	f.mu.Unlock()
	return r
}

// AllowStray lets requests no stub matches go to the real network.
func (f *FakeTransport) AllowStray() *FakeTransport {
	f.mu.Lock()
	f.stray = true
	f.mu.Unlock()
	return f
}

// RoundTrip implements http.RoundTripper.
func (f *FakeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		var err error
		if body, err = io.ReadAll(req.Body); err != nil {
			return nil, err
		}
		_ = req.Body.Close()
		req.Body = io.NopCloser(bytes.NewReader(body))
	}

	f.mu.Lock()
	f.sent = append(f.sent, RecordedRequest{Method: req.Method, URL: req.URL, Header: req.Header.Clone(), Body: body})
	var match *FakeRoute
	for i := len(f.stubs) - 1; i >= 0; i-- {
		if f.stubs[i].matches(req) {
			match = f.stubs[i]
			break
		}
	}
	stray := f.stray
	f.mu.Unlock()

	if match != nil {
		match.mu.Lock()
		match.hits++
		respond := match.respond
		match.mu.Unlock()
		return respond(req)
	}
	if stray {
		return f.prev.RoundTrip(req)
	}
	// Errorf, not Fatalf: RoundTrip may run outside the test goroutine.
	f.t.Errorf("nimbustesting: unexpected HTTP request %s %s (stub it with On, or call AllowStray)", req.Method, req.URL)
	return nil, fmt.Errorf("nimbustesting: no fake response for %s %s", req.Method, req.URL)
}

// Requests returns every request sent, in order.
func (f *FakeTransport) Requests() []RecordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]RecordedRequest, len(f.sent))
	copy(out, f.sent)
	return out
}

// AssertSent fails the test unless a request was sent (and, when match is
// given, one of them matches it).
func (f *FakeTransport) AssertSent(t testing.TB, match ...func(RecordedRequest) bool) {
	t.Helper()
	sent := f.Requests()
	if len(sent) == 0 {
		t.Errorf("expected an HTTP request, got none")
		return
	}
	if len(match) > 0 && !anyMatch(sent, match[0]) {
		t.Errorf("%d HTTP request(s) sent, none matched", len(sent))
	}
}

// AssertSentCount fails the test unless exactly n requests were sent.
func (f *FakeTransport) AssertSentCount(t testing.TB, n int) {
	t.Helper()
	if got := len(f.Requests()); got != n {
		t.Errorf("expected %d HTTP requests, got %d", n, got)
	}
}

// AssertNothingSent fails the test if any request was sent.
func (f *FakeTransport) AssertNothingSent(t testing.TB) {
	t.Helper()
	if sent := f.Requests(); len(sent) > 0 {
		t.Errorf("expected no HTTP requests, got %d (first %s %s)", len(sent), sent[0].Method, sent[0].URL)
	}
}

// FakeRoute is one stub added with FakeTransport.On.
type FakeRoute struct {
	method  string
	pattern string
	re      *regexp.Regexp

	mu      sync.Mutex
	respond func(*http.Request) (*http.Response, error)
	hits    int
}

// Respond answers with status and body. headers are key, value pairs.
func (r *FakeRoute) Respond(status int, body string, headers ...string) *FakeRoute {
	h := http.Header{}
	for i := 0; i+1 < len(headers); i += 2 {
		h.Add(headers[i], headers[i+1])
	}
	return r.RespondWith(func(req *http.Request) (*http.Response, error) {
		return fakeResponse(req, status, h, []byte(body)), nil
	})
}

// RespondJSON answers with status and v encoded as JSON.
func (r *FakeRoute) RespondJSON(status int, v any) *FakeRoute {
	data, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("nimbustesting: RespondJSON: %v", err))
	}
	return r.Respond(status, string(data), "Content-Type", "application/json")
}

// Fail makes matching requests fail with err, like a network error.
func (r *FakeRoute) Fail(err error) *FakeRoute {
	return r.RespondWith(func(*http.Request) (*http.Response, error) { return nil, err })
}

// RespondWith answers with whatever fn returns.
func (r *FakeRoute) RespondWith(fn func(*http.Request) (*http.Response, error)) *FakeRoute {
	r.mu.Lock()
	r.respond = fn
	r.mu.Unlock()
	return r
}

// Hits returns how many requests this stub answered.
func (r *FakeRoute) Hits() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.hits
}

func (r *FakeRoute) matches(req *http.Request) bool {
	if r.method != "" && r.method != "*" && r.method != req.Method {
		return false
	}
	u := *req.URL
	if !strings.Contains(r.pattern, "?") {
		u.RawQuery = ""
	}
	u.Fragment = ""
	s := u.String()
	if !strings.Contains(r.pattern, "://") {
		s = strings.TrimPrefix(s, u.Scheme+"://")
	}
	return r.re.MatchString(s)
}

// globRegexp compiles a pattern where * matches any run of characters.
func globRegexp(pattern string) *regexp.Regexp {
	parts := strings.Split(pattern, "*")
	for i, p := range parts {
		parts[i] = regexp.QuoteMeta(p)
	}
	return regexp.MustCompile("^" + strings.Join(parts, ".*") + "$")
}

func fakeResponse(req *http.Request, status int, h http.Header, body []byte) *http.Response {
	if h == nil {
		h = http.Header{}
	}
	return &http.Response{
		Status:        fmt.Sprintf("%d %s", status, http.StatusText(status)),
		StatusCode:    status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        h.Clone(),
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       req,
	}
}
