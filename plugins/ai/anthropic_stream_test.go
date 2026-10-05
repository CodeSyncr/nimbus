package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
