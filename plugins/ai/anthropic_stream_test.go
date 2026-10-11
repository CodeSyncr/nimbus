package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func sse(events ...string) string {
	var b strings.Builder
	for _, e := range events {
		b.WriteString("event: x\ndata: " + e + "\n\n")
	}
	return b.String()
}

var anthropicToolStream = sse(
	`{"type":"message_start","message":{"usage":{"input_tokens":12,"output_tokens":1}}}`,
	`{"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}`,
	`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Need the "}}`,
	`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"weather."}}`,
	`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sigX"}}`,
	`{"type":"content_block_stop","index":0}`,
	`{"type":"content_block_start","index":1,"content_block":{"type":"text"}}`,
	`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Checking"}}`,
	`{"type":"content_block_stop","index":1}`,
	`{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_1","name":"echo"}}`,
	`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"q\":"}}`,
	`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"\"Pune\"}"}}`,
	`{"type":"content_block_stop","index":2}`,
	`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":30}}`,
	`{"type":"message_stop"}`,
)

func TestAnthropicStreamsToolCallsAndThinking(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(anthropicToolStream))
	}))
	defer srv.Close()
	p, _ := newAnthropicProvider(&Config{AnthropicKey: "k", AnthropicAPIURL: srv.URL})
	st, err := p.Stream(context.Background(), &GenerateRequest{Messages: []Message{{Role: RoleUser, Content: "weather?"}}})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := st.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "Checking" || len(resp.ToolCalls) != 1 || string(resp.ToolCalls[0].Args) != `{"q":"Pune"}` || resp.ToolCalls[0].ID != "toolu_1" {
		t.Fatalf("resp = %+v", resp)
	}
	if len(resp.ReasoningBlocks) != 1 || resp.ReasoningBlocks[0].Text != "Need the weather." || resp.ReasoningBlocks[0].Signature != "sigX" {
		t.Fatalf("reasoning = %+v", resp.ReasoningBlocks)
	}
	if resp.Usage == nil || resp.Usage.CompletionTokens != 30 {
		t.Fatalf("usage = %+v", resp.Usage)
	}
}

func TestAgentStreamsEveryStepOnAnthropic(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "text/event-stream")
		if calls == 1 {
			_, _ = w.Write([]byte(anthropicToolStream))
			return
		}
		_, _ = w.Write([]byte(sse(
			`{"type":"message_start","message":{"usage":{"input_tokens":20,"output_tokens":1}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"text"}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Sunny in Pune."}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"message_stop"}`,
		)))
	}))
	defer srv.Close()
	p, _ := newAnthropicProvider(&Config{AnthropicKey: "k", AnthropicAPIURL: srv.URL})
	mem := MemoryStore()
	agent := NewAgent("x").WithClient(&Client{provider: p, config: &Config{Provider: "anthropic", MaxTokens: 100}}).
		WithToolObjects(echoTool(t)).WithMemory(mem, "c")
	st, err := agent.Stream(context.Background(), "weather?")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := st.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || !strings.Contains(resp.Text, "Sunny in Pune.") {
		t.Fatalf("calls=%d text=%q", calls, resp.Text)
	}
	saved, _ := mem.Load(context.Background(), "c")
	if asst := saved[1]; len(asst.Reasoning) != 1 || asst.Reasoning[0].Signature != "sigX" {
		t.Fatalf("streamed tool turn lost its thinking: %+v", asst)
	}
}

// A relay in front of another model sends zeros in message_start and the
// real input and cache counts in message_delta, at the end. And while a
// tool's arguments are written, each piece is reported as progress.
func TestAnthropicStreamReadsLateUsageAndReportsToolProgress(t *testing.T) {
	stream := sse(
		`{"type":"message_start","message":{"usage":{"input_tokens":0,"output_tokens":0}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_9","name":"write_file"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"a.txt\","}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"content\":\"bread\"}"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"cache_read_input_tokens":18176,"input_tokens":215,"output_tokens":107}}`,
		`{"type":"message_stop"}`,
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(stream))
	}))
	defer srv.Close()
	p, _ := newAnthropicProvider(&Config{AnthropicKey: "k", AnthropicAPIURL: srv.URL})
	st, err := p.Stream(context.Background(), &GenerateRequest{Messages: []Message{{Role: RoleUser, Content: "write it"}}})
	if err != nil {
		t.Fatal(err)
	}
	var progress, name string
	var calls []ToolCall
	var usage *Usage
	for c := range st.Chunks {
		progress += c.ToolArgsDelta
		if c.ToolName != "" {
			name = c.ToolName
		}
		calls = append(calls, c.ToolCalls...)
		if c.Usage != nil {
			usage = c.Usage
		}
	}
	if progress != `{"path":"a.txt","content":"bread"}` || name != "write_file" {
		t.Errorf("progress = %q for %q", progress, name)
	}
	if len(calls) != 1 || string(calls[0].Args) != progress {
		t.Errorf("the finished call must still arrive whole: %+v", calls)
	}
	if usage == nil || usage.PromptTokens != 18391 || usage.CacheReadTokens != 18176 || usage.CompletionTokens != 107 {
		t.Fatalf("usage = %+v", usage)
	}
}

