package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// anthropicVersion is the required Anthropic API version header value.
const anthropicVersion = "2023-06-01"

// defaultAnthropicModel is used when no model is configured.
const defaultAnthropicModel = "claude-sonnet-5"

// defaultAnthropicMaxTokens is the fallback when a request sets no MaxTokens.
// Anthropic Messages API requires max_tokens on every request.
const defaultAnthropicMaxTokens = 8192

// anthropicClients are the provider's two HTTP clients. A whole answer is
// bounded by AI_TIMEOUT (it used to be a fixed 120s, which cut long
// answers off and ignored the setting). A stream has no overall limit,
// because http.Client.Timeout also covers reading the body and would kill
// a long stream mid-answer; it must answer with headers within AI_TIMEOUT,
// and the caller's context ends it.
func anthropicClients(cfg *Config) (whole, stream *http.Client) {
	secs := 600
	if cfg != nil && cfg.Timeout > 0 {
		secs = cfg.Timeout
	}
	limit := time.Duration(secs) * time.Second
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.ResponseHeaderTimeout = limit
	return &http.Client{Timeout: limit, Transport: tr}, &http.Client{Transport: tr}
}

// anthropicBaseURL is the default Messages API endpoint.
var anthropicBaseURL = "https://api.anthropic.com/v1/messages"

// normalizeAnthropicURL ensures the endpoint points to the /messages path.
func normalizeAnthropicURL(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return anthropicBaseURL
	}
	u = strings.TrimRight(u, "/")
	if strings.HasSuffix(u, "/messages") {
		return u
	}
	if strings.HasSuffix(u, "/v1") {
		return u + "/messages"
	}
	return u + "/v1/messages"
}

func newAnthropicProvider(cfg *Config) (Provider, error) {
	apiKey := cfg.AnthropicKey
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("ai: ANTHROPIC_API_KEY is required for Anthropic provider")
	}

	model := cfg.Model
	if model == "" {
		model = os.Getenv("AI_MODEL")
	}
	if model == "" {
		model = defaultAnthropicModel
	}

	apiURL := cfg.AnthropicBaseURL
	if apiURL == "" {
		apiURL = cfg.AnthropicAPIURL
	}
	if apiURL == "" {
		apiURL = os.Getenv("ANTHROPIC_BASE_URL")
	}
	if apiURL == "" {
		apiURL = os.Getenv("ANTHROPIC_API_URL")
	}

	maxTokens := cfg.MaxTokens
	if v := os.Getenv("AI_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxTokens = n
		}
	}
	if maxTokens <= 0 {
		maxTokens = defaultAnthropicMaxTokens
	}

	whole, stream := anthropicClients(cfg)
	return &anthropicProvider{
		apiKey:     apiKey,
		model:      model,
		endpoint:   normalizeAnthropicURL(apiURL),
		maxTokens:  maxTokens,
		client:     whole,
		streamHTTP: stream,
	}, nil
}

type anthropicProvider struct {
	apiKey     string
	model      string
	endpoint   string
	maxTokens  int
	client     *http.Client
	streamHTTP *http.Client
}

func (p *anthropicProvider) httpClient(stream bool) *http.Client {
	c := p.client
	if stream {
		c = p.streamHTTP
	}
	if c == nil {
		whole, s := anthropicClients(nil)
		if c = whole; stream {
			c = s
		}
	}
	return c
}

func (p *anthropicProvider) Name() string { return "anthropic" }

func (p *anthropicProvider) effectiveMaxTokens(reqMax int) int {
	if reqMax > 0 {
		return reqMax
	}
	if p.maxTokens > 0 {
		return p.maxTokens
	}
	return defaultAnthropicMaxTokens
}

// ── Wire types (Anthropic Messages API) ──────────────────────────

type anthropicRequest struct {
	Model      string               `json:"model"`
	MaxTokens  int                  `json:"max_tokens"`
	System     any                  `json:"system,omitempty"` // string, or blocks when caching
	Messages   []anthropicMessage   `json:"messages"`
	Tools      []anthropicTool      `json:"tools,omitempty"`
	Stop       []string             `json:"stop_sequences,omitempty"`
	Stream     bool                 `json:"stream,omitempty"`
	Thinking   *anthropicThinking   `json:"thinking,omitempty"`
	ToolChoice *anthropicToolChoice `json:"tool_choice,omitempty"`
	// NOTE: temperature/top_p/top_k are not forwarded to Opus/Sonnet
	// to avoid 400 rejection; behavior is steered via prompting.
}

type anthropicMessage struct {
	Role    string             `json:"role"`
	Content []anthropicContent `json:"content"`
}

