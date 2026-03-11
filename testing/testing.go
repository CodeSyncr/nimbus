package testing

import (
	"bytes"
	"net/http"
	"net/http/httptest"

	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/router"
)

// TestClient performs HTTP requests against a Nimbus router.
// It is a thin wrapper around net/http/httptest utilities and is intended
// to be used from Go tests.
//
// Example:
//
//	r := router.New()
//	r.Get("/hello", func(c *http.Context) error {
//	    return c.String(http.StatusOK, "ok")
//	})
//	client := testing.NewTestClient(r)
//	res := client.Get("/hello")
//	require.Equal(t, http.StatusOK, res.Code)
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

// Post performs a POST request with an optional JSON body and returns the response.
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

// AssertStatus is a trivial helper (tests can also use require.Equal).
func AssertStatus(w *httptest.ResponseRecorder, want int) bool {
	return w.Code == want
}

// NewAppTestClient creates a minimal Nimbus application with a fresh router
// and returns both the app and an HTTP TestClient bound to its router.
//
// The optional setup callback can register routes, middleware, or modify the
// app/container before tests run.
//
// Example:
//
//	app, client := testing.NewAppTestClient(func(app *nimbus.App) {
//	    app.Router.Get("/ping", func(c *http.Context) error {
//	        return c.String(http.StatusOK, "pong")
//	    })
//	})
//	res := client.Get("/ping")
//	require.Equal(t, http.StatusOK, res.Code)
func NewAppTestClient(setup func(app *nimbus.App)) (*nimbus.App, *TestClient) {
	app := nimbus.New()
	if setup != nil {
		setup(app)
	}
	return app, NewTestClient(app.Router)
}
