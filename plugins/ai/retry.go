/*
|--------------------------------------------------------------------------
| AI SDK — Retries
|--------------------------------------------------------------------------
|
| Rate limits (429), overloaded or failing servers (5xx, Anthropic's 529)
| and dropped connections are retried with exponential backoff and jitter,
| honouring the server's Retry-After. Other errors (a bad request, a wrong
| key) fail at once. Config.MaxRetries sets the attempts after the first
| (default 2; AI_MAX_RETRIES; negative turns retries off).
|
| A stream is retried only when it fails to start; once text has reached
| the caller it is not replayed.
|
*/

package ai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// DefaultMaxRetries is the number of retries after the first attempt when
// Config.MaxRetries is zero.
const DefaultMaxRetries = 2

// APIError is a non-success HTTP response from a provider.
type APIError struct {
	Provider   string
	StatusCode int
	Body       string
	// RetryAfter is the wait the server asked for (Retry-After header), or 0.
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s error (%d): %s", e.Provider, e.StatusCode, e.Body)
}

// newAPIError reads a failed response into an APIError.
func newAPIError(provider string, resp *http.Response) *APIError {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	return &APIError{
		Provider:   provider,
		StatusCode: resp.StatusCode,
		Body:       string(body),
		RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
	}
}

func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.ParseFloat(v, 64); err == nil && secs >= 0 {
		return time.Duration(secs * float64(time.Second))
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// selfRetrying is implemented by providers that retry on their own (the
// OpenAI provider), so the client does not retry on top of them.
type selfRetrying interface {
	retriesItself() bool
}

func (p *openAIProvider) retriesItself() bool { return true }

// Retryable reports whether err is worth another attempt, and how long the
// server asked to wait (0 when it did not say).
func Retryable(err error) (bool, time.Duration) {
	if err == nil || errors.Is(err, context.Canceled) {
		return false, 0
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.StatusCode == http.StatusTooManyRequests,
			apiErr.StatusCode == http.StatusRequestTimeout,
			apiErr.StatusCode == 529, // Anthropic: overloaded
			apiErr.StatusCode >= 500:
			return true, apiErr.RetryAfter
		}
		return false, 0
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED) {
		return true, 0
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true, 0
	}
	msg := strings.ToLower(err.Error())
	for _, s := range []string{"connection reset", "broken pipe", "unexpected eof", "(429)", "(500)", "(502)", "(503)", "(504)", "(529)", "overloaded"} {
		if strings.Contains(msg, s) {
			return true, 0
		}
	}
	return false, 0
}

// backoff is the wait before retry number attempt (1-based): exponential
// from 500ms with jitter, capped at 20s; a Retry-After of up to a minute
// wins.
func backoff(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 && retryAfter <= time.Minute {
		return retryAfter
	}
	d := 500 * time.Millisecond << (attempt - 1)
	if d > 20*time.Second || d <= 0 {
		d = 20 * time.Second
	}
	return d/2 + time.Duration(rand.Int64N(int64(d/2)+1))
}

// sleepFn is replaced in tests.
var sleepFn = func(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (c *Client) maxRetries() int {
	if _, ok := c.provider.(selfRetrying); ok {
		return 0
	}
	n := DefaultMaxRetries
	if c.config != nil && c.config.MaxRetries != 0 {
		n = c.config.MaxRetries
	}
	if n < 0 {
		return 0
	}
	return n
}

// withRetries runs call, retrying transient failures.
func withRetries[T any](ctx context.Context, max int, call func() (T, error)) (T, error) {
	var (
		out T
		err error
	)
	for attempt := 0; ; attempt++ {
		out, err = call()
		if err == nil {
			return out, nil
		}
		ok, wait := Retryable(err)
		if !ok || attempt >= max || ctx.Err() != nil {
			return out, err
		}
		if sErr := sleepFn(ctx, backoff(attempt+1, wait)); sErr != nil {
			return out, err
		}
	}
}
