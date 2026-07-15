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

func withCohereServer(t *testing.T, handler http.HandlerFunc, fn func(p *cohereProvider)) {
	t.Helper()
	srv := httptest.NewServer(handler)
	defer srv.Close()

	orig := cohereBaseURL
	cohereBaseURL = srv.URL
	defer func() { cohereBaseURL = orig }()

	fn(&cohereProvider{apiKey: "test-key", model: defaultCohereModel})
}

func TestCohereGenerate(t *testing.T) {
	var gotBody map[string]any
	var gotAuth string

	handler := func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"finish_reason": "COMPLETE",
			"message": {
				"role": "assistant",
				"content": [{"type":"text","text":"Bonjour "},{"type":"text","text":"monde"}]
			},
			"usage": {"tokens": {"input_tokens": 9, "output_tokens": 4}}
		}`)
	}

	withCohereServer(t, handler, func(p *cohereProvider) {
		resp, err := p.Generate(context.Background(), &GenerateRequest{
			System: "Tu es concis.",
			Messages: []Message{
				{Role: RoleUser, Content: "Salut"},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if resp.Text != "Bonjour monde" {
			t.Fatalf("text: got %q", resp.Text)
		}
		if resp.FinishReason != "COMPLETE" {
			t.Fatalf("finish: got %q", resp.FinishReason)
		}
		if resp.Usage == nil || resp.Usage.TotalTokens != 13 {
			t.Fatalf("usage: got %+v", resp.Usage)
		}
	})

	if gotAuth != "Bearer test-key" {
		t.Errorf("auth header: got %q", gotAuth)
	}
	// System is sent as the first message with role "system".
	msgs := gotBody["messages"].([]any)
	first := msgs[0].(map[string]any)
	if first["role"] != RoleSystem || first["content"] != "Tu es concis." {
		t.Errorf("system message: got %+v", first)
	}
	if len(msgs) != 2 {
		t.Errorf("messages: expected 2 (system+user), got %d", len(msgs))
	}
}

func TestCohereStream(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		sse := strings.Join([]string{
			`data: {"type":"message-start"}`,
			`data: {"type":"content-start","index":0}`,
			`data: {"type":"content-delta","index":0,"delta":{"message":{"content":{"text":"Bon"}}}}`,
			`data: {"type":"content-delta","index":0,"delta":{"message":{"content":{"text":"jour"}}}}`,
			`data: {"type":"content-end","index":0}`,
			`data: {"type":"message-end","delta":{"finish_reason":"COMPLETE","usage":{"tokens":{"input_tokens":9,"output_tokens":2}}}}`,
		}, "\n\n") + "\n\n"
		io.WriteString(w, sse)
	}

	withCohereServer(t, handler, func(p *cohereProvider) {
		sr, err := p.Stream(context.Background(), &GenerateRequest{
			Messages: []Message{{Role: RoleUser, Content: "Salut"}},
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

		if text.String() != "Bonjour" {
			t.Fatalf("streamed text: got %q", text.String())
		}
		if !done {
			t.Fatalf("stream never signaled Done")
		}
		if final == nil || final.PromptTokens != 9 || final.CompletionTokens != 2 || final.TotalTokens != 11 {
			t.Fatalf("final usage: got %+v", final)
		}
	})
}

func TestCohereMissingKey(t *testing.T) {
	if _, err := newCohereProvider(&Config{}); err == nil {
		t.Fatal("expected error when COHERE_API_KEY is unset")
	}
	p, err := newCohereProvider(&Config{CohereKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	if p.(*cohereProvider).model != defaultCohereModel {
		t.Fatalf("default model: got %q", p.(*cohereProvider).model)
	}
}

func TestMistralProvider(t *testing.T) {
	if _, err := newMistralProvider(&Config{}); err == nil {
		t.Fatal("expected error when MISTRAL_API_KEY is unset")
	}
	p, err := newMistralProvider(&Config{MistralKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	if p.model != "mistral-large-latest" {
		t.Fatalf("default model: got %q", p.model)
	}
	// Custom model is respected.
	p2, _ := newMistralProvider(&Config{MistralKey: "k", Model: "mistral-small-latest"})
	if p2.model != "mistral-small-latest" {
		t.Fatalf("custom model: got %q", p2.model)
	}
}
