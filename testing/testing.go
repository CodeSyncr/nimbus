package testing

import (
	"bytes"
	"net/http"
	"net/http/httptest"

	"github.com/CodeSyncr/nimbus/router"
)

// TestClient performs HTTP requests against the router (plan: HTTP test client, test helpers).
type TestClient struct {
	Router *router.Router
}

// NewTestClient returns a client that sends requests to the given router.
func NewTestClient(r *router.Router) *TestClient {
	return &TestClient{Router: r}
}

// Get performs a GET request and returns the response.
func (c *TestClient) Get(path string) *httptest.ResponseRecorder {
	return c.Do(httptest.NewRequest(http.MethodGet, path, nil))
}

// Post performs a POST request with an optional body and returns the response.
func (c *TestClient) Post(path string, body []byte) *httptest.ResponseRecorder {
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(http.MethodPost, path, nil)
	}
	return c.Do(req)
}

// Do executes the request and returns the recorder.
func (c *TestClient) Do(req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c.Router.ServeHTTP(w, req)
	return w
}

// AssertStatus is a trivial helper (tests can use require.Equal).
func AssertStatus(w *httptest.ResponseRecorder, want int) bool {
	return w.Code == want
}
