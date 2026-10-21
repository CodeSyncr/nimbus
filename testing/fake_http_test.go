package testing_test

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	nimbustesting "github.com/CodeSyncr/nimbus/testing"
)

func TestFakeHTTPStubsDefaultClients(t *testing.T) {
	api := nimbustesting.FakeHTTP(t)
	charges := api.On("POST", "https://api.stripe.com/v1/charges").RespondJSON(201, map[string]any{"id": "ch_1"})
	api.On("*", "api.example.com/users/*").Respond(404, "missing", "X-Trace", "t1")

	resp, err := http.DefaultClient.Post("https://api.stripe.com/v1/charges?expand=x", "application/json", strings.NewReader(`{"amount":500}`))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 201 || string(body) != `{"id":"ch_1"}` || resp.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("stripe stub = %d %q %v", resp.StatusCode, body, resp.Header)
	}

	// A client built the usual way, with only a timeout, is intercepted too.
	c := &http.Client{Timeout: time.Second}
	resp, err = c.Get("http://api.example.com/users/42")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 || resp.Header.Get("X-Trace") != "t1" {
		t.Fatalf("users stub = %d %v", resp.StatusCode, resp.Header)
	}

	api.AssertSentCount(t, 2)
	api.AssertSent(t, func(r nimbustesting.RecordedRequest) bool {
		var in struct{ Amount int }
		return r.Method == "POST" && r.JSON(&in) == nil && in.Amount == 500
	})
	if charges.Hits() != 1 {
		t.Fatalf("charges hits = %d", charges.Hits())
	}
}

func TestFakeHTTPLatestStubWinsAndFail(t *testing.T) {
	api := nimbustesting.FakeHTTP(t)
	api.On("GET", "https://svc.test/*").Respond(500, "old")
	api.On("GET", "https://svc.test/health").Respond(200, "ok")
	api.On("", "https://down.test/*").Fail(errors.New("connection refused"))

	resp, err := http.Get("https://svc.test/health")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("health = %v %v", resp, err)
	}
	resp.Body.Close()
	if _, err := http.Get("https://down.test/x"); err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("Fail stub err = %v", err)
	}
}

func TestFakeHTTPUnmatchedRequestFailsTest(t *testing.T) {
	rec := &errorRecorder{TB: t}
	nimbustesting.FakeHTTP(rec)
	if _, err := http.Get("https://unstubbed.test/x"); err == nil {
		t.Fatal("unmatched request succeeded")
	}
	if len(rec.errors) != 1 || !strings.Contains(rec.errors[0], "unstubbed.test") {
		t.Fatalf("errors = %v, want one naming the request", rec.errors)
	}
}

func TestFakeHTTPAllowStrayAndRestore(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "real")
	}))
	defer srv.Close()
	prev := http.DefaultTransport

	t.Run("faked", func(t *testing.T) {
		nimbustesting.FakeHTTP(t).AllowStray()
		resp, err := http.Get(srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(body) != "real" {
			t.Fatalf("stray body = %q", body)
		}
	})
	if http.DefaultTransport != prev {
		t.Fatal("http.DefaultTransport not restored")
	}
}

// errorRecorder records Errorf instead of failing the real test.
type errorRecorder struct {
	testing.TB
	mu     sync.Mutex
	errors []string
}

func (r *errorRecorder) Errorf(format string, args ...any) {
	r.mu.Lock()
	r.errors = append(r.errors, fmt.Sprintf(format, args...))
	r.mu.Unlock()
}
