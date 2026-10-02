package ai

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// flakyProvider fails with the given errors, then answers.
type flakyProvider struct {
	errs  []error
	calls int32
}

func (f *flakyProvider) Name() string { return "flaky" }
func (f *flakyProvider) Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
	n := int(atomic.AddInt32(&f.calls, 1))
	if n <= len(f.errs) {
		return nil, f.errs[n-1]
	}
	return &GenerateResponse{Text: "ok"}, nil
}
func (f *flakyProvider) Stream(ctx context.Context, req *GenerateRequest) (*StreamResponse, error) {
	resp, err := f.Generate(ctx, req)
	if err != nil {
		return nil, err
	}
	ch := make(chan StreamChunk, 1)
	ch <- StreamChunk{Text: resp.Text, Done: true}
	close(ch)
	errc := make(chan error, 1)
	errc <- nil
	return &StreamResponse{Chunks: ch, Err: errc}, nil
}

func noSleep(t *testing.T) *[]time.Duration {
	t.Helper()
	var waits []time.Duration
	prev := sleepFn
	sleepFn = func(ctx context.Context, d time.Duration) error { waits = append(waits, d); return ctx.Err() }
	t.Cleanup(func() { sleepFn = prev })
	return &waits
}

func clientFor(p Provider, maxRetries int) *Client {
	return &Client{provider: p, config: &Config{Provider: "test", Model: "m", MaxTokens: 10, MaxRetries: maxRetries}}
}

func TestRetriesRateLimitsAndHonoursRetryAfter(t *testing.T) {
	waits := noSleep(t)
	p := &flakyProvider{errs: []error{
		&APIError{Provider: "x", StatusCode: 429, RetryAfter: 3 * time.Second},
		&APIError{Provider: "x", StatusCode: 529},
	}}
	resp, err := clientFor(p, 0).Generate(context.Background(), "hi")
	if err != nil || resp.Text != "ok" {
		t.Fatalf("Generate = %v, %v", resp, err)
	}
	if p.calls != 3 {
		t.Fatalf("calls = %d, want 3", p.calls)
	}
	if (*waits)[0] != 3*time.Second {
		t.Fatalf("first wait = %v, want the server's Retry-After", (*waits)[0])
	}
	if w := (*waits)[1]; w < 500*time.Millisecond || w > time.Second {
		t.Fatalf("second wait = %v, want backoff around 0.5–1s", w)
	}
}

func TestDoesNotRetryClientErrors(t *testing.T) {
	noSleep(t)
	p := &flakyProvider{errs: []error{&APIError{Provider: "x", StatusCode: 400, Body: "bad request"}}}
	_, err := clientFor(p, 0).Generate(context.Background(), "hi")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 400 || p.calls != 1 {
		t.Fatalf("err=%v calls=%d", err, p.calls)
	}
}

func TestGivesUpAfterMaxRetries(t *testing.T) {
	noSleep(t)
	e := &APIError{Provider: "x", StatusCode: 503}
	p := &flakyProvider{errs: []error{e, e, e, e, e}}
	if _, err := clientFor(p, 0).Generate(context.Background(), "hi"); err == nil {
		t.Fatal("expected an error")
	}
	if p.calls != 1+DefaultMaxRetries {
		t.Fatalf("calls = %d, want %d", p.calls, 1+DefaultMaxRetries)
	}
	p2 := &flakyProvider{errs: []error{e}}
	if _, err := clientFor(p2, -1).Generate(context.Background(), "hi"); err == nil || p2.calls != 1 {
		t.Fatalf("MaxRetries<0 should not retry: calls=%d", p2.calls)
	}
}

func TestStreamRetriesWhenItFailsToStart(t *testing.T) {
	noSleep(t)
	p := &flakyProvider{errs: []error{&APIError{Provider: "x", StatusCode: 502}}}
	st, err := clientFor(p, 0).StreamRequest(context.Background(), &GenerateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := st.Collect(context.Background())
	if err != nil || resp.Text != "ok" || p.calls != 2 {
		t.Fatalf("resp=%v err=%v calls=%d", resp, err, p.calls)
	}
}

func TestSelfRetryingProviderIsNotRetriedAgain(t *testing.T) {
	c := &Client{provider: &openAIProvider{}, config: &Config{}}
	if c.maxRetries() != 0 {
		t.Fatal("the OpenAI provider retries itself; the client must not multiply its attempts")
	}
}

func TestRetryableClassification(t *testing.T) {
	yes := []error{
		&APIError{StatusCode: 429}, &APIError{StatusCode: 500}, &APIError{StatusCode: 529}, &APIError{StatusCode: 408},
		errors.New("read tcp: connection reset by peer"), errors.New("anthropic error (529): overloaded"),
	}
	no := []error{&APIError{StatusCode: 400}, &APIError{StatusCode: 401}, &APIError{StatusCode: 404},
		errors.New("ai: ANTHROPIC_API_KEY is required"), context.Canceled, nil}
	for _, e := range yes {
		if ok, _ := Retryable(e); !ok {
			t.Errorf("%v should be retryable", e)
		}
	}
	for _, e := range no {
		if ok, _ := Retryable(e); ok {
			t.Errorf("%v should not be retryable", e)
		}
	}
}

func TestParseRetryAfterAndAPIErrorFromResponse(t *testing.T) {
	if d := parseRetryAfter("2.5"); d != 2500*time.Millisecond {
		t.Fatalf("parseRetryAfter(2.5) = %v", d)
	}
	future := time.Now().Add(10 * time.Second).UTC().Format(http.TimeFormat)
	if d := parseRetryAfter(future); d < 8*time.Second || d > 11*time.Second {
		t.Fatalf("HTTP-date Retry-After = %v", d)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"error":"slow down"}`))
	}))
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	e := newAPIError("anthropic", resp)
	if e.StatusCode != 429 || e.RetryAfter != 7*time.Second || !strings.Contains(e.Error(), "slow down") || !strings.HasPrefix(e.Error(), "anthropic error (429)") {
		t.Fatalf("APIError = %+v / %q", e, e.Error())
	}
}

func TestAnthropicProviderReturnsTypedErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(529)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"overloaded_error"}}`))
	}))
	defer srv.Close()
	p, err := newAnthropicProvider(&Config{AnthropicKey: "k", AnthropicAPIURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Generate(context.Background(), &GenerateRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}, MaxTokens: 5})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 529 || apiErr.RetryAfter != time.Second {
		t.Fatalf("err = %#v", err)
	}
}
