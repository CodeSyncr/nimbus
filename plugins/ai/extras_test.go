package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

// ── Tool choice ─────────────────────────────────────────────────────

func TestToolChoiceOnEachProvider(t *testing.T) {
	tools := []ToolSpec{{Name: "lookup", Parameters: json.RawMessage(`{"type":"object"}`)}}
	msgs := []openai.ChatCompletionMessage{{Role: "user", Content: "q"}}

	if r := openAIChatRequest(&GenerateRequest{Tools: tools, ToolChoice: ToolChoiceRequired}, "m", msgs, 10, false); r.ToolChoice != "required" {
		t.Fatalf("openai required = %v", r.ToolChoice)
	}
	if r := openAIChatRequest(&GenerateRequest{Tools: tools, ToolChoice: "lookup"}, "m", msgs, 10, false); r.ToolChoice.(openai.ToolChoice).Function.Name != "lookup" {
		t.Fatalf("openai named = %v", r.ToolChoice)
	}

	ap := &anthropicProvider{model: "c", maxTokens: 10}
	if tc := ap.buildRequest(&GenerateRequest{Tools: tools, ToolChoice: ToolChoiceRequired}, false).ToolChoice; tc == nil || tc.Type != "any" {
		t.Fatalf("anthropic required = %+v", tc)
	}
	if tc := ap.buildRequest(&GenerateRequest{Tools: tools, ToolChoice: "lookup"}, false).ToolChoice; tc == nil || tc.Type != "tool" || tc.Name != "lookup" {
		t.Fatalf("anthropic named = %+v", tc)
	}
	if tc := ap.buildRequest(&GenerateRequest{Tools: tools, ToolChoice: "lookup", Reasoning: &Reasoning{Effort: "low"}}, false).ToolChoice; tc != nil {
		t.Fatalf("anthropic must not force a tool with thinking on: %+v", tc)
	}

	gp := &geminiProvider{model: "g"}
	gc := gp.buildRequest(&GenerateRequest{Tools: tools, ToolChoice: "lookup"}).ToolConfig
	if gc == nil || gc.FunctionCallingConfig.Mode != "ANY" || gc.FunctionCallingConfig.AllowedFunctionNames[0] != "lookup" {
		t.Fatalf("gemini named = %+v", gc)
	}
	if gc := gp.buildRequest(&GenerateRequest{Tools: tools, ToolChoice: ToolChoiceNone}).ToolConfig; gc.FunctionCallingConfig.Mode != "NONE" {
		t.Fatalf("gemini none = %+v", gc)
	}

	cp := &cohereProvider{model: "command"}
	if cr := cp.buildRequest(&GenerateRequest{Tools: tools, ToolChoice: ToolChoiceRequired}, false); cr.ToolChoice != "REQUIRED" {
		t.Fatalf("cohere = %q", cr.ToolChoice)
	}
	op := &ollamaProvider{model: "llama"}
	if b := op.buildRequest(&GenerateRequest{Tools: tools, ToolChoice: ToolChoiceNone}, false); len(b.Tools) != 0 {
		t.Fatal("ollama should offer no tools for ToolChoiceNone")
	}
}

func TestAgentForcesAToolOnlyOnTheFirstStep(t *testing.T) {
	fake := NewFake(FakeToolCall("echo", `{"q":"x"}`), FakeText("done"))
	_, err := NewAgent("x").WithClient(fake.Client()).WithToolObjects(echoTool(t)).
		Prompt(context.Background(), "go", WithToolChoice("echo"))
	if err != nil {
		t.Fatal(err)
	}
	reqs := fake.Requests()
	if reqs[0].ToolChoice != "echo" || reqs[1].ToolChoice != "" {
		t.Fatalf("tool choice per step = %q, %q", reqs[0].ToolChoice, reqs[1].ToolChoice)
	}
}

// ── Parallel tools ──────────────────────────────────────────────────

