package ai

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

/*
Transport policy for the AI CLI.

An agent run is a sequence of long requests, and a single blip used to lose the
whole turn: one attempt, no backoff, and a flat 600s client timeout that could
neither fail fast on a dead server nor keep a legitimately long stream alive.

This file replaces that with:

  - retries with exponential backoff and jitter for transient failures only,
    honouring Retry-After when the server sends it;
  - fine-grained timeouts (dial, TLS, time-to-first-byte) instead of one
    deadline covering an entire streamed response;
  - a stall watchdog that aborts a stream which stops producing bytes, so a
    half-open connection cannot hang the CLI forever.

Retries stop at the response headers. Once the body starts streaming deltas to
the terminal, a retry would duplicate output, so a mid-stream failure is
returned to the caller instead.
*/

const (
	// maxAttempts counts the first try plus retries.
	maxAttempts = 4
	// baseBackoff is the first retry delay; it doubles per attempt.
	baseBackoff = 500 * time.Millisecond
	// maxBackoff caps a single wait.
	maxBackoff = 8 * time.Second
	// maxRetryAfter caps a server-provided Retry-After, so a bad header
	// cannot park the CLI for minutes.
	maxRetryAfter = 30 * time.Second

	// responseHeaderTimeout bounds time-to-first-byte.
	//
	// A server that flushes its SSE headers before generating answers in
	// milliseconds, but not every server does: one that generates first has a
	// time-to-first-byte equal to the whole model call, which on a large
	// request runs into minutes. The bound has to clear that, or the CLI kills
	// a request the server is still working on.
	responseHeaderTimeout = 300 * time.Second
	// streamIdleTimeout aborts a stream that goes quiet mid-flight.
	streamIdleTimeout = 180 * time.Second
)

// newHTTPClient builds the client used for every Nimbus Cloud call.
//
// It deliberately sets no Client.Timeout: that deadline covers reading the
// body, which would kill a long SSE stream mid-answer. Bounding the phases
// that can hang instead (dial, TLS, first byte) fails fast on a dead server
// while letting a healthy stream run as long as it needs.
func newHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: responseHeaderTimeout,
			ExpectContinueTimeout: 1 * time.Second,
			IdleConnTimeout:       90 * time.Second,
			MaxIdleConns:          10,
			ForceAttemptHTTP2:     true,
		},
	}
}

// postJSON sends body to url and returns the first response whose status is
// not retryable. The caller owns the response body.
//
// Every call is a fresh completion request, so replaying one is safe: there is
// no partially applied server state to reconcile.
func (c *NimbusCloudClient) postJSON(ctx context.Context, url string, body []byte) (*http.Response, error) {
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		c.setHeaders(req)
		if attempt > 1 {
			req.Header.Set("X-Nimbus-Retry", strconv.Itoa(attempt-1))
		}

		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			// A cancelled context is the user pressing Esc, not a fault.
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = fmt.Errorf("connection error to Nimbus Cloud (%s): %w", c.ServerURL, err)
			if !retryableErr(err) || attempt == maxAttempts {
				return nil, lastErr
			}
			if waitErr := sleepCtx(ctx, backoffFor(attempt, 0)); waitErr != nil {
				return nil, waitErr
			}
			c.retryNotice(attempt, lastErr.Error())
			continue
		}

		if !retryableStatus(resp.StatusCode) || attempt == maxAttempts {
			return resp, nil
		}

		wait := backoffFor(attempt, retryAfter(resp))
		reason := fmt.Sprintf("server returned %d", resp.StatusCode)
		drainAndClose(resp.Body)

		if waitErr := sleepCtx(ctx, wait); waitErr != nil {
			return nil, waitErr
		}
		c.retryNotice(attempt, reason)
		lastErr = errors.New(reason)
	}

	if lastErr == nil {
		lastErr = errors.New("request failed")
	}
	return nil, lastErr
}

