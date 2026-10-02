package ai

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnthropicCacheBreakpoints(t *testing.T) {
	p := &anthropicProvider{model: "claude-x", maxTokens: 100}
	req := &GenerateRequest{
		System: "You are a support agent.",
		Tools: []ToolSpec{
			{Name: "a", Parameters: json.RawMessage(`{"type":"object"}`)},
			{Name: "b", Parameters: json.RawMessage(`{"type":"object"}`)},
		},
		Messages: []Message{{Role: RoleUser, Content: "first"}, {Role: RoleAssistant, Content: "ok"}, {Role: RoleUser, Content: "second"}},
		Cache:    true,
	}
	raw, _ := json.Marshal(p.buildRequest(req, false))
	var got map[string]any
	_ = json.Unmarshal(raw, &got)

	sys, ok := got["system"].([]any)
	if !ok || sys[0].(map[string]any)["cache_control"] == nil {
		t.Fatalf("system is not a cached block: %s", raw)
	}
	tools := got["tools"].([]any)
	if tools[0].(map[string]any)["cache_control"] != nil || tools[1].(map[string]any)["cache_control"] == nil {
		t.Fatalf("only the last tool should carry the breakpoint: %s", raw)
	}
	msgs := got["messages"].([]any)
	last := msgs[len(msgs)-1].(map[string]any)["content"].([]any)
	if last[len(last)-1].(map[string]any)["cache_control"] == nil {
		t.Fatalf("last message block should carry the breakpoint: %s", raw)
	}
	if n := strings.Count(string(raw), "cache_control"); n != 3 {
		t.Fatalf("%d breakpoints, want 3 (Anthropic allows 4)", n)
	}

	req.Cache = false
	raw, _ = json.Marshal(p.buildRequest(req, false))
	if strings.Contains(string(raw), "cache_control") || !strings.Contains(string(raw), `"system":"You are a support agent."`) {
		t.Fatalf("without Cache the request must be unchanged: %s", raw)
	}
}

func TestAnthropicReportsCachedTokens(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"hi"}],"model":"claude-x","stop_reason":"end_turn",
		  "usage":{"input_tokens":10,"output_tokens":5,"cache_creation_input_tokens":2000,"cache_read_input_tokens":0}}`))
	}))
	defer srv.Close()
	p, _ := newAnthropicProvider(&Config{AnthropicKey: "k", AnthropicAPIURL: srv.URL})
	c := &Client{provider: p, config: &Config{Provider: "anthropic", Model: "claude-x", MaxTokens: 50, PromptCache: true}}
	resp, err := c.Generate(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	u := resp.Usage
	if u.PromptTokens != 2010 || u.CacheWriteTokens != 2000 || u.TotalTokens != 2015 {
		t.Fatalf("usage = %+v", u)
	}
	if !strings.Contains(body, "cache_control") {
		t.Fatal("Config.PromptCache should turn caching on for every request")
	}
}

func TestCachedTokensArePricedAtTheirOwnRate(t *testing.T) {
	p := ModelPricing{PromptPer1K: 3, CompletionPer1K: 15, CacheReadPer1K: 0.3, CacheWritePer1K: 3.75}
	u := &Usage{PromptTokens: 3000, CacheReadTokens: 2000, CacheWriteTokens: 0}
	if got := p.promptCost(u); math.Abs(got-(1*3+2*0.3)) > 1e-9 {
		t.Fatalf("promptCost = %v", got)
	}
	// Defaults: reads at half price, writes at full price.
	d := ModelPricing{PromptPer1K: 2}
	if got := d.promptCost(&Usage{PromptTokens: 2000, CacheReadTokens: 1000}); math.Abs(got-(2+1)) > 1e-9 {
		t.Fatalf("default promptCost = %v", got)
	}
	if c := DefaultPricing["claude-sonnet-4-20250514"]; c.CacheReadPer1K != 0.0003 || c.CacheWritePer1K != 0.00375 {
		t.Fatalf("anthropic cache prices = %+v", c)
	}
}

func TestAgentPromptCacheReachesEveryStep(t *testing.T) {
	fake := NewFake(FakeToolCall("echo", `{"q":"x"}`), FakeText("done"))
	_, err := NewAgent("x").WithClient(fake.Client()).WithToolObjects(echoTool(t)).WithPromptCache().
		Prompt(context.Background(), "go")
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range fake.Requests() {
		if !r.Cache {
			t.Fatalf("step %d was sent without caching", i)
		}
	}
}
