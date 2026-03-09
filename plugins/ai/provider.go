package ai

import "context"

// Provider is the interface for AI providers (OpenAI, Anthropic, etc.).
type Provider interface {
	// Generate produces a single completion for the given messages.
	Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error)
	// Stream produces a streaming completion. The channel receives text chunks.
	Stream(ctx context.Context, req *GenerateRequest) (<-chan string, <-chan error)
}

// GenerateRequest holds the input for text generation.
type GenerateRequest struct {
	Messages    []Message
	Model       string
	MaxTokens   int
	Temperature float32
	System      string
}

// Message represents a chat message.
type Message struct {
	Role    string
	Content string
}

// GenerateResponse holds the completion result.
type GenerateResponse struct {
	Text   string
	Usage  *Usage
	Model  string
}

// Usage holds token usage info.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}
