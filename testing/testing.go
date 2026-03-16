package testing

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/router"
)

// ── Test Client ─────────────────────────────────────────────────

// TestClient performs HTTP requests against a Nimbus router.
type TestClient struct {
	Router  *router.Router
	headers http.Header
	cookies []*http.Cookie
}

// NewTestClient returns a client that sends requests to the given router.
func NewTestClient(r *router.Router) *TestClient {
	return &TestClient{
		Router:  r,
		headers: make(http.Header),
	}
}

// WithHeader sets a header for all subsequent requests.
func (c *TestClient) WithHeader(key, value string) *TestClient {
	c.headers.Set(key, value)
	return c
}

// WithCookie adds a cookie for all subsequent requests.
func (c *TestClient) WithCookie(cookie *http.Cookie) *TestClient {
	c.cookies = append(c.cookies, cookie)
	return c
}

// WithBearerToken sets the Authorization header.
func (c *TestClient) WithBearerToken(token string) *TestClient {
	return c.WithHeader("Authorization", "Bearer "+token)
}

// Get performs a GET request and returns a TestResponse.
func (c *TestClient) Get(path string) *TestResponse {
	return c.Do(httptest.NewRequest(http.MethodGet, path, nil))
}

// Post performs a POST request with an optional JSON body.
func (c *TestClient) Post(path string, body []byte) *TestResponse {
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(http.MethodPost, path, nil)
	}
	return c.Do(req)
}

// PostJSON posts a JSON-serializable value.
func (c *TestClient) PostJSON(path string, v any) *TestResponse {
	b, _ := json.Marshal(v)
	return c.Post(path, b)
}

// PostForm posts form data.
func (c *TestClient) PostForm(path string, data url.Values) *TestResponse {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return c.Do(req)
}

// Put performs a PUT request.
func (c *TestClient) Put(path string, body []byte) *TestResponse {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req := httptest.NewRequest(http.MethodPut, path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.Do(req)
}

// PutJSON puts a JSON-serializable value.
func (c *TestClient) PutJSON(path string, v any) *TestResponse {
	b, _ := json.Marshal(v)
	return c.Put(path, b)
}

// Patch performs a PATCH request.
func (c *TestClient) Patch(path string, body []byte) *TestResponse {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req := httptest.NewRequest(http.MethodPatch, path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.Do(req)
}

// Delete performs a DELETE request.
func (c *TestClient) Delete(path string) *TestResponse {
	return c.Do(httptest.NewRequest(http.MethodDelete, path, nil))
}

// Do executes a raw *http.Request and returns a TestResponse.
func (c *TestClient) Do(req *http.Request) *TestResponse {
	// Apply default headers.
	for k, vv := range c.headers {
		for _, v := range vv {
			req.Header.Set(k, v)
		}
	}
	// Apply cookies.
	for _, ck := range c.cookies {
		req.AddCookie(ck)
	}
	w := httptest.NewRecorder()
	c.Router.ServeHTTP(w, req)
	return &TestResponse{ResponseRecorder: w}
}

// ── Test Response (fluent assertions) ───────────────────────────

// TestResponse wraps httptest.ResponseRecorder with assertion helpers.
type TestResponse struct {
	*httptest.ResponseRecorder
}

// AssertStatus fails t if the status code does not match.
func (r *TestResponse) AssertStatus(t testing.TB, want int) *TestResponse {
	t.Helper()
	if r.Code != want {
		t.Errorf("expected status %d, got %d\nbody: %s", want, r.Code, r.Body.String())
	}
	return r
}

// AssertOK asserts 200.
func (r *TestResponse) AssertOK(t testing.TB) *TestResponse {
	return r.AssertStatus(t, http.StatusOK)
}

// AssertCreated asserts 201.
func (r *TestResponse) AssertCreated(t testing.TB) *TestResponse {
	return r.AssertStatus(t, http.StatusCreated)
}

// AssertNoContent asserts 204.
func (r *TestResponse) AssertNoContent(t testing.TB) *TestResponse {
	return r.AssertStatus(t, http.StatusNoContent)
}

// AssertNotFound asserts 404.
func (r *TestResponse) AssertNotFound(t testing.TB) *TestResponse {
	return r.AssertStatus(t, http.StatusNotFound)
}

// AssertUnauthorized asserts 401.
func (r *TestResponse) AssertUnauthorized(t testing.TB) *TestResponse {
	return r.AssertStatus(t, http.StatusUnauthorized)
}

// AssertForbidden asserts 403.
func (r *TestResponse) AssertForbidden(t testing.TB) *TestResponse {
	return r.AssertStatus(t, http.StatusForbidden)
}

// AssertRedirect asserts a 3xx response with the given Location header.
func (r *TestResponse) AssertRedirect(t testing.TB, location string) *TestResponse {
	t.Helper()
	if r.Code < 300 || r.Code >= 400 {
		t.Errorf("expected redirect, got %d", r.Code)
	}
	got := r.Header().Get("Location")
	if got != location {
		t.Errorf("expected redirect to %q, got %q", location, got)
	}
	return r
}

// AssertHeader asserts a response header value.
func (r *TestResponse) AssertHeader(t testing.TB, key, want string) *TestResponse {
	t.Helper()
	got := r.Header().Get(key)
	if got != want {
		t.Errorf("expected header %s=%q, got %q", key, want, got)
	}
	return r
}

// AssertContains fails t if body does not contain substr.
func (r *TestResponse) AssertContains(t testing.TB, substr string) *TestResponse {
	t.Helper()
	if !strings.Contains(r.Body.String(), substr) {
		t.Errorf("expected body to contain %q, got:\n%s", substr, r.Body.String())
	}
	return r
}

// AssertJSON decodes the response body as JSON into v.
func (r *TestResponse) AssertJSON(t testing.TB, v any) *TestResponse {
	t.Helper()
	if err := json.Unmarshal(r.Body.Bytes(), v); err != nil {
		t.Errorf("failed to decode JSON: %v\nbody: %s", err, r.Body.String())
	}
	return r
}

// AssertJSONPath asserts that a JSON object has the given key with the given value.
func (r *TestResponse) AssertJSONPath(t testing.TB, key string, want any) *TestResponse {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &m); err != nil {
		t.Errorf("failed to decode JSON: %v", err)
		return r
	}
	got, ok := m[key]
	if !ok {
		t.Errorf("key %q not found in JSON response", key)
		return r
	}
	gotStr := fmt.Sprintf("%v", got)
	wantStr := fmt.Sprintf("%v", want)
	if gotStr != wantStr {
		t.Errorf("JSON[%s]: expected %v, got %v", key, want, got)
	}
	return r
}

// ── App Test Client (legacy compat) ─────────────────────────────

// NewAppTestClient creates a minimal Nimbus application with a fresh router
// and returns both the app and an HTTP TestClient bound to its router.
func NewAppTestClient(setup func(app *nimbus.App)) (*nimbus.App, *TestClient) {
	app := nimbus.New()
	if setup != nil {
		setup(app)
	}
	return app, NewTestClient(app.Router)
}

// AssertStatus is a legacy helper (use TestResponse assertions instead).
func AssertStatus(w *httptest.ResponseRecorder, want int) bool {
	return w.Code == want
}
