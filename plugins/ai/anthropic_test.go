package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// withAnthropicServer points the provider at a local test server for the
// duration of fn, restoring the real endpoint afterward.
func withAnthropicServer(t *testing.T, handler http.HandlerFunc, fn func(p *anthropicProvider)) {
	t.Helper()
	srv := httptest.NewServer(handler)
	defer srv.Close()

	orig := anthropicBaseURL
	anthropicBaseURL = srv.URL
	defer func() { anthropicBaseURL = orig }()

	fn(&anthropicProvider{apiKey: "test-key", model: defaultAnthropicModel, endpoint: srv.URL})
}

func TestAnthropicGenerate(t *testing.T) {
	var gotBody map[string]any
	var gotHeaders http.Header

	handler := func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"model": "claude-opus-4-8",
			"stop_reason": "end_turn",
			"content": [
				{"type":"text","text":"Hello "},
				{"type":"text","text":"world"},
				{"type":"tool_use","id":"tu_1","name":"get_weather","input":{"city":"NYC"}}
			],
			"usage": {"input_tokens": 12, "output_tokens": 5}
		}`)
	}

	withAnthropicServer(t, handler, func(p *anthropicProvider) {
		resp, err := p.Generate(context.Background(), &GenerateRequest{
			System:      "You are terse.",
			Temperature: 0.7, // must NOT be forwarded (400 on opus-4-8)
			Messages: []Message{
				{Role: RoleSystem, Content: "Extra system."},
				{Role: RoleUser, Content: "Hi"},
				{Role: RoleAssistant, Content: "Hey"},
			},
			Tools: []ToolSpec{{Name: "get_weather", Description: "Weather"}},
		})
		if err != nil {
			t.Fatal(err)
		}

		if resp.Text != "Hello world" {
			t.Fatalf("text: got %q", resp.Text)
		}
		if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "get_weather" {
			t.Fatalf("tool calls: got %+v", resp.ToolCalls)
		}
		if resp.FinishReason != "end_turn" {
			t.Fatalf("finish: got %q", resp.FinishReason)
		}
		if resp.Usage == nil || resp.Usage.TotalTokens != 17 {
			t.Fatalf("usage: got %+v", resp.Usage)
		}
	})

	// Required headers.
	if gotHeaders.Get("x-api-key") != "test-key" {
		t.Errorf("missing x-api-key header")
	}
	if gotHeaders.Get("anthropic-version") != anthropicVersion {
		t.Errorf("anthropic-version: got %q", gotHeaders.Get("anthropic-version"))
	}

	// Wire-format correctness.
	if _, hasTemp := gotBody["temperature"]; hasTemp {
		t.Errorf("temperature must not be sent (rejected with 400 on opus-4-8)")
	}
	if gotBody["max_tokens"].(float64) != float64(defaultAnthropicMaxTokens) {
		t.Errorf("max_tokens default: got %v", gotBody["max_tokens"])
	}
	// System hoisting: top-level system merges request System + system-role msg.
	sys, _ := gotBody["system"].(string)
	if !strings.Contains(sys, "You are terse.") || !strings.Contains(sys, "Extra system.") {
		t.Errorf("system hoisting failed: got %q", sys)
	}
	// Only user/assistant turns remain in messages (system hoisted out).
	msgs := gotBody["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages: expected 2 (user+assistant), got %d", len(msgs))
	}
	// Tool schema defaulted when Parameters empty.
	tools := gotBody["tools"].([]any)
	tool0 := tools[0].(map[string]any)
	if tool0["input_schema"] == nil {
		t.Errorf("tool input_schema missing")
	}
}

func TestAnthropicStream(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		sse := strings.Join([]string{
			`data: {"type":"message_start","message":{"usage":{"input_tokens":8,"output_tokens":0}}}`,
			`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"Hel"}}`,
			`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"lo"}}`,
			`data: {"type":"message_delta","usage":{"output_tokens":3}}`,
			`data: {"type":"message_stop"}`,
		}, "\n\n") + "\n\n"
		io.WriteString(w, sse)
	}

	withAnthropicServer(t, handler, func(p *anthropicProvider) {
		sr, err := p.Stream(context.Background(), &GenerateRequest{
			Messages: []Message{{Role: RoleUser, Content: "Hi"}},
		})
		if err != nil {
			t.Fatal(err)
		}

		var text strings.Builder
		var final *Usage
		done := false
		for chunk := range sr.Chunks {
			text.WriteString(chunk.Text)
			if chunk.Done {
				done = true
				final = chunk.Usage
			}
		}
		if err := <-sr.Err; err != nil {
			t.Fatalf("stream err: %v", err)
		}

		if text.String() != "Hello" {
			t.Fatalf("streamed text: got %q", text.String())
		}
		if !done {
			t.Fatalf("stream never signaled Done")
		}
		if final == nil || final.PromptTokens != 8 || final.CompletionTokens != 3 || final.TotalTokens != 11 {
			t.Fatalf("final usage: got %+v", final)
		}
	})
}

func TestAnthropicMissingKey(t *testing.T) {
	if _, err := newAnthropicProvider(&Config{}); err == nil {
		t.Fatal("expected error when ANTHROPIC_API_KEY is unset")
	}
	p, err := newAnthropicProvider(&Config{AnthropicKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	if p.(*anthropicProvider).model != defaultAnthropicModel {
		t.Fatalf("default model: got %q", p.(*anthropicProvider).model)
	}
	if p.(*anthropicProvider).endpoint != "https://api.anthropic.com/v1/messages" {
		t.Fatalf("default endpoint: got %q", p.(*anthropicProvider).endpoint)
	}
}

func TestAnthropicCustomURL(t *testing.T) {
	tests := []struct {
		input   string
		wantURL string
	}{
		{"https://custom-proxy.example.com/v1", "https://custom-proxy.example.com/v1/messages"},
		{"https://custom-proxy.example.com/v1/", "https://custom-proxy.example.com/v1/messages"},
		{"https://custom-proxy.example.com/v1/messages", "https://custom-proxy.example.com/v1/messages"},
		{"https://api.anthropic.com/v1/messages", "https://api.anthropic.com/v1/messages"},
		{"https://api.anthropic.com/v1", "https://api.anthropic.com/v1/messages"},
		{"", "https://api.anthropic.com/v1/messages"},
	}

	for _, tt := range tests {
		gotURL := normalizeAnthropicURL(tt.input)
		if gotURL != tt.wantURL {
			t.Errorf("normalizeAnthropicURL(%q) URL = %q, want %q", tt.input, gotURL, tt.wantURL)
		}
	}

	p, err := newAnthropicProvider(&Config{
		AnthropicKey:    "k",
		AnthropicAPIURL: "https://custom-proxy.example.com/v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	ap := p.(*anthropicProvider)
	if ap.endpoint != "https://custom-proxy.example.com/v1/messages" {
		t.Fatalf("custom endpoint: got %q", ap.endpoint)
	}
}