type anthropicContent struct {
	Type   string                `json:"type"`
	Text   string                `json:"text,omitempty"`
	Source *anthropicImageSource `json:"source,omitempty"`
	Title  string                `json:"title,omitempty"` // document blocks
	// tool_use blocks (assistant turns)
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// tool_result blocks (user turns)
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
	// thinking / redacted_thinking blocks (assistant turns)
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
	Data      string `json:"data,omitempty"`

	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

type anthropicToolChoice struct {
	Type string `json:"type"` // auto, any, tool, none
	Name string `json:"name,omitempty"`
}

// anthropicThinking turns on extended thinking.
type anthropicThinking struct {
	Type         string `json:"type"` // "enabled"
	BudgetTokens int    `json:"budget_tokens"`
}

// anthropicCacheControl marks a prompt-cache breakpoint: everything up to
// and including the marked block is cached.
type anthropicCacheControl struct {
	Type string `json:"type"` // "ephemeral"
}

var anthropicEphemeral = &anthropicCacheControl{Type: "ephemeral"}

type anthropicSystemBlock struct {
	Type         string                 `json:"type"`
	Text         string                 `json:"text"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

type anthropicImageSource struct {
	Type      string `json:"type"`       // "base64", or "text" for a text document
	MediaType string `json:"media_type"` // e.g. image/png, application/pdf, text/plain
	Data      string `json:"data"`
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`

	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

// anthropicUsage is the usage block of a response or message_start event.
type anthropicUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// toUsage reports every input token in PromptTokens (Anthropic counts
// cached ones separately), so totals compare across providers.
func (u anthropicUsage) toUsage() *Usage {
	prompt := u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
	return &Usage{
		PromptTokens:     prompt,
		CompletionTokens: u.OutputTokens,
		TotalTokens:      prompt + u.OutputTokens,
		CacheReadTokens:  u.CacheReadInputTokens,
		CacheWriteTokens: u.CacheCreationInputTokens,
	}
}

type anthropicResponse struct {
	Content []struct {
		Type      string          `json:"type"`
		Text      string          `json:"text"`
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Input     json.RawMessage `json:"input"`
		Thinking  string          `json:"thinking"`
		Signature string          `json:"signature"`
		Data      string          `json:"data"`
	} `json:"content"`
	Model      string         `json:"model"`
	StopReason string         `json:"stop_reason"`
	Usage      anthropicUsage `json:"usage"`
}

// ── Generate ─────────────────────────────────────────────────────

func (p *anthropicProvider) Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
	body := p.buildRequest(req, false)
	respBody, err := p.do(ctx, body)
	if err != nil {
		return nil, err
	}

	var ar anthropicResponse
	if err := json.Unmarshal(respBody, &ar); err != nil {
		return nil, fmt.Errorf("anthropic: failed to parse response: %w", err)
	}

	var text, thinking strings.Builder
	var toolCalls []ToolCall
	var reasoning []ReasoningBlock
	for _, block := range ar.Content {
		switch block.Type {
		case "thinking":
			thinking.WriteString(block.Thinking)
			reasoning = append(reasoning, ReasoningBlock{Text: block.Thinking, Signature: block.Signature})
		case "redacted_thinking":
			reasoning = append(reasoning, ReasoningBlock{Redacted: block.Data})
		case "text":
			text.WriteString(block.Text)
		case "tool_use":
			toolCalls = append(toolCalls, ToolCall{
				ID:   block.ID,
				Name: block.Name,
				Args: block.Input,
			})
		}
	}

	return &GenerateResponse{
		Text:            text.String(),
		ToolCalls:       toolCalls,
		Reasoning:       thinking.String(),
		ReasoningBlocks: reasoning,
		Model:           ar.Model,
		FinishReason:    ar.StopReason,
		Usage:           ar.Usage.toUsage(),
	}, nil
}

func (p *anthropicProvider) do(ctx context.Context, body *anthropicRequest) ([]byte, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	endpoint := p.endpoint
	if endpoint == "" {
		endpoint = anthropicBaseURL
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	p.setHeaders(httpReq)

	resp, err := p.httpClient(false).Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, &APIError{Provider: "anthropic", StatusCode: resp.StatusCode, Body: string(respBody), RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"))}
	}
	return respBody, nil
}

// ── Stream ───────────────────────────────────────────────────────

type anthropicStreamEvent struct {
	Type         string `json:"type"`
	Index        int    `json:"index"`
	ContentBlock struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
		Data string `json:"data"` // redacted_thinking
	} `json:"content_block"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
		Thinking    string `json:"thinking"`
		Signature   string `json:"signature"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	Message struct {
		Usage anthropicUsage `json:"usage"`
	} `json:"message"`
	Usage struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// anthropicStreamBlock accumulates one content block of a stream.
type anthropicStreamBlock struct {
	kind, id, name, signature, redacted string
	buf                                 strings.Builder
}

// StreamsToolCalls implements ToolCallStreamer.
func (p *anthropicProvider) StreamsToolCalls() bool { return true }

func (p *anthropicProvider) Stream(ctx context.Context, req *GenerateRequest) (*StreamResponse, error) {
	body := p.buildRequest(req, true)
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	endpoint := p.endpoint
	if endpoint == "" {
		endpoint = anthropicBaseURL
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	p.setHeaders(httpReq)

	resp, err := p.httpClient(true).Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		defer resp.Body.Close()
		return nil, newAPIError("anthropic", resp)
	}

	chunks := make(chan StreamChunk, 32)
	errCh := make(chan error, 1)

	go func() {
		defer resp.Body.Close()
		defer close(chunks)
		defer close(errCh)

		usage := &Usage{}
		blocks := map[int]*anthropicStreamBlock{}
		send := func(c StreamChunk) bool {
			select {
			case chunks <- c:
				return true
			case <-ctx.Done():
				errCh <- ctx.Err()
				return false
			}
		}
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "" {
				continue
			}

			var ev anthropicStreamEvent
			if err := json.Unmarshal([]byte(data), &ev); err != nil {
				errCh <- fmt.Errorf("anthropic: SSE parse error: %w", err)
				return
			}

			switch ev.Type {
			case "message_start":
				usage = ev.Message.Usage.toUsage()
			case "content_block_start":
				blocks[ev.Index] = &anthropicStreamBlock{kind: ev.ContentBlock.Type, id: ev.ContentBlock.ID, name: ev.ContentBlock.Name, redacted: ev.ContentBlock.Data}
			case "content_block_delta":
				b := blocks[ev.Index]
				switch ev.Delta.Type {
				case "text_delta":
					if ev.Delta.Text != "" && !send(StreamChunk{Text: ev.Delta.Text}) {
						return
					}
				case "input_json_delta":
					if b != nil {
						b.buf.WriteString(ev.Delta.PartialJSON)
					}
				case "thinking_delta":
					if b != nil {
						b.buf.WriteString(ev.Delta.Thinking)
					}
				case "signature_delta":
					if b != nil {
						b.signature += ev.Delta.Signature
					}
				}
			case "content_block_stop":
				b := blocks[ev.Index]
				delete(blocks, ev.Index)
				if b == nil {
					break
				}
				var c StreamChunk
				switch b.kind {
				case "tool_use":
					args := json.RawMessage(b.buf.String())
					if len(strings.TrimSpace(string(args))) == 0 {
						args = json.RawMessage("{}")
					}
					c.ToolCalls = []ToolCall{{ID: b.id, Name: b.name, Args: args}}
				case "thinking":
					c.Reasoning = []ReasoningBlock{{Text: b.buf.String(), Signature: b.signature}}
				case "redacted_thinking":
					c.Reasoning = []ReasoningBlock{{Redacted: b.redacted}}
				default:
					break
				}
				if (len(c.ToolCalls) > 0 || len(c.Reasoning) > 0) && !send(c) {
					return
				}
			case "message_delta":
				usage.CompletionTokens = ev.Usage.OutputTokens
				usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
			case "message_stop":
				select {
				case chunks <- StreamChunk{Usage: usage, Done: true}:
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

func (p *anthropicProvider) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)
}

func (p *anthropicProvider) buildRequest(req *GenerateRequest, stream bool) *anthropicRequest {
	maxTokens := p.effectiveMaxTokens(req.MaxTokens)

	model := req.Model
	if model == "" {
		model = p.model
	}

	ar := &anthropicRequest{
		Model:     model,
		MaxTokens: maxTokens,
		Stop:      req.Stop,
		Stream:    stream,
	}

	for _, tool := range req.Tools {
		schema := tool.Parameters
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object"}`)
		}
		ar.Tools = append(ar.Tools, anthropicTool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: schema,
		})
	}

	// Hoist system prompt to top-level field per Messages API spec.
	var systemParts []string
	if req.System != "" {
		systemParts = append(systemParts, req.System)
	}
	for _, msg := range req.Messages {
		if msg.Role == RoleSystem {
			if msg.Content != "" {
				systemParts = append(systemParts, msg.Content)
			}
			continue
		}
		role := "user"
		if msg.Role == RoleAssistant {
			role = "assistant"
		}
		content := []anthropicContent{}
		if msg.Role == RoleTool {
			// Tool results are user-role tool_result blocks that must
			// reference the assistant's tool_use id.
			result := msg.Content
			if result == "" {
				result = "(no output)"
			}
			content = append(content, anthropicContent{
				Type:      "tool_result",
				ToolUseID: msg.ToolCallID,
				Content:   result,
				IsError:   strings.HasPrefix(result, "Error:"),
			})
		} else {
			// Thinking comes first in an assistant turn, exactly as returned.
			for _, rb := range msg.Reasoning {
				switch {
				case rb.Redacted != "":
					content = append(content, anthropicContent{Type: "redacted_thinking", Data: rb.Redacted})
				case rb.Signature != "":
					content = append(content, anthropicContent{Type: "thinking", Thinking: rb.Text, Signature: rb.Signature})
				}
			}
			if msg.Content != "" {
				content = append(content, anthropicContent{Type: "text", Text: msg.Content})
			}
			content = append(content, anthropicAttachmentBlocks(msg)...)
			for _, tc := range msg.ToolCalls {
				input := tc.Args
				if len(strings.TrimSpace(string(input))) == 0 {
					input = json.RawMessage(`{}`)
				}
				content = append(content, anthropicContent{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Name,
					Input: input,
				})
			}
		}
		if len(content) == 0 {
			continue
		}
		// The API requires strictly alternating roles: fold consecutive
		// same-role messages (e.g. several tool results) into one turn.
		if n := len(ar.Messages); n > 0 && ar.Messages[n-1].Role == role {
			ar.Messages[n-1].Content = append(ar.Messages[n-1].Content, content...)
			continue
		}
		ar.Messages = append(ar.Messages, anthropicMessage{Role: role, Content: content})
	}
	if len(systemParts) > 0 {
		ar.System = strings.Join(systemParts, "\n\n")
	}
	if req.Cache {
		applyAnthropicCache(ar)
	}
	if len(ar.Tools) > 0 {
		switch c := req.ToolChoice; {
		case c == "" || c == ToolChoiceAuto:
		case c == ToolChoiceNone:
			ar.ToolChoice = &anthropicToolChoice{Type: "none"}
		case req.Reasoning != nil:
			// With extended thinking the API allows only auto and none.
		case c == ToolChoiceRequired:
			ar.ToolChoice = &anthropicToolChoice{Type: "any"}
		default:
			ar.ToolChoice = &anthropicToolChoice{Type: "tool", Name: c}
		}
	}
	if req.Reasoning != nil {
		budget := req.Reasoning.budget()
		if budget < 1024 {
			budget = 1024 // the API's minimum
		}
		ar.Thinking = &anthropicThinking{Type: "enabled", BudgetTokens: budget}
		if ar.MaxTokens <= budget {
			ar.MaxTokens = budget + 4096 // max_tokens must leave room for the answer
		}
	}

	return ar
}

// applyAnthropicCache sets up to three cache breakpoints, in prefix order:
// the tools, the system prompt, and the last block of the conversation.
// Each request then reuses everything the previous one cached. Prompts
// shorter than the model's minimum are simply not cached.
func applyAnthropicCache(ar *anthropicRequest) {
	if n := len(ar.Tools); n > 0 {
		ar.Tools[n-1].CacheControl = anthropicEphemeral
	}
	if s, ok := ar.System.(string); ok && s != "" {
		ar.System = []anthropicSystemBlock{{Type: "text", Text: s, CacheControl: anthropicEphemeral}}
	}
	if n := len(ar.Messages); n > 0 {
		blocks := ar.Messages[n-1].Content
		if m := len(blocks); m > 0 {
			blocks[m-1].CacheControl = anthropicEphemeral
		}
	}
}

// anthropicAttachmentBlocks turns a message's images and files into
// content blocks: images as image blocks, PDFs and text as document blocks
// (Claude reads PDFs page by page, figures and tables included).
func anthropicAttachmentBlocks(msg Message) []anthropicContent {
	var blocks []anthropicContent
	for _, a := range loadAttachments(append(append([]string(nil), msg.Images...), msg.Files...)) {
		switch {
		case a.IsImage():
			blocks = append(blocks, anthropicContent{Type: "image", Source: &anthropicImageSource{Type: "base64", MediaType: a.MediaType, Data: a.Base64()}})
		case a.IsPDF():
			blocks = append(blocks, anthropicContent{Type: "document", Source: &anthropicImageSource{Type: "base64", MediaType: "application/pdf", Data: a.Base64()}, Title: a.Name})
		default:
			text, err := DocumentText(a)
			if err != nil {
				continue
			}
			blocks = append(blocks, anthropicContent{Type: "document", Source: &anthropicImageSource{Type: "text", MediaType: "text/plain", Data: text}, Title: a.Name})
		}
	}
	return blocks
}
