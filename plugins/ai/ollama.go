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
	Model    string          `json:"model"`
	Messages []ollamaMsg     `json:"messages"`
	Stream   bool            `json:"stream"`
	Tools    []ollamaTool    `json:"tools,omitempty"`
	Think    bool            `json:"think,omitempty"`
	Format   json.RawMessage `json:"format,omitempty"`
	Options  map[string]any  `json:"options,omitempty"`
}

type ollamaMsg struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	Images    []string         `json:"images,omitempty"` // base64, no data: prefix
	Thinking  string           `json:"thinking,omitempty"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
	ToolName  string           `json:"tool_name,omitempty"`
}

type ollamaTool struct {
	Type     string `json:"type"` // "function"
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		Parameters  json.RawMessage `json:"parameters,omitempty"`
	} `json:"function"`
}

type ollamaToolCall struct {
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

type ollamaChatResp struct {
	Message         ollamaMsg `json:"message"`
	Done            bool      `json:"done"`
	DoneReason      string    `json:"done_reason,omitempty"`
	EvalCount       int       `json:"eval_count,omitempty"`
	PromptEvalCount int       `json:"prompt_eval_count,omitempty"`
}

func (r *ollamaChatResp) usage() *Usage {
	return &Usage{PromptTokens: r.PromptEvalCount, CompletionTokens: r.EvalCount, TotalTokens: r.PromptEvalCount + r.EvalCount}
}

// toolCalls converts Ollama's calls, which carry no ids, numbering them.
func (m ollamaMsg) toolCalls(offset int) []ToolCall {
	var out []ToolCall
	for i, tc := range m.ToolCalls {
		args := tc.Function.Arguments
		if len(args) == 0 {
			args = json.RawMessage("{}")
		}
		out = append(out, ToolCall{ID: fmt.Sprintf("ollama_call_%d", offset+i+1), Name: tc.Function.Name, Args: args})
	}
	return out
}

func (p *ollamaProvider) modelFor(req *GenerateRequest) string {
	if req.Model != "" {
		return req.Model
	}
	return p.model
}

func (p *ollamaProvider) buildRequest(req *GenerateRequest, stream bool) ollamaChatReq {
	body := ollamaChatReq{Model: p.modelFor(req), Messages: p.toOllamaMessages(req), Stream: stream}
	for _, t := range req.Tools {
		if req.ToolChoice == ToolChoiceNone {
			break // Ollama has no tool_choice; offering no tools is the same
		}
		var ot ollamaTool
		ot.Type = "function"
		ot.Function.Name, ot.Function.Description, ot.Function.Parameters = t.Name, t.Description, t.Parameters
		body.Tools = append(body.Tools, ot)
	}
	body.Think = req.Reasoning != nil
	if len(req.Schema) > 0 {
		body.Format = req.Schema
	}
	opts := map[string]any{}
	if req.MaxTokens > 0 {
		opts["num_predict"] = req.MaxTokens
	}
	if req.Temperature > 0 {
		opts["temperature"] = req.Temperature
	}
	if len(req.Stop) > 0 {
		opts["stop"] = req.Stop
	}
	if len(opts) > 0 {
		body.Options = opts
	}
	return body
}

func (p *ollamaProvider) post(ctx context.Context, body ollamaChatReq) (*http.Response, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/api/chat", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("ai: ollama: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ai: ollama: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, newAPIError("ollama", resp)
	}
	return resp, nil
}

func (p *ollamaProvider) Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
	resp, err := p.post(ctx, p.buildRequest(req, false))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var chatResp ollamaChatResp
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("ai: ollama: %w", err)
	}
	out := &GenerateResponse{
		Text:         chatResp.Message.Content,
		ToolCalls:    chatResp.Message.toolCalls(0),
		Reasoning:    chatResp.Message.Thinking,
		Usage:        chatResp.usage(),
		Model:        p.modelFor(req),
		FinishReason: chatResp.DoneReason,
	}
	if len(out.ToolCalls) > 0 {
		out.FinishReason = "tool_calls"
	}
	return out, nil
}

func (p *ollamaProvider) Stream(ctx context.Context, req *GenerateRequest) (*StreamResponse, error) {
	resp, err := p.post(ctx, p.buildRequest(req, true))
	if err != nil {
		return nil, err
	}
	chunks := make(chan StreamChunk, 32)
	errCh := make(chan error, 1)
	go func() {
		defer resp.Body.Close()
		defer close(chunks)
		defer close(errCh)
		send := func(c StreamChunk) bool {
			select {
			case chunks <- c:
				return true
			case <-ctx.Done():
				errCh <- ctx.Err()
				return false
			}
		}
		calls := 0
		dec := json.NewDecoder(resp.Body)
		for {
			var chunk ollamaChatResp
			if err := dec.Decode(&chunk); err == io.EOF {
				return
			} else if err != nil {
				errCh <- fmt.Errorf("ai: ollama stream: %w", err)
				return
			}
			if chunk.Message.Content != "" && !send(StreamChunk{Text: chunk.Message.Content}) {
				return
			}
			if tcs := chunk.Message.toolCalls(calls); len(tcs) > 0 {
				calls += len(tcs)
				if !send(StreamChunk{ToolCalls: tcs}) {
					return
				}
			}
			if chunk.Done {
				send(StreamChunk{Usage: chunk.usage(), Done: true})
				return
			}
		}
	}()
	return &StreamResponse{Chunks: chunks, Err: errCh}, nil
}

// StreamsToolCalls implements ToolCallStreamer.
func (p *ollamaProvider) StreamsToolCalls() bool { return true }

func (p *ollamaProvider) toOllamaMessages(req *GenerateRequest) []ollamaMsg {
	var msgs []ollamaMsg
	if req.System != "" {
		msgs = append(msgs, ollamaMsg{Role: "system", Content: req.System})
	}
	names := map[string]string{}
	for _, m := range req.Messages {
		role := m.Role
		if role == "" {
			role = "user"
		}
		om := ollamaMsg{Role: role, Content: contentWithDocuments(m)}
		for _, ref := range imageRefs(m) {
			if a, err := LoadAttachment(context.Background(), ref); err == nil && a.IsImage() {
				om.Images = append(om.Images, a.Base64())
			}
		}
		for _, tc := range m.ToolCalls {
			names[tc.ID] = tc.Name
			var otc ollamaToolCall
			otc.Function.Name = tc.Name
			otc.Function.Arguments = tc.Args
			if len(strings.TrimSpace(string(otc.Function.Arguments))) == 0 {
				otc.Function.Arguments = json.RawMessage("{}")
			}
			om.ToolCalls = append(om.ToolCalls, otc)
		}
		if role == RoleTool {
			om.ToolName = names[m.ToolCallID]
		}
		msgs = append(msgs, om)
	}
	return msgs
}
