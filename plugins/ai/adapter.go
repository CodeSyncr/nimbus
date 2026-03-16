/*
|--------------------------------------------------------------------------
| AI SDK — Provider Adapters
|--------------------------------------------------------------------------
|
| Bridges between the legacy Provider interface (v1) and the
| expanded v2 provider interface. Existing providers that return
| (<-chan string, <-chan error) from Stream are wrapped automatically.
|
*/

package ai

import "context"

// ---------------------------------------------------------------------------
// LegacyProvider wraps old-style providers into the v2 interface.
// ---------------------------------------------------------------------------

// LegacyStreamProvider is the original Stream signature.
type LegacyStreamProvider interface {
	Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error)
	LegacyStream(ctx context.Context, req *GenerateRequest) (<-chan string, <-chan error)
}

// wrapLegacyStream converts old-style (<-chan string, <-chan error) into
// (*StreamResponse, error). Used internally for backward compat.
func wrapLegacyStream(textCh <-chan string, errCh <-chan error) (*StreamResponse, error) {
	chunks := make(chan StreamChunk, 32)
	outErr := make(chan error, 1)

	go func() {
		defer close(chunks)
		defer close(outErr)
		for text := range textCh {
			chunks <- StreamChunk{Text: text}
		}
		// Check error channel.
		select {
		case err := <-errCh:
			if err != nil {
				outErr <- err
			}
		default:
		}
	}()

	return &StreamResponse{Chunks: chunks, Err: outErr}, nil
}

// ---------------------------------------------------------------------------
// Adapters for existing providers to add Name() method
// ---------------------------------------------------------------------------

// providerWithName wraps a Provider and adds a Name() method.
type providerWithName struct {
	Provider
	name string
}

func (p *providerWithName) Name() string {
	return p.name
}

// wrapWithName wraps a provider to satisfy the v2 Provider interface
// by adding a Name() method if it doesn't already have one.
func wrapWithName(p Provider, name string) Provider {
	// Check if it already has Name().
	if p.Name() != "" {
		return p
	}
	return &providerWithName{Provider: p, name: name}
}