func TestParallelToolsRunConcurrentlyAndKeepOrder(t *testing.T) {
	var running, peak int32
	type in struct {
		N int `json:"n"`
	}
	slow, _ := NewTool("slow").Desc("slow").Handler(func(_ context.Context, v in) (int, error) {
		n := atomic.AddInt32(&running, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		atomic.AddInt32(&running, -1)
		return v.N * 10, nil
	}).Build()
	fake := NewFake(FakeToolCalls(
		ToolCall{ID: "a", Name: "slow", Args: []byte(`{"n":1}`)},
		ToolCall{ID: "b", Name: "slow", Args: []byte(`{"n":2}`)},
		ToolCall{ID: "c", Name: "slow", Args: []byte(`{"n":3}`)},
	), FakeText("ok"))
	start := time.Now()
	_, err := NewAgent("x").WithClient(fake.Client()).WithToolObjects(slow).WithParallelTools().Prompt(context.Background(), "go")
	if err != nil {
		t.Fatal(err)
	}
	if peak < 2 || time.Since(start) > 140*time.Millisecond {
		t.Fatalf("peak concurrency %d, took %v", peak, time.Since(start))
	}
	var got []string
	for _, m := range fake.Requests()[1].Messages {
		if m.Role == RoleTool {
			got = append(got, m.ToolCallID+"="+m.Content)
		}
	}
	if strings.Join(got, ",") != "a=10,b=20,c=30" {
		t.Fatalf("results = %v", got)
	}
}

// ── Ollama ──────────────────────────────────────────────────────────

func TestOllamaToolsImagesAndThinking(t *testing.T) {
	var mu sync.Mutex
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		mu.Lock()
		bodies = append(bodies, m)
		n := len(bodies)
		mu.Unlock()
		if n == 1 {
			_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"","thinking":"use echo","tool_calls":[{"function":{"name":"echo","arguments":{"q":"hi"}}}]},"done":true,"prompt_eval_count":5,"eval_count":3}`))
			return
		}
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"echoed"},"done":true,"done_reason":"stop"}`))
	}))
	defer srv.Close()
	client := &Client{provider: &ollamaProvider{baseURL: srv.URL, model: "qwen3", client: http.DefaultClient}, config: &Config{MaxTokens: 50}}
	resp, err := NewAgent("x").WithClient(client).WithToolObjects(echoTool(t)).WithReasoning("low").
		Prompt(context.Background(), "echo hi", WithImages([]string{tinyPNG}))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "echoed" {
		t.Fatalf("resp = %q", resp.Text)
	}
	first := bodies[0]
	if first["think"] != true || len(first["tools"].([]any)) != 1 {
		t.Fatalf("first request = %v", first)
	}
	var user map[string]any
	for _, m := range first["messages"].([]any) {
		if m.(map[string]any)["role"] == "user" {
			user = m.(map[string]any)
		}
	}
	if imgs, _ := user["images"].([]any); len(imgs) != 1 || strings.HasPrefix(imgs[0].(string), "data:") {
		t.Fatalf("images should be bare base64: %v", user["images"])
	}
	msgs := bodies[1]["messages"].([]any)
	tool := msgs[len(msgs)-1].(map[string]any)
	if tool["role"] != "tool" || tool["tool_name"] != "echo" || tool["content"] != `"echo:hi"` {
		t.Fatalf("tool message = %v", tool)
	}
}

// ── Cohere ──────────────────────────────────────────────────────────

func TestCohereToolTurnsAndStringArguments(t *testing.T) {
	cp := &cohereProvider{model: "command-r"}
	cr := cp.buildRequest(&GenerateRequest{Model: "command-a", Messages: []Message{
		{Role: RoleUser, Content: "q"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "t1", Name: "f", Args: json.RawMessage(`{"a":1}`)}}},
		{Role: RoleTool, ToolCallID: "t1", Content: "r"},
	}}, false)
	if cr.Model != "command-a" {
		t.Fatalf("model = %s", cr.Model)
	}
	if tc := cr.Messages[1].ToolCalls; len(tc) != 1 || tc[0].Function.Arguments != `{"a":1}` {
		t.Fatalf("assistant tool calls = %+v", cr.Messages[1])
	}
	if cr.Messages[2].ToolCallID != "t1" {
		t.Fatalf("tool result lost its id: %+v", cr.Messages[2])
	}
	if got := string(cohereArgs(json.RawMessage(`"{\"q\":\"x\"}"`))); got != `{"q":"x"}` {
		t.Fatalf("string arguments = %s", got)
	}
}

// ── Rerank ──────────────────────────────────────────────────────────

func TestCohereRerankAndRAG(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Documents []string `json:"documents"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		// Score documents mentioning "password" highest.
		type res struct {
			Index int     `json:"index"`
			Score float64 `json:"relevance_score"`
		}
		var out []res
		for i, d := range body.Documents {
			s := 0.1
			if strings.Contains(d, "password") {
				s = 0.9
			}
			out = append(out, res{i, s})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": out})
	}))
	defer srv.Close()
	prev := cohereRerankURL
	cohereRerankURL = srv.URL
	defer func() { cohereRerankURL = prev }()

	reranker := &Client{provider: &cohereProvider{apiKey: "k", model: "command"}, config: &Config{}}
	docs := []string{"Billing runs monthly.", "Reset your password from settings.", "We ship worldwide."}
	ranked, err := reranker.Rerank(context.Background(), "forgot password", docs, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(ranked) != 1 || ranked[0].Index != 1 || ranked[0].Score != 0.9 {
		t.Fatalf("ranked = %+v", ranked)
	}

	fake := NewFake(FakeText("Use settings to reset it."))
	defer fake.Install()()
	store := VectorStoreInstance("kb")
	for i, d := range docs {
		if err := store.Add(context.Background(), string(rune('a'+i)), d); err != nil {
			t.Fatal(err)
		}
	}
	resp, err := NewRAG(store).TopK(3).WithReranker(reranker, 1).Ask(context.Background(), "How do I reset my password?")
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Sources) != 1 || !strings.Contains(resp.Sources[0].Text, "password") {
		t.Fatalf("sources = %+v", resp.Sources)
	}
}

func TestRerankUnsupportedProvider(t *testing.T) {
	if _, err := NewFake().Client().Rerank(context.Background(), "q", []string{"a"}, 1); err == nil {
		t.Fatal("expected an error")
	}
}
