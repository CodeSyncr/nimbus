package ai

import (
	"context"
	"fmt"
)

func newAnthropicProvider(cfg *Config) (Provider, error) {
	if cfg.AnthropicKey == "" {
		return nil, fmt.Errorf("ai: ANTHROPIC_API_KEY is required for Anthropic provider")
	}
	return &anthropicProvider{apiKey: cfg.AnthropicKey, model: cfg.Model}, nil
}

type anthropicProvider struct {
	apiKey string
	model  string
}

func (p *anthropicProvider) Name() string { return "anthropic" }

func (p *anthropicProvider) Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
	return nil, fmt.Errorf("ai: Anthropic provider not yet implemented - use AI_PROVIDER=openai or ollama")
}

func (p *anthropicProvider) Stream(ctx context.Context, req *GenerateRequest) (*StreamResponse, error) {
	return nil, fmt.Errorf("ai: Anthropic provider not yet implemented")
}