// A reply cut at the output limit says so on its last chunk, and the tool
// call it was writing arrives with only the first part of its arguments:
// the caller is told, so it does not run half a file.
func TestAnthropicStreamSaysWhenItWasCutOff(t *testing.T) {
	stream := sse(
		`{"type":"message_start","message":{"usage":{"input_tokens":40,"output_tokens":0}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_3","name":"write_file"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"a.html\",\"content\":\"<html><bo"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"max_tokens"},"usage":{"output_tokens":8192}}`,
		`{"type":"message_stop"}`,
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(stream))
	}))
	defer srv.Close()
	p, _ := newAnthropicProvider(&Config{AnthropicKey: "k", AnthropicAPIURL: srv.URL})
	st, err := p.Stream(context.Background(), &GenerateRequest{Messages: []Message{{Role: RoleUser, Content: "write it"}}})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := st.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if resp.FinishReason != "max_tokens" || !Truncated(resp.FinishReason) {
		t.Fatalf("finish reason = %q", resp.FinishReason)
	}
	if len(resp.ToolCalls) != 1 || json.Valid(resp.ToolCalls[0].Args) {
		t.Fatalf("the cut-off call arrives as it was written, not repaired: %+v", resp.ToolCalls)
	}
	for _, ended := range []string{"end_turn", "tool_use", "stop", "tool_calls", ""} {
		if Truncated(ended) {
			t.Errorf("%q is not a cut-off", ended)
		}
	}
	if !Truncated("length") {
		t.Error(`"length" is how OpenAI-compatible APIs say it`)
	}
}

// A key that is not scoped to one workspace is refused by Anthropic unless
// the request names the workspace. The header goes on every call, streamed
// or not, when one is set, and is absent otherwise.
func TestAnthropicSendsTheWorkspaceWhenOneIsSet(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("anthropic-workspace-id"))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(anthropicToolStream))
	}))
	defer srv.Close()
	ask := func(cfg *Config) {
		cfg.AnthropicKey, cfg.AnthropicAPIURL = "k", srv.URL
		p, err := newAnthropicProvider(cfg)
		if err != nil {
			t.Fatal(err)
		}
		st, err := p.Stream(context.Background(), &GenerateRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.Collect(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("ANTHROPIC_WORKSPACE_ID", "")
	ask(&Config{})
	ask(&Config{AnthropicWorkspace: "wrkspc_01abc"})
	t.Setenv("ANTHROPIC_WORKSPACE_ID", "wrkspc_from_env")
	ask(&Config{})
	if len(got) != 3 || got[0] != "" || got[1] != "wrkspc_01abc" || got[2] != "wrkspc_from_env" {
		t.Errorf("workspace header on the three calls: %q", got)
	}
}

// A fallback model on Anthropic with a key of its own sends its own
// workspace, or none: never the base model's, which is another account's.
func TestTheFallbackModelSendsItsOwnWorkspace(t *testing.T) {
	seen := map[string]string{} // model → workspace header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		seen[body.Model] = r.Header.Get("anthropic-workspace-id") + "|" + r.Header.Get("x-api-key")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(anthropicToolStream))
	}))
	defer srv.Close()
	t.Setenv("ANTHROPIC_WORKSPACE_ID", "wrkspc_base")
	ask := func(cfg *Config) {
		cfg.Provider, cfg.Model, cfg.MaxRetries = "anthropic", "main", -1
		cfg.AnthropicKey, cfg.AnthropicAPIURL, cfg.AnthropicWorkspace = "base-key", srv.URL, "wrkspc_base"
		cfg.FallbackModel, cfg.FallbackProvider, cfg.FallbackAPIKey = "spare", "anthropic", "spare-key"
		c, err := NewClient(cfg)
		if err != nil {
			t.Fatal(err)
		}
		for _, model := range []string{"main", "spare"} {
			st, err := c.StreamRequest(context.Background(), &GenerateRequest{Model: model, Messages: []Message{{Role: RoleUser, Content: "hi"}}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := st.Collect(context.Background()); err != nil {
				t.Fatal(err)
			}
		}
	}
	ask(&Config{FallbackWorkspace: "wrkspc_spare"})
	if seen["main"] != "wrkspc_base|base-key" || seen["spare"] != "wrkspc_spare|spare-key" {
		t.Errorf("with a fallback workspace: %v", seen)
	}
	ask(&Config{})
	if seen["main"] != "wrkspc_base|base-key" || seen["spare"] != "|spare-key" {
		t.Errorf("a fallback with its own key and no workspace borrowed the base's: %v", seen)
	}
}

// A stream that goes silent part-way is ended with an error that says so,
// and one that reports an error of its own is not passed off as a reply
// that simply stopped.
func TestAnthropicStreamThatStallsOrFailsSaysSo(t *testing.T) {
	old := streamIdleLimit
	streamIdleLimit = 150 * time.Millisecond
	t.Cleanup(func() { streamIdleLimit = old })

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	stalled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sse(`{"type":"message_start","message":{"usage":{"input_tokens":5}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"text"}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Half an "}}`)))
		w.(http.Flusher).Flush()
		select { // and then nothing, with the connection left open
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer stalled.Close()
	p, _ := newAnthropicProvider(&Config{AnthropicKey: "k", AnthropicAPIURL: stalled.URL})
	st, err := p.Stream(context.Background(), &GenerateRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	began := time.Now()
	_, err = st.Collect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "stalled") {
		t.Fatalf("a stalled stream should end with an error that says so: %v", err)
	}
	if time.Since(began) > 5*time.Second {
		t.Errorf("it took %s to give up on a stream that had stalled", time.Since(began))
	}

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sse(`{"type":"message_start","message":{"usage":{"input_tokens":5}}}`,
			`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`)))
	}))
	defer failing.Close()
	p, _ = newAnthropicProvider(&Config{AnthropicKey: "k", AnthropicAPIURL: failing.URL})
	st, err = p.Stream(context.Background(), &GenerateRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.Collect(context.Background()); err == nil || !strings.Contains(err.Error(), "overloaded_error") {
		t.Errorf("an error said mid-stream should be the stream's error: %v", err)
	}
}
