/*
|--------------------------------------------------------------------------
| AI SDK — Client (v2)
|--------------------------------------------------------------------------
|
| The Client delegates to the configured provider and integrates
| observability hooks, guardrails, and the expanded type system.
|
*/

package ai

import (
	"context"
	"sync"
	"time"
)

var (
	globalClient *Client
	clientMu     sync.RWMutex
)

// Client is the main AI client that delegates to the configured provider.
type Client struct {
	provider   Provider
	config     *Config
	guardrails *Guardrails
	// image and video answer generation requests when their settings
	// differ from the text ones (see media.go).
	image, video       *Client
	imageErr, videoErr error
	// fallback answers requests naming the fallback model when it lives on
	// another account (see media.go).
	fallback    *Client
	fallbackErr error
}

// NewClient creates a new AI client from the given config.
func NewClient(cfg *Config) (*Client, error) {
	factory, ok := GetProviderFactory(cfg.Provider)
	if !ok {
		// Fallback: try the legacy switch for backward compatibility.
		return newClientLegacy(cfg)
	}
	provider, err := factory(cfg)
	if err != nil {
		return nil, err
	}
	c := &Client{provider: provider, config: cfg}
	c.configureMedia()
	return c, nil
}

// WithGuardrails returns a new client that validates all responses.
func WithGuardrailsClient(c *Client, g *Guardrails) *Client {
	return &Client{
		provider:    c.provider,
		config:      c.config,
		guardrails:  g,
		image:       c.image,
		video:       c.video,
		imageErr:    c.imageErr,
		videoErr:    c.videoErr,
		fallback:    c.fallback,
		fallbackErr: c.fallbackErr,
	}
}

// Provider returns the underlying provider.
func (c *Client) Provider() Provider {
	return c.provider
}

// setClient sets the global client (used by the plugin).
func setClient(c *Client) {
	clientMu.Lock()
	defer clientMu.Unlock()
	globalClient = c
}

// GetClient returns the global AI client. Panics if the plugin is not registered.
func GetClient() *Client {
	clientMu.RLock()
	defer clientMu.RUnlock()
	if globalClient == nil {
		panic("ai: plugin not registered. Call app.Use(ai.New())")
	}
	return globalClient
}

// IsConfigured returns true if the global client is set.
func IsConfigured() bool {
	clientMu.RLock()
	defer clientMu.RUnlock()
	return globalClient != nil
}

// ---------------------------------------------------------------------------
// Generate
// ---------------------------------------------------------------------------

// Generate produces a completion for the given prompt.
func (c *Client) Generate(ctx context.Context, prompt string, opts ...GenerateOption) (*GenerateResponse, error) {
	req := &GenerateRequest{
		Messages:  []Message{{Role: RoleUser, Content: prompt}},
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
	if fb, err := c.fallbackFor(req.Model); err != nil {
		return nil, err
	} else if fb != nil {
		return fb.GenerateRequest(ctx, req)
	}
	if req.MaxTokens <= 0 {
		req.MaxTokens = c.config.MaxTokens
	}
	attachRequestImages(req)

	start := time.Now()
	resp, err := c.provider.Generate(ctx, req)
	latency := time.Since(start)

	// Emit observability event.
	event := RequestEvent{
		Provider:  c.config.Provider,
		Model:     req.Model,
		Messages:  req.Messages,
		Latency:   latency,
		Error:     err,
		Timestamp: start,
	}
	if resp != nil {
		event.Usage = resp.Usage
	}
	EmitRequestEvent(event)

	if err != nil {
		return nil, err
	}

	// Apply guardrails.
	if c.guardrails != nil {
		if gErr := c.guardrails.ValidateResponse(resp); gErr != nil {
			return nil, gErr
		}
	}

	return resp, nil
}

// ---------------------------------------------------------------------------
// Stream
// ---------------------------------------------------------------------------

// Stream produces a streaming completion.
func (c *Client) Stream(ctx context.Context, prompt string, opts ...GenerateOption) (<-chan string, <-chan error) {
	req := &GenerateRequest{
		Messages:  []Message{{Role: RoleUser, Content: prompt}},
		Model:     c.config.Model,
		MaxTokens: c.config.MaxTokens,
		Stream:    true,
	}
	for _, opt := range opts {
		opt(req)
	}

	// Wrap the v2 StreamRequest into legacy channels for backward compat.
	textCh := make(chan string, 32)
	errCh := make(chan error, 1)

	go func() {
		defer close(textCh)
		defer close(errCh)

		sr, err := c.StreamRequest(ctx, req)
		if err != nil {
			errCh <- err
			return
		}
		for chunk := range sr.Chunks {
			if chunk.Text != "" {
				select {
				case textCh <- chunk.Text:
				case <-ctx.Done():
					errCh <- ctx.Err()
					return
				}
			}
		}
		select {
		case e, ok := <-sr.Err:
			if ok && e != nil {
				errCh <- e
			}
		case <-ctx.Done():
			errCh <- ctx.Err()
		}
	}()

	return textCh, errCh
}

// StreamRequest produces a v2 StreamResponse.
func (c *Client) StreamRequest(ctx context.Context, req *GenerateRequest) (*StreamResponse, error) {
	if req.Model == "" {
		req.Model = c.config.Model
	}
	if fb, err := c.fallbackFor(req.Model); err != nil {
		return nil, err
	} else if fb != nil {
		return fb.StreamRequest(ctx, req)
	}
	if req.MaxTokens <= 0 {
		req.MaxTokens = c.config.MaxTokens
	}
	req.Stream = true
	return c.provider.Stream(ctx, req)
}

// ---------------------------------------------------------------------------
// Package-level facades (uses global client)
// ---------------------------------------------------------------------------

// Generate is a convenience that uses the global client.
func Generate(ctx context.Context, prompt string, opts ...GenerateOption) (*GenerateResponse, error) {
	return GetClient().Generate(ctx, prompt, opts...)
}

// Stream is a convenience that uses the global client.
func Stream(ctx context.Context, prompt string, opts ...GenerateOption) (<-chan string, <-chan error) {
	return GetClient().Stream(ctx, prompt, opts...)
}

// ---------------------------------------------------------------------------
// Legacy client constructor (backward compat)
// ---------------------------------------------------------------------------

func newClientLegacy(cfg *Config) (*Client, error) {
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
	c := &Client{provider: provider, config: cfg}
	c.configureMedia()
	return c, nil
}

// attachRequestImages moves images given with WithImages onto the last user
// message, where providers read them, as Agent.Prompt already does. The
// caller's message slice is copied, not changed.
func attachRequestImages(req *GenerateRequest) {
	if len(req.Images) == 0 {
		return
	}
	msgs := append([]Message(nil), req.Messages...)
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == RoleUser || msgs[i].Role == "" {
			msgs[i].Images = append(append([]string(nil), msgs[i].Images...), req.Images...)
			req.Messages = msgs
			req.Images = nil
			return
		}
	}
}
