package ai

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// testClient points a client at a test server with fast backoff so the suite
// does not spend seconds sleeping.
func testClient(t *testing.T, url string) *NimbusCloudClient {
	t.Helper()
	return &NimbusCloudClient{
		ServerURL:  strings.TrimRight(url, "/"),
		HTTPClient: newHTTPClient(),
	}
}

func TestPostJSONRetriesTransientServerErrors(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"success":true}`)
	}))
	defer srv.Close()

	c := testClient(t, srv.URL)
	resp, err := c.postJSON(context.Background(), srv.URL, []byte(`{}`))
	if err != nil {
		t.Fatalf("postJSON: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Errorf("attempts = %d, want 3 (two failures then success)", got)
	}
}

func TestPostJSONDoesNotRetryClientErrors(t *testing.T) {
	// 402 means "subscribe", 401 means "log in" — retrying just hammers the
	// endpoint and delays the message the user needs to see.
	for _, code := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusForbidden, http.StatusNotFound} {
		var attempts int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&attempts, 1)
			w.WriteHeader(code)
		}))

		c := testClient(t, srv.URL)
		resp, err := c.postJSON(context.Background(), srv.URL, []byte(`{}`))
		if err != nil {
			t.Fatalf("status %d: unexpected error %v", code, err)
		}
		resp.Body.Close()
		srv.Close()

		if got := atomic.LoadInt32(&attempts); got != 1 {
			t.Errorf("status %d: attempts = %d, want 1", code, got)
		}
	}
}

func TestPostJSONGivesUpAfterMaxAttempts(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := testClient(t, srv.URL)
	resp, err := c.postJSON(context.Background(), srv.URL, []byte(`{}`))
	if err != nil {
		t.Fatalf("the final response should be returned, not an error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 surfaced to the caller", resp.StatusCode)
	}
	if got := atomic.LoadInt32(&attempts); got != maxAttempts {
		t.Errorf("attempts = %d, want %d", got, maxAttempts)
	}
}

func TestPostJSONHonorsRetryAfter(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := testClient(t, srv.URL)
	start := time.Now()
	resp, err := c.postJSON(context.Background(), srv.URL, []byte(`{}`))
	if err != nil {
		t.Fatalf("postJSON: %v", err)
	}
	defer resp.Body.Close()

	if elapsed := time.Since(start); elapsed < time.Second {
		t.Errorf("waited %v, want at least the 1s the server asked for", elapsed)
	}
}

func TestPostJSONSignalsRetriesToTheUI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	var notices []string
	c := testClient(t, srv.URL)
	c.OnRetry = func(attempt int, reason string) {
		notices = append(notices, fmt.Sprintf("%d:%s", attempt, reason))
	}

	resp, err := c.postJSON(context.Background(), srv.URL, []byte(`{}`))
	if err == nil {
		resp.Body.Close()
	}
	if len(notices) != maxAttempts-1 {
		t.Errorf("got %d retry notices (%v), want %d", len(notices), notices, maxAttempts-1)
	}
	for _, n := range notices {
		if !strings.Contains(n, "502") {
			t.Errorf("retry notice should say why: %q", n)
		}
	}
}

func TestPostJSONStopsImmediatelyWhenCancelled(t *testing.T) {
	// Pressing Esc mid-run must abort now, not after the backoff schedule.
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	c := testClient(t, srv.URL)
	c.OnRetry = func(attempt int, reason string) { cancel() }

	start := time.Now()
	_, err := c.postJSON(ctx, srv.URL, []byte(`{}`))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("cancellation took %v; it should not wait out the backoff", elapsed)
	}
	if got := atomic.LoadInt32(&attempts); got > 2 {
		t.Errorf("kept retrying after cancel: %d attempts", got)
	}
}

func TestBackoffGrowsAndIsCapped(t *testing.T) {
	var prev time.Duration
	for attempt := 1; attempt <= 6; attempt++ {
		d := backoffFor(attempt, 0)
		if d > maxBackoff+maxBackoff/2 {
			t.Errorf("attempt %d: backoff %v exceeds the cap", attempt, d)
		}
		if attempt > 1 && attempt < 5 && d < prev {
			t.Errorf("attempt %d: backoff %v did not grow from %v", attempt, d, prev)
		}
		prev = d
	}

	// A server hint wins, but only up to the cap.
	if got := backoffFor(1, 2*time.Second); got != 2*time.Second {
		t.Errorf("server hint ignored: %v", got)
	}
	if got := backoffFor(1, time.Hour); got != maxRetryAfter {
		t.Errorf("absurd Retry-After not capped: %v", got)
	}
}

func TestStreamStallGuardAbortsSilentStream(t *testing.T) {
	// Headers arrive, then the server goes quiet forever. Without the guard
	// the CLI would sit on a spinner indefinitely.
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-release // never sends a body
	}))
	defer srv.Close()
	defer close(release)

	c := testClient(t, srv.URL)
	resp, err := c.postJSONStream(context.Background(), srv.URL, []byte(`{}`))
	if err != nil {
		t.Fatalf("postJSONStream: %v", err)
	}
	defer resp.Body.Close()

	// Re-arm the guard with a short idle window for the test.
	if g, ok := resp.Body.(*stallGuard); ok {
		g.idle = 300 * time.Millisecond
		g.timer.Reset(g.idle)
	} else {
		t.Fatal("streaming body was not wrapped in a stall guard")
	}

	done := make(chan error, 1)
	go func() {
		buf := make([]byte, 64)
		_, readErr := resp.Body.Read(buf)
		done <- readErr
	}()

	select {
	case readErr := <-done:
		if readErr == nil {
			t.Error("expected the stalled read to fail")
		}
	case <-time.After(5 * time.Second):
		t.Error("stall guard did not abort a silent stream")
	}
}

// A server that sends headers immediately and then keeps the connection alive
// with SSE comments while it thinks must not trip either the header timeout or
// the stall watchdog — this is the shape of a long generation.
func TestSlowServerWithHeartbeatsIsNotKilled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		flusher.Flush()

		// Three heartbeats standing in for a long model call, then the answer.
		for i := 0; i < 3; i++ {
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
			time.Sleep(50 * time.Millisecond)
		}
		fmt.Fprint(w, "data: {\"type\":\"text\",\"text\":\"finally\"}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	c := testClient(t, srv.URL)
	resp, err := c.postJSONStream(context.Background(), srv.URL, []byte(`{}`))
	if err != nil {
		t.Fatalf("postJSONStream: %v", err)
	}
	defer resp.Body.Close()

	msg, err := parseMessageResponse(resp, nil)
	if err != nil {
		t.Fatalf("parseMessageResponse: %v", err)
	}
	if msg.TextContent() != "finally" {
		t.Errorf("text = %q, want the answer that arrived after the heartbeats", msg.TextContent())
	}
}