// postJSONStream is postJSON for a streamed response. It arms a stall
// watchdog on the body: a connection that stops delivering bytes is aborted
// instead of leaving the CLI spinning on a half-open socket. Callers must
// close the returned body, which disarms the watchdog.
func (c *NimbusCloudClient) postJSONStream(ctx context.Context, url string, body []byte) (*http.Response, error) {
	streamCtx, cancel := context.WithCancel(ctx)

	resp, err := c.postJSON(streamCtx, url, body)
	if err != nil {
		cancel()
		return nil, err
	}
	resp.Body = guardStream(resp.Body, cancel, streamIdleTimeout)
	return resp, nil
}

// OnRetry, when set, is called before each retry so the UI can say why the
// CLI is pausing instead of appearing to hang.
type retryNotifier func(attempt int, reason string)

func (c *NimbusCloudClient) retryNotice(attempt int, reason string) {
	if c.OnRetry != nil {
		c.OnRetry(attempt, reason)
	}
}

// retryableStatus reports whether a status is worth another attempt.
// Client errors are not: a 400 stays a 400, and 401/402 need the user to log
// in or subscribe rather than the CLI hammering the endpoint.
func retryableStatus(code int) bool {
	switch code {
	case http.StatusRequestTimeout, // 408
		http.StatusTooManyRequests,     // 429
		http.StatusInternalServerError, // 500
		http.StatusBadGateway,          // 502
		http.StatusServiceUnavailable,  // 503
		http.StatusGatewayTimeout:      // 504
		return true
	}
	return false
}

// retryableErr reports whether a transport error is likely transient.
func retryableErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	// Connection reset / EOF mid-handshake surface as plain errors.
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed)
}

// backoffFor returns the wait before the next attempt: the server's
// Retry-After when it sent one, otherwise exponential backoff with jitter.
func backoffFor(attempt int, serverHint time.Duration) time.Duration {
	if serverHint > 0 {
		if serverHint > maxRetryAfter {
			return maxRetryAfter
		}
		return serverHint
	}
	wait := baseBackoff << (attempt - 1)
	if wait > maxBackoff {
		wait = maxBackoff
	}
	// Jitter spreads retries when several clients fail together.
	return wait + time.Duration(rand.Int63n(int64(wait/2+1)))
}

// retryAfter reads a Retry-After header in either supported form.
func retryAfter(resp *http.Response) time.Duration {
	v := resp.Header.Get("Retry-After")
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if when, err := http.ParseTime(v); err == nil {
		if d := time.Until(when); d > 0 {
			return d
		}
	}
	return 0
}

// sleepCtx waits, returning early if the context ends.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// drainAndClose consumes a little of the body so the connection can be reused,
// then closes it.
func drainAndClose(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 4<<10))
	_ = body.Close()
}

// stallGuard aborts a response body that stops producing data.
//
// ResponseHeaderTimeout only covers the first byte; without this a half-open
// connection mid-stream leaves the CLI waiting forever with a spinner up.
type stallGuard struct {
	body   io.ReadCloser
	cancel context.CancelFunc
	timer  *time.Timer
	idle   time.Duration

	mu     sync.Mutex
	closed bool
}

// guardStream wraps a streaming body so that idle seconds without any bytes
// cancel the request. Returns the wrapped body and a cancel-cleanup func.
func guardStream(body io.ReadCloser, cancel context.CancelFunc, idle time.Duration) io.ReadCloser {
	g := &stallGuard{body: body, cancel: cancel, idle: idle}
	g.timer = time.AfterFunc(idle, func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		if !g.closed {
			g.cancel() // unblocks the pending Read with a context error
		}
	})
	return g
}

func (g *stallGuard) Read(p []byte) (int, error) {
	n, err := g.body.Read(p)
	if n > 0 {
		g.mu.Lock()
		if !g.closed {
			g.timer.Reset(g.idle)
		}
		g.mu.Unlock()
	}
	return n, err
}

func (g *stallGuard) Close() error {
	g.mu.Lock()
	g.closed = true
	g.timer.Stop()
	g.mu.Unlock()
	return g.body.Close()
}
