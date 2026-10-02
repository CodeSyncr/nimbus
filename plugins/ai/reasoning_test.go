package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	openai "github.com/sashabaranov/go-openai"
)

func TestReasoningEffortAndBudget(t *testing.T) {
	cases := []struct {
		r      *Reasoning
		effort string
		budget int
	}{
		{&Reasoning{Effort: "high"}, "high", 24576},
		{&Reasoning{Effort: "low"}, "low", 2048},
		{&Reasoning{BudgetTokens: 10000}, "medium", 10000},
		{&Reasoning{BudgetTokens: 1000}, "low", 1000},
	}
	for _, c := range cases {
		if c.r.effort() != c.effort || c.r.budget() != c.budget {
			t.Errorf("%+v: effort=%s budget=%d", c.r, c.r.effort(), c.r.budget())
		}
	}
}

func TestAnthropicThinkingRequestAndResponse(t *testing.T) {
	p := &anthropicProvider{model: "claude-x", maxTokens: 4096}
	req := &GenerateRequest{
		Messages: []Message{
			{Role: RoleUser, Content: "weather?"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "t1", Name: "w", Args: json.RawMessage(`{}`)}},
				Reasoning: []ReasoningBlock{{Text: "I should call w", Signature: "sig1"}, {Redacted: "enc"}}},
			{Role: RoleTool, ToolCallID: "t1", Content: `"sunny"`},
		},
		Reasoning: &Reasoning{Effort: "medium"},
	}
	ar := p.buildRequest(req, false)
	if ar.Thinking == nil || ar.Thinking.BudgetTokens != 8192 || ar.MaxTokens <= 8192 {
		t.Fatalf("thinking=%+v max_tokens=%d", ar.Thinking, ar.MaxTokens)
	}
	asst := ar.Messages[1].Content
	if asst[0].Type != "thinking" || asst[0].Signature != "sig1" || asst[1].Type != "redacted_thinking" || asst[2].Type != "tool_use" {
		t.Fatalf("assistant turn must replay thinking first: %+v", asst)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"claude-x","stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":9},
		  "content":[{"type":"thinking","thinking":"Let me think.","signature":"s2"},{"type":"text","text":"42"}]}`))
	}))
	defer srv.Close()
	ap, _ := newAnthropicProvider(&Config{AnthropicKey: "k", AnthropicAPIURL: srv.URL})
	resp, err := ap.Generate(context.Background(), &GenerateRequest{Messages: []Message{{Role: RoleUser, Content: "q"}}, Reasoning: &Reasoning{Effort: "low"}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "42" || resp.Reasoning != "Let me think." || len(resp.ReasoningBlocks) != 1 || resp.ReasoningBlocks[0].Signature != "s2" {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestOpenAIReasoningRequest(t *testing.T) {
	msgs := []openai.ChatCompletionMessage{{Role: "user", Content: "q"}}
	r := openAIChatRequest(&GenerateRequest{Temperature: 0.7, Reasoning: &Reasoning{Effort: "high"}}, "o3", msgs, 2000, false)
	if r.ReasoningEffort != "high" || r.MaxCompletionTokens != 2000 || r.MaxTokens != 0 || r.Temperature != 0 {
		t.Fatalf("request = %+v", r)
	}
	plain := openAIChatRequest(&GenerateRequest{Temperature: 0.7}, "gpt-4o", msgs, 2000, false)
	if plain.ReasoningEffort != "" || plain.MaxTokens != 2000 || plain.Temperature != 0.7 {
		t.Fatalf("plain request changed: %+v", plain)
	}
}

func TestAgentKeepsReasoningWithToolTurns(t *testing.T) {
	first := FakeToolCall("echo", `{"q":"x"}`)
	first.ReasoningBlocks = []ReasoningBlock{{Text: "call echo", Signature: "sig"}}
	fake := NewFake(first, FakeText("done"))
	_, err := NewAgent("x").WithClient(fake.Client()).WithToolObjects(echoTool(t)).WithReasoning("low").
		Prompt(context.Background(), "go")
	if err != nil {
		t.Fatal(err)
	}
	second := fake.Requests()[1]
	if second.Reasoning == nil || second.Reasoning.Effort != "low" {
		t.Fatalf("reasoning not sent: %+v", second.Reasoning)
	}
	asst := second.Messages[1]
	if asst.Role != RoleAssistant || len(asst.Reasoning) != 1 || asst.Reasoning[0].Signature != "sig" {
		t.Fatalf("assistant turn lost its reasoning: %+v", asst)
	}
}

// ── Gemini ──────────────────────────────────────────────────────────

type geminiStub struct {
	mu    sync.Mutex
	paths []string
	body  []map[string]any
	reply func(path string) string
}

func (g *geminiStub) server(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		g.mu.Lock()
		g.paths = append(g.paths, r.URL.Path)
		g.body = append(g.body, m)
		g.mu.Unlock()
		if r.Header.Get("x-goog-api-key") != "k" {
			w.WriteHeader(401)
			return
		}
		_, _ = w.Write([]byte(g.reply(r.URL.Path)))
	}))
}

func TestGeminiToolsThinkingAndModelOverride(t *testing.T) {
	stub := &geminiStub{reply: func(string) string {
		return `{"modelVersion":"gemini-2.5-pro","candidates":[{"finishReason":"STOP","content":{"parts":[
		  {"text":"thinking about weather","thought":true,"thoughtSignature":"ts1"},
		  {"functionCall":{"name":"get_weather","args":{"city":"Pune"}},"thoughtSignature":"ts2"}]}}],
		  "usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":4,"thoughtsTokenCount":6,"totalTokenCount":20}}`
	}}
	srv := stub.server(t)
	defer srv.Close()
	p := &geminiProvider{apiKey: "k", model: "gemini-2.0-flash", baseURL: srv.URL}

	resp, err := p.Generate(context.Background(), &GenerateRequest{
		Model:     "gemini-2.5-pro",
		System:    "be brief",
		Messages:  []Message{{Role: RoleUser, Content: "Weather in Pune?"}},
		Tools:     []ToolSpec{{Name: "get_weather", Description: "weather", Parameters: json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"city":{"type":"string"}}}`)}},
		Reasoning: &Reasoning{Effort: "medium"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(stub.paths[0], "/models/gemini-2.5-pro:generateContent") {
		t.Fatalf("request model override ignored: %s", stub.paths[0])
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "get_weather" || string(resp.ToolCalls[0].Args) != `{"city":"Pune"}` || resp.ToolCalls[0].Signature != "ts2" {
		t.Fatalf("tool calls = %+v", resp.ToolCalls)
	}
	if resp.Reasoning != "thinking about weather" || resp.Text != "" {
		t.Fatalf("thought leaked into text: text=%q reasoning=%q", resp.Text, resp.Reasoning)
	}
	if u := resp.Usage; u.CompletionTokens != 10 || u.ReasoningTokens != 6 {
		t.Fatalf("usage = %+v", u)
	}
	sent := stub.body[0]
	gen := sent["generationConfig"].(map[string]any)
	if tc := gen["thinkingConfig"].(map[string]any); tc["thinkingBudget"].(float64) != 8192 {
		t.Fatalf("thinkingConfig = %v", tc)
	}
	decl := sent["tools"].([]any)[0].(map[string]any)["functionDeclarations"].([]any)[0].(map[string]any)
	if params, _ := json.Marshal(decl["parameters"]); strings.Contains(string(params), "additionalProperties") {
		t.Fatalf("unsupported schema keyword sent to Gemini: %s", params)
	}

	// The follow-up request sends the call back with its signature and the
	// result as a functionResponse named after the tool.
	_, err = p.Generate(context.Background(), &GenerateRequest{Messages: []Message{
		{Role: RoleUser, Content: "Weather in Pune?"},
		{Role: RoleAssistant, ToolCalls: resp.ToolCalls, Reasoning: resp.ReasoningBlocks},
		{Role: RoleTool, ToolCallID: resp.ToolCalls[0].ID, Content: `{"temp":30}`},
	}})
	if err != nil {
		t.Fatal(err)
	}
	contents := stub.body[1]["contents"].([]any)
	model := contents[1].(map[string]any)["parts"].([]any)
	call := model[len(model)-1].(map[string]any)
	if call["thoughtSignature"] != "ts2" || call["functionCall"] == nil {
		t.Fatalf("model turn = %v", model)
	}
	fr := contents[2].(map[string]any)["parts"].([]any)[0].(map[string]any)["functionResponse"].(map[string]any)
	if fr["name"] != "get_weather" || fr["response"].(map[string]any)["temp"].(float64) != 30 {
		t.Fatalf("functionResponse = %v", fr)
	}
}

func TestGeminiStreamsTextAndToolCalls(t *testing.T) {
	stub := &geminiStub{reply: func(string) string {
		return "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"Hel\"}]}}]}\n\n" +
			"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"lo\"},{\"functionCall\":{\"name\":\"f\",\"args\":{}}}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":3,\"candidatesTokenCount\":2,\"totalTokenCount\":5}}\n\n"
	}}
	srv := stub.server(t)
	defer srv.Close()
	p := &geminiProvider{apiKey: "k", model: "gemini-2.0-flash", baseURL: srv.URL}
	st, err := p.Stream(context.Background(), &GenerateRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := st.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "Hello" || len(resp.ToolCalls) != 1 || resp.Usage == nil || resp.Usage.TotalTokens != 5 {
		t.Fatalf("stream = %+v", resp)
	}
	if !strings.Contains(stub.paths[0], ":streamGenerateContent") {
		t.Fatalf("path = %s", stub.paths[0])
	}
}

func TestAgentOnGeminiCallsTools(t *testing.T) {
	calls := 0
	stub := &geminiStub{reply: func(string) string {
		calls++
		if calls == 1 {
			return `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"echo","args":{"q":"hi"}}}]}}]}`
		}
		return `{"candidates":[{"content":{"parts":[{"text":"echoed"}]},"finishReason":"STOP"}]}`
	}}
	srv := stub.server(t)
	defer srv.Close()
	client := &Client{provider: &geminiProvider{apiKey: "k", model: "gemini-2.0-flash", baseURL: srv.URL}, config: &Config{Provider: "gemini", MaxTokens: 100}}
	resp, err := NewAgent("x").WithClient(client).WithToolObjects(echoTool(t)).Prompt(context.Background(), "echo hi")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "echoed" || calls != 2 {
		t.Fatalf("resp=%q calls=%d", resp.Text, calls)
	}
	fr := stub.body[1]["contents"].([]any)[2].(map[string]any)["parts"].([]any)[0].(map[string]any)["functionResponse"].(map[string]any)
	if fr["name"] != "echo" || fr["response"].(map[string]any)["result"] != "echo:hi" {
		t.Fatalf("tool result sent as %v", fr)
	}
}
