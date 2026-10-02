package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// defaultCohereModel is used when no model is configured.
const defaultCohereModel = "command-r-plus"

var cohereHTTPClient = &http.Client{Timeout: 120 * time.Second}

// cohereBaseURL is the v2 Chat endpoint. It is a package var so tests can point
// the provider at a local server.
var cohereBaseURL = "https://api.cohere.com/v2/chat"

func newCohereProvider(cfg *Config) (Provider, error) {
	if cfg.CohereKey == "" {
		return nil, fmt.Errorf("ai: COHERE_API_KEY is required for Cohere provider")
	}
	model := cfg.Model
	if model == "" {
		model = defaultCohereModel
	}
	return &cohereProvider{apiKey: cfg.CohereKey, model: model}, nil
}

type cohereProvider struct {
	apiKey string
	model  string
}

func (p *cohereProvider) Name() string { return "cohere" }

// ── Wire types (v2 Chat API) ─────────────────────────────────────

type cohereRequest struct {
	Model       string          `json:"model"`
	Messages    []cohereMessage `json:"messages"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature float32         `json:"temperature,omitempty"`
	Stop        []string        `json:"stop_sequences,omitempty"`
	Tools       []cohereTool    `json:"tools,omitempty"`
	ToolChoice  string          `json:"tool_choice,omitempty"` // REQUIRED, NONE
	Stream      bool            `json:"stream,omitempty"`
}

type cohereMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCalls  []cohereToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type cohereToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // "function"
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"` // a JSON document, as a string
	} `json:"function"`
}

// cohereArgs normalises tool-call arguments, which v2 sends as a string
// holding JSON (older responses sent the object).
func cohereArgs(raw json.RawMessage) json.RawMessage {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		raw = json.RawMessage(s)
	}
	if len(strings.TrimSpace(string(raw))) == 0 || !json.Valid(raw) {
		return json.RawMessage("{}")
	}
	return raw
}

type cohereTool struct {
	Type     string             `json:"type"` // "function"
	Function cohereToolFunction `json:"function"`
}

type cohereToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type cohereResponse struct {
	FinishReason string `json:"finish_reason"`
	Message      struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		ToolCalls []struct {
			ID       string `json:"id"`
			Function struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls"`
	} `json:"message"`
	Usage cohereUsage `json:"usage"`
}

