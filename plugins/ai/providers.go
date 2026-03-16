/*
|--------------------------------------------------------------------------
| AI SDK — Provider Interface (v2)
|--------------------------------------------------------------------------
|
| Provider is the central abstraction for all AI backends. A provider
| must implement text generation; other capabilities are expressed as
| optional interfaces so lightweight wrappers (e.g. Ollama, Cohere)
| can ship without implementing everything.
|
| Capability interfaces:
|   • EmbeddingProvider   — vector embeddings
|   • ImageProvider       — image generation
|   • StreamProvider      — streaming text generation (separate from Generate)
|   • ToolCallProvider    — native function-calling support
|
*/

package ai

import "context"

// ---------------------------------------------------------------------------
// Core provider interface
// ---------------------------------------------------------------------------

// Provider is the minimum contract every AI backend must satisfy.
type Provider interface {
	// Name returns the provider identifier (e.g. "openai", "anthropic").
	Name() string

	// Generate produces a single completion.
	Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error)

	// Stream produces a streaming completion. If the provider does not
	// support native streaming, the default implementation calls
	// Generate and wraps the result in a single-chunk stream.
	Stream(ctx context.Context, req *GenerateRequest) (*StreamResponse, error)
}

// ---------------------------------------------------------------------------
// Capability interfaces — implement what your provider supports
// ---------------------------------------------------------------------------

// EmbeddingProvider generates vector embeddings.
type EmbeddingProvider interface {
	Embed(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error)
}

// ImageProvider generates images.
type ImageProvider interface {
	GenerateImage(ctx context.Context, req *ImageRequest) (*ImageResponse, error)
}

// ---------------------------------------------------------------------------
// Provider registry (global, thread-safe)
// ---------------------------------------------------------------------------

// ProviderFactory creates a Provider from a Config.
type ProviderFactory func(cfg *Config) (Provider, error)

var providerRegistry = map[string]ProviderFactory{}

// RegisterProvider makes a provider available by name.
// Call from init() in each provider file (e.g. openai.go, ollama.go).
func RegisterProvider(name string, factory ProviderFactory) {
	providerRegistry[name] = factory
}

// GetProviderFactory returns the factory for the named provider.
func GetProviderFactory(name string) (ProviderFactory, bool) {
	f, ok := providerRegistry[name]
	return f, ok
}

// AllProviders returns all registered provider names.
func AllProviders() []string {
	names := make([]string, 0, len(providerRegistry))
	for n := range providerRegistry {
		names = append(names, n)
	}
	return names
}

// ---------------------------------------------------------------------------
// Helpers for providers that don't support streaming natively
// ---------------------------------------------------------------------------

// GenerateToStream wraps a Generate call as a single-chunk
// StreamResponse. Use inside Stream() implementations for providers
// that lack native streaming.
func GenerateToStream(ctx context.Context, p Provider, req *GenerateRequest) (*StreamResponse, error) {
	resp, err := p.Generate(ctx, req)
	if err != nil {
		return nil, err
	}
	chunks := make(chan StreamChunk, 1)
	errCh := make(chan error, 1)
	go func() {
		defer close(chunks)
		defer close(errCh)
		chunks <- StreamChunk{
			Text:      resp.Text,
			ToolCalls: resp.ToolCalls,
			Usage:     resp.Usage,
			Done:      true,
		}
	}()
	return &StreamResponse{Chunks: chunks, Err: errCh}, nil
}
