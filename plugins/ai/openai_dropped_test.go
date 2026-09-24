package ai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// A server that hangs up on the first request without answering, the way
// DeepSeek did under load ("unexpected EOF"), and answers the second.
func hangUpOnce(t *testing.T, stream bool) (*httptest.Server, *int32) {
	return hangUp(t, stream, 1)
}

// hangUp hangs up on the first `drops` requests and answers the rest.
func hangUp(t *testing.T, stream bool, drops int32) (*httptest.Server, *int32) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&n, 1) <= drops {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				conn.Close()
			}
			return
		}
		_, _ = io.ReadAll(r.Body)
		if stream {
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, `data: {"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"back"}}]}`+"\n\n")
			io.WriteString(w, "data: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"back"},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

func TestStreamRetriesADroppedConnection(t *testing.T) {
	srv, n := hangUpOnce(t, true)
	p, err := newOpenAIProvider(&Config{OpenAIKey: "k", OpenAIBaseURL: srv.URL, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	sr, err := p.Stream(context.Background(), &GenerateRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}, MaxTokens: 16})
	if err != nil {
		t.Fatal(err)
	}
	var got strings.Builder
	for c := range sr.Chunks {
		got.WriteString(c.Text)
	}
	if e := <-sr.Err; e != nil {
		t.Fatalf("stream failed: %v", e)
	}
	if got.String() != "back" || atomic.LoadInt32(n) != 2 {
		t.Errorf("got %q after %d requests, want \"back\" after 2", got.String(), atomic.LoadInt32(n))
	}
}

func TestGenerateRetriesADroppedConnection(t *testing.T) {
	srv, n := hangUpOnce(t, false)
	p, err := newOpenAIProvider(&Config{OpenAIKey: "k", OpenAIBaseURL: srv.URL, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.Generate(context.Background(), &GenerateRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}, MaxTokens: 16})
	if err != nil || res.Text != "back" || atomic.LoadInt32(n) != 2 {
		t.Errorf("got %v %+v after %d requests", err, res, atomic.LoadInt32(n))
	}
}

func TestDroppedConnectionIsNotAnyError(t *testing.T) {
	for _, s := range []string{"status code: 400, bad request", "invalid api key", "context canceled"} {
		if droppedConnection(errString(s)) {
			t.Errorf("%q counted as a dropped connection", s)
		}
	}
	for _, s := range []string{`Post "https://api.deepseek.com/chat/completions": unexpected EOF`, "read tcp: connection reset by peer", `Post "x": EOF`} {
		if !droppedConnection(errString(s)) {
			t.Errorf("%q not counted as a dropped connection", s)
		}
	}
}

type errString string

func (e errString) Error() string { return string(e) }

// DeepSeek under load hung up several times in a row before answering.
func TestStreamOutlastsARunOfDroppedConnections(t *testing.T) {
	srv, n := hangUp(t, true, 4)
	p, err := newOpenAIProvider(&Config{OpenAIKey: "k", OpenAIBaseURL: srv.URL, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	sr, err := p.Stream(context.Background(), &GenerateRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}, MaxTokens: 16})
	if err != nil {
		t.Fatal(err)
	}
	var got strings.Builder
	for c := range sr.Chunks {
		got.WriteString(c.Text)
	}
	if e := <-sr.Err; e != nil || got.String() != "back" || atomic.LoadInt32(n) != 5 {
		t.Errorf("got %q err %v after %d requests, want \"back\" after 5", got.String(), e, atomic.LoadInt32(n))
	}
}

// It does not go on for ever: after five hang-ups the error comes back.
func TestStreamGivesUpAfterFiveDrops(t *testing.T) {
	srv, n := hangUp(t, true, 100)
	p, err := newOpenAIProvider(&Config{OpenAIKey: "k", OpenAIBaseURL: srv.URL, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	sr, err := p.Stream(context.Background(), &GenerateRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}, MaxTokens: 16})
	if err != nil {
		t.Fatal(err)
	}
	for range sr.Chunks {
	}
	if e := <-sr.Err; e == nil || atomic.LoadInt32(n) != 5 {
		t.Errorf("err %v after %d requests, want an error after 5", e, atomic.LoadInt32(n))
	}
}
