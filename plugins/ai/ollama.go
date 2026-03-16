package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ollamaProvider implements Provider using Ollama's local HTTP API.
type ollamaProvider struct {
	baseURL string
	model   string
	client  *http.Client
}

func (p *ollamaProvider) Name() string { return "ollama" }

func newOllamaProvider(cfg *Config) (*ollamaProvider, error) {
	model := cfg.Model
	if model == "" {
		model = "llama3.2"
	}
	return &ollamaProvider{
		baseURL: strings.TrimSuffix(cfg.OllamaHost, "/"),
		model:   model,
		client:  &http.Client{},
	}, nil
}

type ollamaChatReq struct {
	Model    string      `json:"model"`
	Messages []ollamaMsg `json:"messages"`
	Stream   bool        `json:"stream"`
}

type ollamaMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaChatResp struct {
	Message         ollamaMsg `json:"message"`
	Done            bool      `json:"done"`
	EvalCount       int       `json:"eval_count,omitempty"`
	PromptEvalCount int       `json:"prompt_eval_count,omitempty"`
}

func (p *ollamaProvider) Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
	msgs := p.toOllamaMessages(req)
	model := req.Model
	if model == "" {
		model = p.model
	}

	body := ollamaChatReq{Model: model, Messages: msgs, Stream: false}
	jsonBody, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/api/chat", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("ai: ollama: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ai: ollama: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ai: ollama: %s: %s", resp.Status, string(b))
	}

	var chatResp ollamaChatResp
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("ai: ollama: %w", err)
	}

	usage := &Usage{
		PromptTokens:     chatResp.PromptEvalCount,
		CompletionTokens: chatResp.EvalCount,
		TotalTokens:      chatResp.PromptEvalCount + chatResp.EvalCount,
	}

	return &GenerateResponse{
		Text:  chatResp.Message.Content,
		Usage: usage,
		Model: model,
	}, nil
}

func (p *ollamaProvider) Stream(ctx context.Context, req *GenerateRequest) (*StreamResponse, error) {
	chunks := make(chan StreamChunk, 32)
	errCh := make(chan error, 1)

	go func() {
		defer close(chunks)
		defer close(errCh)

		msgs := p.toOllamaMessages(req)
		model := req.Model
		if model == "" {
			model = p.model
		}

		body := ollamaChatReq{Model: model, Messages: msgs, Stream: true}
		jsonBody, _ := json.Marshal(body)
		httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/api/chat", bytes.NewReader(jsonBody))
		if err != nil {
			errCh <- fmt.Errorf("ai: ollama: %w", err)
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := p.client.Do(httpReq)
		if err != nil {
			errCh <- fmt.Errorf("ai: ollama: %w", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			errCh <- fmt.Errorf("ai: ollama: %s: %s", resp.Status, string(b))
			return
		}

		dec := json.NewDecoder(resp.Body)
		for {
			var chunk ollamaChatResp
			if err := dec.Decode(&chunk); err == io.EOF {
				return
			} else if err != nil {
				errCh <- fmt.Errorf("ai: ollama stream: %w", err)
				return
			}
			if chunk.Message.Content != "" {
				chunks <- StreamChunk{Text: chunk.Message.Content}
			}
			if chunk.Done {
				return
			}
		}
	}()

	return &StreamResponse{Chunks: chunks, Err: errCh}, nil
}

func (p *ollamaProvider) toOllamaMessages(req *GenerateRequest) []ollamaMsg {
	var msgs []ollamaMsg
	if req.System != "" {
		msgs = append(msgs, ollamaMsg{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		role := m.Role
		if role == "" {
			role = "user"
		}
		msgs = append(msgs, ollamaMsg{Role: role, Content: m.Content})
	}
	return msgs
}