type cohereUsage struct {
	Tokens struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"tokens"`
}

// ── Generate ─────────────────────────────────────────────────────

func (p *cohereProvider) Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
	body := p.buildRequest(req, false)
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", cohereBaseURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	p.setHeaders(httpReq)

	resp, err := cohereHTTPClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, &APIError{Provider: "cohere", StatusCode: resp.StatusCode, Body: string(respBody), RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"))}
	}

	var cr cohereResponse
	if err := json.Unmarshal(respBody, &cr); err != nil {
		return nil, fmt.Errorf("cohere: failed to parse response: %w", err)
	}

	var text strings.Builder
	for _, block := range cr.Message.Content {
		if block.Type == "text" {
			text.WriteString(block.Text)
		}
	}

	var toolCalls []ToolCall
	for _, tc := range cr.Message.ToolCalls {
		toolCalls = append(toolCalls, ToolCall{
			ID:   tc.ID,
			Name: tc.Function.Name,
			Args: cohereArgs(tc.Function.Arguments),
		})
	}

	return &GenerateResponse{
		Text:         text.String(),
		ToolCalls:    toolCalls,
		Model:        body.Model,
		FinishReason: cr.FinishReason,
		Usage: &Usage{
			PromptTokens:     cr.Usage.Tokens.InputTokens,
			CompletionTokens: cr.Usage.Tokens.OutputTokens,
			TotalTokens:      cr.Usage.Tokens.InputTokens + cr.Usage.Tokens.OutputTokens,
		},
	}, nil
}

// ── Stream ───────────────────────────────────────────────────────

// cohereStreamEvent is a superset of the v2 SSE event shapes we consume; the
// incremental text lives at delta.message.content.text, and the terminating
// message-end event carries finish_reason + usage under delta.
type cohereStreamEvent struct {
	Type  string `json:"type"`
	Delta struct {
		Message struct {
			Content struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
		FinishReason string      `json:"finish_reason"`
		Usage        cohereUsage `json:"usage"`
	} `json:"delta"`
}

func (p *cohereProvider) Stream(ctx context.Context, req *GenerateRequest) (*StreamResponse, error) {
	body := p.buildRequest(req, true)
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", cohereBaseURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	p.setHeaders(httpReq)

	resp, err := cohereHTTPClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		defer resp.Body.Close()
		return nil, newAPIError("cohere", resp)
	}

	chunks := make(chan StreamChunk, 32)
	errCh := make(chan error, 1)

	go func() {
		defer resp.Body.Close()
		defer close(chunks)
		defer close(errCh)

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "" || data == "[DONE]" {
				continue
			}

			var ev cohereStreamEvent
			if err := json.Unmarshal([]byte(data), &ev); err != nil {
				errCh <- fmt.Errorf("cohere: SSE parse error: %w", err)
				return
			}

			switch ev.Type {
			case "content-delta":
				if ev.Delta.Message.Content.Text != "" {
					select {
					case chunks <- StreamChunk{Text: ev.Delta.Message.Content.Text}:
					case <-ctx.Done():
						errCh <- ctx.Err()
						return
					}
				}
			case "message-end":
				in := ev.Delta.Usage.Tokens.InputTokens
				out := ev.Delta.Usage.Tokens.OutputTokens
				select {
				case chunks <- StreamChunk{
					Usage: &Usage{
						PromptTokens:     in,
						CompletionTokens: out,
						TotalTokens:      in + out,
					},
					Done: true,
				}:
				case <-ctx.Done():
					errCh <- ctx.Err()
					return
				}
			}
		}

		if err := scanner.Err(); err != nil {
			errCh <- err
		}
	}()

	return &StreamResponse{Chunks: chunks, Err: errCh}, nil
}

// ── Shared request building ──────────────────────────────────────

func (p *cohereProvider) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
}

func (p *cohereProvider) buildRequest(req *GenerateRequest, stream bool) *cohereRequest {
	model := req.Model
	if model == "" {
		model = p.model
	}
	cr := &cohereRequest{
		Model:       model,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Stop:        req.Stop,
		Stream:      stream,
	}

	if req.System != "" {
		cr.Messages = append(cr.Messages, cohereMessage{Role: RoleSystem, Content: req.System})
	}
	for _, msg := range req.Messages {
		role := msg.Role
		if role == "" {
			role = RoleUser
		}
		cm := cohereMessage{Role: role, Content: contentWithDocuments(msg)}
		for _, tc := range msg.ToolCalls {
			var c cohereToolCall
			c.ID, c.Type = tc.ID, "function"
			c.Function.Name, c.Function.Arguments = tc.Name, string(cohereArgs(tc.Args))
			cm.ToolCalls = append(cm.ToolCalls, c)
		}
		if role == RoleTool {
			cm.ToolCallID = msg.ToolCallID
		}
		cr.Messages = append(cr.Messages, cm)
	}

	for _, tool := range req.Tools {
		schema := tool.Parameters
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object"}`)
		}
		cr.Tools = append(cr.Tools, cohereTool{
			Type: "function",
			Function: cohereToolFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  schema,
			},
		})
	}

	switch req.ToolChoice {
	case ToolChoiceNone:
		if len(cr.Tools) > 0 {
			cr.ToolChoice = "NONE"
		}
	case "", ToolChoiceAuto:
	default: // required, or a named tool: Cohere can only require some tool
		if len(cr.Tools) > 0 {
			cr.ToolChoice = "REQUIRED"
		}
	}
	return cr
}
