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

// A streamed answer's token counts are asked for and read, whether the
// server sends them once at the end (OpenAI) or a running count on every
// chunk with the total last (some compatible servers).
func TestStreamReportsUsage(t *testing.T) {
	for name, body := range map[string]string{
		"once at the end": `data: {"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"hi"}}]}

data: {"id":"1","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":73,"completion_tokens":80,"total_tokens":153,"prompt_tokens_details":{"cached_tokens":64},"completion_tokens_details":{"reasoning_tokens":50}}}

data: [DONE]

`,
		"running, then the total": `data: {"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"h"}}],"usage":{"prompt_tokens":73,"completion_tokens":0,"total_tokens":73}}

data: {"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"i"}}],"usage":{"prompt_tokens":0,"completion_tokens":1,"total_tokens":1}}

data: {"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":73,"completion_tokens":80,"total_tokens":153}}

data: [DONE]

`,
	} {
		t.Run(name, func(t *testing.T) {
			var asked map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(b, &asked)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, body)
			}))
			defer srv.Close()
			p, err := newOpenAIProvider(&Config{OpenAIKey: "k", OpenAIBaseURL: srv.URL, Model: "m"})
			if err != nil {
				t.Fatal(err)
			}
			sr, err := p.Stream(context.Background(), &GenerateRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}, MaxTokens: 16})
			if err != nil {
				t.Fatal(err)
			}
			var text strings.Builder
			var usage *Usage
			for c := range sr.Chunks {
				text.WriteString(c.Text)
				if c.Usage != nil {
					usage = c.Usage
				}
			}
			if e := <-sr.Err; e != nil {
				t.Fatal(e)
			}
			if text.String() != "hi" || usage == nil || usage.PromptTokens != 73 || usage.CompletionTokens != 80 || usage.TotalTokens != 153 {
				t.Fatalf("text %q usage %+v", text.String(), usage)
			}
			if so, _ := asked["stream_options"].(map[string]any); so["include_usage"] != true {
				t.Errorf("the counts were not asked for: %v", asked["stream_options"])
			}
		})
	}
}

// OpenAI's thinking models refuse max_tokens and a temperature of the
// caller's; the others, and other servers' models, keep them.
func TestReasoningModelsTakeCompletionTokens(t *testing.T) {
	req := &GenerateRequest{Temperature: 0.5}
	for model, thinking := range map[string]bool{
		"gpt-5": true, "gpt-5-mini": true, "openai/gpt-5.1": true, "o3": true, "o4-mini": true,
		"gpt-4o-mini": false, "gpt-4.1": false, "agnes-3-flash": false, "@cf/openai/gpt-oss-120b": false, "deepseek-chat": false,
	} {
		r := openAIChatRequest(req, model, nil, 4000, false)
		if thinking && (r.MaxCompletionTokens != 4000 || r.MaxTokens != 0 || r.Temperature != 0) {
			t.Errorf("%s: want max_completion_tokens only, got max_tokens=%d max_completion_tokens=%d temperature=%v", model, r.MaxTokens, r.MaxCompletionTokens, r.Temperature)
		}
		if !thinking && (r.MaxTokens != 4000 || r.MaxCompletionTokens != 0 || r.Temperature != 0.5) {
			t.Errorf("%s: want max_tokens and the temperature kept, got max_tokens=%d max_completion_tokens=%d temperature=%v", model, r.MaxTokens, r.MaxCompletionTokens, r.Temperature)
		}
		if r.StreamOptions != nil {
			t.Errorf("%s: stream options on a plain request", model)
		}
	}
}
