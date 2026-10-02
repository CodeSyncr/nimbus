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
	Role    string   `json:"role"`
	Content string   `json:"content"`
	Images  []string `json:"images,omitempty"`
	// Files are documents (PDF, text) or images attached to a user turn:
	// paths, data: URIs or URLs. See WithFiles.
	Files      []string   `json:"files,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	// Reasoning is the model's thinking on an assistant turn, kept so it
	// can be sent back: Anthropic requires the signed thinking blocks of a
	// turn that called tools to accompany the tool results.
	Reasoning []ReasoningBlock `json:"reasoning,omitempty"`
}

// ReasoningBlock is one piece of a model's thinking.
type ReasoningBlock struct {
	Text string `json:"text,omitempty"`
	// Signature verifies the block to the provider (Anthropic).
	Signature string `json:"signature,omitempty"`
	// Redacted holds encrypted thinking the provider did not show.
	Redacted string `json:"redacted,omitempty"`
}

// Reasoning asks a reasoning model to think before it answers. Set Effort
// ("low", "medium", "high") or BudgetTokens (the most tokens to spend
// thinking); each provider gets whichever it understands.
type Reasoning struct {
	Effort       string
	BudgetTokens int
}

// effort returns the effort level, derived from the budget when only a
// budget was given.
func (r *Reasoning) effort() string {
	switch {
	case r == nil:
		return ""
	case r.Effort != "":
		return r.Effort
	case r.BudgetTokens >= 16384:
		return "high"
	case r.BudgetTokens >= 4096:
		return "medium"
	default:
		return "low"
	}
}

// budget returns the thinking budget, derived from the effort when only an
// effort was given.
func (r *Reasoning) budget() int {
	switch {
	case r == nil:
		return 0
	case r.BudgetTokens > 0:
		return r.BudgetTokens
	}
	switch r.Effort {
	case "high":
		return 24576
	case "medium":
		return 8192
	default:
		return 2048
	}
}

// ToolCall represents a function call requested by the model.
type ToolCall struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"arguments"`
	// Signature is a provider token that must accompany the call when it is
	// sent back (Gemini's thought signature with thinking on).
	Signature string `json:"signature,omitempty"`
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
	// Images carries attachments for the current user turn (data URIs, http(s)
	// URLs, or provider-resolvable paths). The Agent moves these onto the
	// current user Message; providers serialize them as multimodal content.
	Images []string `json:"-"`
	// Files carries documents for the current user turn (see WithFiles).
	Files []string `json:"-"`
	// Cache asks the provider to cache the stable start of the prompt
	// (system prompt, tools, conversation so far). See WithPromptCache.
	Cache bool `json:"-"`
	// Reasoning turns on thinking for reasoning models (see WithReasoning).
	Reasoning *Reasoning `json:"-"`
	// ToolChoice controls tool use: "" or "auto" (the model decides),
	// "none" (no tools), "required" (must call one), or a tool's name.
	ToolChoice string `json:"-"`
}

// Tool choices for WithToolChoice.
const (
	ToolChoiceAuto     = "auto"
	ToolChoiceNone     = "none"
	ToolChoiceRequired = "required"
)

// forcesTool reports whether the choice makes the model call a tool.
func forcesTool(choice string) bool {
	return choice != "" && choice != ToolChoiceAuto && choice != ToolChoiceNone
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
	Text      string     `json:"text"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// Reasoning is the model's visible thinking (Anthropic, Gemini, Ollama,
	// DeepSeek-style APIs); OpenAI's reasoning models do not return it.
	Reasoning       string           `json:"reasoning,omitempty"`
	ReasoningBlocks []ReasoningBlock `json:"reasoning_blocks,omitempty"`
	Usage           *Usage           `json:"usage,omitempty"`
	Model           string           `json:"model"`
	FinishReason    string           `json:"finish_reason,omitempty"`
	// Agent names the agent that produced the response, after handoffs
	// (empty for unnamed agents).
	Agent string `json:"agent,omitempty"`
}

// Usage holds token usage information.
type Usage struct {
	// PromptTokens counts every input token, cached or not.
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	// CacheReadTokens are input tokens served from the provider's prompt
	// cache (billed at a discount); CacheWriteTokens were written to it
	// (Anthropic bills these at a premium). Both are part of PromptTokens.
	CacheReadTokens  int `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
	// ReasoningTokens are output tokens spent thinking (part of
	// CompletionTokens), when the provider reports them.
	ReasoningTokens int `json:"reasoning_tokens,omitempty"`
}

// ---------------------------------------------------------------------------
// Streaming
// ---------------------------------------------------------------------------

// StreamChunk is one piece of a streaming response.
type StreamChunk struct {
	Text      string     `json:"text,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// Reasoning carries completed thinking blocks (with signatures) so they
	// can be sent back with tool results.
	Reasoning []ReasoningBlock `json:"reasoning,omitempty"`
	Usage     *Usage           `json:"usage,omitempty"`
	Done      bool             `json:"done,omitempty"`
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
	var reasoning []ReasoningBlock
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
				resp := &GenerateResponse{Text: text, ToolCalls: toolCalls, Usage: usage, ReasoningBlocks: reasoning}
				for _, r := range reasoning {
					resp.Reasoning += r.Text
				}
				return resp, nil
			}
			text += chunk.Text
			reasoning = append(reasoning, chunk.Reasoning...)
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

// WithImages attaches image references (data URIs, http(s) URLs, or
// provider-resolvable paths) to the current user turn. Agent.Prompt moves
// them onto the user Message so vision-capable providers can send them.
func WithImages(images []string) GenerateOption {
	return func(r *GenerateRequest) { r.Images = images }
}

// WithFiles attaches documents (PDF, text) or images to the current user
// turn: paths, data: URIs or http(s) URLs. Anthropic and Gemini read PDFs
// natively (layout, tables, figures); other providers get the extracted
// text.
func WithFiles(refs ...string) GenerateOption {
	return func(r *GenerateRequest) { r.Files = append(r.Files, refs...) }
}

// WithToolChoice controls tool use: ToolChoiceAuto, ToolChoiceNone,
// ToolChoiceRequired, or a tool's name to force that tool. In an agent a
// forcing choice applies to the first step only, so the agent can answer.
func WithToolChoice(choice string) GenerateOption {
	return func(r *GenerateRequest) { r.ToolChoice = choice }
}

// WithPromptCache caches the stable start of the prompt — system prompt,
// tools and the conversation so far — so repeated requests pay for it at a
// fraction of the price. Anthropic needs it asked for; OpenAI and Gemini
// cache long prompts on their own, and report it in Usage either way.
func WithPromptCache() GenerateOption {
	return func(r *GenerateRequest) { r.Cache = true }
}

// WithReasoning asks a reasoning model to think first: "low", "medium" or
// "high" effort. Anthropic and Gemini get a matching thinking budget
// (2,048 / 8,192 / 24,576 tokens); OpenAI gets reasoning_effort.
func WithReasoning(effort string) GenerateOption {
	return func(r *GenerateRequest) { r.Reasoning = &Reasoning{Effort: effort} }
}

// WithThinkingBudget caps how many tokens the model may spend thinking.
func WithThinkingBudget(tokens int) GenerateOption {
	return func(r *GenerateRequest) { r.Reasoning = &Reasoning{BudgetTokens: tokens} }
}

// WithSchema sets the JSON schema for structured output.
func WithSchema(schema json.RawMessage) GenerateOption {
	return func(r *GenerateRequest) { r.Schema = schema }
}
