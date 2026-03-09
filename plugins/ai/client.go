package ai

import (
	"context"
	"sync"
)

var (
	globalClient *Client
	clientMu     sync.RWMutex
)

// Client is the main AI client that delegates to the configured provider.
type Client struct {
	provider Provider
	config   *Config
}

// NewClient creates a new AI client from the given config.
func NewClient(cfg *Config) (*Client, error) {
	var provider Provider
	var err error
	switch cfg.Provider {
	case "openai":
		provider, err = newOpenAIProvider(cfg)
	case "xai":
		provider, err = newXAIProvider(cfg)
	case "ollama":
		provider, err = newOllamaProvider(cfg)
	case "anthropic":
		provider, err = newAnthropicProvider(cfg)
	case "gemini":
		provider, err = newGeminiProvider(cfg)
	case "mistral":
		provider, err = newMistralProvider(cfg)
	case "cohere":
		provider, err = newCohereProvider(cfg)
	default:
		provider, err = newOpenAIProvider(cfg)
	}
	if err != nil {
		return nil, err
	}
	return &Client{provider: provider, config: cfg}, nil
}

// setClient sets the global client (used by the plugin).
func setClient(c *Client) {
	clientMu.Lock()
	defer clientMu.Unlock()
	globalClient = c
}

// Client returns the global AI client. Panics if the plugin is not registered.
func GetClient() *Client {
	clientMu.RLock()
	defer clientMu.RUnlock()
	if globalClient == nil {
		panic("ai: plugin not registered. Call app.Use(ai.New())")
	}
	return globalClient
}

// Generate produces a completion for the given prompt.
func (c *Client) Generate(ctx context.Context, prompt string, opts ...GenerateOption) (*GenerateResponse, error) {
	req := &GenerateRequest{
		Messages:  []Message{{Role: "user", Content: prompt}},
		Model:     c.config.Model,
		MaxTokens: c.config.MaxTokens,
	}
	for _, opt := range opts {
		opt(req)
	}
	return c.GenerateRequest(ctx, req)
}

// GenerateRequest produces a completion for the given request.
func (c *Client) GenerateRequest(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
	if req.Model == "" {
		req.Model = c.config.Model
	}
	if req.MaxTokens <= 0 {
		req.MaxTokens = c.config.MaxTokens
	}
	return c.provider.Generate(ctx, req)
}

// Stream produces a streaming completion. Caller must consume the channel until closed.
func (c *Client) Stream(ctx context.Context, prompt string, opts ...GenerateOption) (<-chan string, <-chan error) {
	req := &GenerateRequest{
		Messages:  []Message{{Role: "user", Content: prompt}},
		Model:     c.config.Model,
		MaxTokens: c.config.MaxTokens,
	}
	for _, opt := range opts {
		opt(req)
	}
	return c.StreamRequest(ctx, req)
}

// StreamRequest produces a streaming completion for the given request.
func (c *Client) StreamRequest(ctx context.Context, req *GenerateRequest) (<-chan string, <-chan error) {
	if req.Model == "" {
		req.Model = c.config.Model
	}
	if req.MaxTokens <= 0 {
		req.MaxTokens = c.config.MaxTokens
	}
	return c.provider.Stream(ctx, req)
}

// GenerateOption configures a generation request.
type GenerateOption func(*GenerateRequest)

// WithModel sets the model to use.
func WithModel(model string) GenerateOption {
	return func(r *GenerateRequest) { r.Model = model }
}

// WithMaxTokens sets the max tokens.
func WithMaxTokens(n int) GenerateOption {
	return func(r *GenerateRequest) { r.MaxTokens = n }
}

// WithTemperature sets the temperature (0.0–2.0).
func WithTemperature(t float32) GenerateOption {
	return func(r *GenerateRequest) { r.Temperature = t }
}

// WithSystem sets the system message.
func WithSystem(s string) GenerateOption {
	return func(r *GenerateRequest) { r.System = s }
}

// WithMessages sets the full message list (overrides prompt).
func WithMessages(msgs []Message) GenerateOption {
	return func(r *GenerateRequest) { r.Messages = msgs }
}

// ---------------------------------------------------------------------------
// Package-level facade (uses global client)
// ---------------------------------------------------------------------------

// Generate is a convenience that uses the global client.
func Generate(ctx context.Context, prompt string, opts ...GenerateOption) (*GenerateResponse, error) {
	return GetClient().Generate(ctx, prompt, opts...)
}

// Stream is a convenience that uses the global client.
func Stream(ctx context.Context, prompt string, opts ...GenerateOption) (<-chan string, <-chan error) {
	return GetClient().Stream(ctx, prompt, opts...)
}
