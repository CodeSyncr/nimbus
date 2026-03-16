/*
|--------------------------------------------------------------------------
| AI SDK — Embeddings
|--------------------------------------------------------------------------
|
| Provides a unified embedding API that delegates to the configured
| provider's EmbeddingProvider capability, or uses the model's
| provider-specific embedding endpoint.
|
| Usage:
|
|   vector, err := ai.Embed(ctx, "hello world")
|   vectors, err := ai.EmbedBatch(ctx, []string{"hello", "world"})
|
*/

package ai

import (
	"context"
	"fmt"
)

// ---------------------------------------------------------------------------
// Package-level embedding facade
// ---------------------------------------------------------------------------

// Embed generates a vector embedding for a single text input.
func Embed(ctx context.Context, text string, opts ...GenerateOption) ([]float32, error) {
	resp, err := EmbedBatch(ctx, []string{text}, opts...)
	if err != nil {
		return nil, err
	}
	if len(resp.Embeddings) == 0 {
		return nil, fmt.Errorf("ai: embed: no embeddings returned")
	}
	return resp.Embeddings[0], nil
}

// EmbedBatch generates vector embeddings for multiple text inputs.
func EmbedBatch(ctx context.Context, texts []string, opts ...GenerateOption) (*EmbeddingResponse, error) {
	client := GetClient()

	// Build a GenerateRequest to extract model from options.
	gr := &GenerateRequest{}
	for _, opt := range opts {
		opt(gr)
	}

	req := &EmbeddingRequest{
		Input: texts,
		Model: gr.Model,
	}

	ep, ok := client.provider.(EmbeddingProvider)
	if !ok {
		return nil, fmt.Errorf("ai: provider %q does not support embeddings", client.config.Provider)
	}
	return ep.Embed(ctx, req)
}

// ---------------------------------------------------------------------------
// Cosine similarity
// ---------------------------------------------------------------------------

// CosineSimilarity computes the cosine similarity between two vectors.
// Returns a value in [-1, 1]; higher = more similar.
func CosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float32
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (sqrt32(normA) * sqrt32(normB))
}

func sqrt32(x float32) float32 {
	// Newton's method for float32 sqrt.
	if x <= 0 {
		return 0
	}
	z := x
	for i := 0; i < 10; i++ {
		z = (z + x/z) / 2
	}
	return z
}
