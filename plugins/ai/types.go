/*
|--------------------------------------------------------------------------
| AI SDK — Core Types
|--------------------------------------------------------------------------
|
| Canonical types shared across the AI subsystem. Every provider,
| agent, tool, and pipeline operates on these primitives.
|
*/

package ai

import (
	"context"
	"encoding/json"
	"time"
)

// ---------------------------------------------------------------------------
// Messages
// ---------------------------------------------------------------------------

// Role constants for chat messages.
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// Message represents a single turn in a conversation. Content is the
// text payload; ToolCalls and ToolCallID enable the function-calling
// loop.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall represents a function call requested by the model.
type ToolCall struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"arguments"`
}

// ---------------------------------------------------------------------------
// Generation request / response
// ---------------------------------------------------------------------------

// GenerateRequest holds everything needed for a generation call.
type GenerateRequest struct {
	Messages    []Message       `json:"messages"`
	Model       string          `json:"model,omitempty"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature float32         `json:"temperature,omitempty"`
	TopP        float32         `json:"top_p,omitempty"`
	System      string          `json:"system,omitempty"`
	Tools       []ToolSpec      `json:"tools,omitempty"`
	Schema      json.RawMessage `json:"response_format,omitempty"` // JSON schema for structured output
	Stop        []string        `json:"stop,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
}

// ToolSpec describes a tool the model may call. Mirrors the OpenAI
// function-calling schema so providers can translate as needed.
type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"` // JSON Schema
}

// GenerateResponse is the result of a non-streaming generation.
type GenerateResponse struct {
	Text         string     `json:"text"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	Usage        *Usage     `json:"usage,omitempty"`
	Model        string     `json:"model"`
	FinishReason string     `json:"finish_reason,omitempty"`
}

// Usage holds token usage information.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ---------------------------------------------------------------------------
// Streaming
// ---------------------------------------------------------------------------

// StreamChunk is one piece of a streaming response.
type StreamChunk struct {
	Text      string     `json:"text,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	Usage     *Usage     `json:"usage,omitempty"`
	Done      bool       `json:"done,omitempty"`
}

// StreamResponse wraps a streaming channel plus a done channel.
type StreamResponse struct {
	// Chunks delivers text/tool-call chunks until closed.
	Chunks <-chan StreamChunk
	// Err is buffered (cap=1). A nil send signals clean completion.
	Err <-chan error
}

// Collect drains the stream and returns the concatenated text and
// final usage. Blocks until the stream is finished.
func (s *StreamResponse) Collect(ctx context.Context) (*GenerateResponse, error) {
	var text string
	var usage *Usage
	var toolCalls []ToolCall
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case chunk, ok := <-s.Chunks:
			if !ok {
				// channel closed — read error
				select {
				case err := <-s.Err:
					if err != nil {
						return nil, err
					}
				default:
				}
				return &GenerateResponse{Text: text, ToolCalls: toolCalls, Usage: usage}, nil
			}
			text += chunk.Text
			if chunk.Usage != nil {
				usage = chunk.Usage
			}
			toolCalls = append(toolCalls, chunk.ToolCalls...)
		}
	}
}

// ---------------------------------------------------------------------------
// Embeddings
// ---------------------------------------------------------------------------

// EmbeddingRequest asks a provider for vector embeddings.
type EmbeddingRequest struct {
	Input []string `json:"input"`
	Model string   `json:"model,omitempty"`
}

// EmbeddingResponse wraps one or more embedding vectors.
type EmbeddingResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
	Model      string      `json:"model"`
	Usage      *Usage      `json:"usage,omitempty"`
}

// ---------------------------------------------------------------------------
// Image generation
// ---------------------------------------------------------------------------

// ImageRequest configures an image generation call.
type ImageRequest struct {
	Prompt string `json:"prompt"`
	Model  string `json:"model,omitempty"`
	N      int    `json:"n,omitempty"`
	Size   string `json:"size,omitempty"` // e.g. "1024x1024"
	Style  string `json:"style,omitempty"`
}

// ImageResponse wraps the generated image data.
type ImageResponse struct {
	Images []ImageData `json:"images"`
	Model  string      `json:"model"`
}

// ImageData holds a single generated image.
type ImageData struct {
	URL     string `json:"url,omitempty"`
	B64JSON string `json:"b64_json,omitempty"`
}

// ---------------------------------------------------------------------------
// Observability
// ---------------------------------------------------------------------------

// RequestEvent is emitted for every AI API call. Middleware and
// observability hooks receive this.
type RequestEvent struct {
	Provider  string        `json:"provider"`
	Model     string        `json:"model"`
	Prompt    string        `json:"prompt,omitempty"`
	Messages  []Message     `json:"messages,omitempty"`
	Usage     *Usage        `json:"usage,omitempty"`
	Latency   time.Duration `json:"latency_ms"`
	Error     error         `json:"error,omitempty"`
	Timestamp time.Time     `json:"timestamp"`
}

// GenerateOption is a functional option applied to GenerateRequest.
type GenerateOption func(*GenerateRequest)

// WithModel overrides the model.
func WithModel(model string) GenerateOption {
	return func(r *GenerateRequest) { r.Model = model }
}

// WithMaxTokens overrides max tokens.
func WithMaxTokens(n int) GenerateOption {
	return func(r *GenerateRequest) { r.MaxTokens = n }
}

// WithTemperature sets the sampling temperature.
func WithTemperature(t float32) GenerateOption {
	return func(r *GenerateRequest) { r.Temperature = t }
}

// WithTopP sets the nucleus sampling parameter.
func WithTopP(p float32) GenerateOption {
	return func(r *GenerateRequest) { r.TopP = p }
}

// WithSystem sets the system prompt.
func WithSystem(s string) GenerateOption {
	return func(r *GenerateRequest) { r.System = s }
}

// WithMessages sets the full message list (overrides prompt).
func WithMessages(msgs []Message) GenerateOption {
	return func(r *GenerateRequest) { r.Messages = msgs }
}

// WithStop sets stop sequences.
func WithStop(stop ...string) GenerateOption {
	return func(r *GenerateRequest) { r.Stop = stop }
}

// WithSchema sets the JSON schema for structured output.
func WithSchema(schema json.RawMessage) GenerateOption {
	return func(r *GenerateRequest) { r.Schema = schema }
}
