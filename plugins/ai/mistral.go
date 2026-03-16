package ai

import (
	"context"
	"fmt"
)

func newMistralProvider(cfg *Config) (Provider, error) {
	if cfg.MistralKey == "" {
		return nil, fmt.Errorf("ai: MISTRAL_API_KEY is required for Mistral provider")
	}
	return &mistralProvider{apiKey: cfg.MistralKey, model: cfg.Model}, nil
}

type mistralProvider struct {
	apiKey string
	model  string
}

func (p *mistralProvider) Name() string { return "mistral" }

func (p *mistralProvider) Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
	return nil, fmt.Errorf("ai: Mistral provider not yet implemented - use AI_PROVIDER=openai or ollama")
}

func (p *mistralProvider) Stream(ctx context.Context, req *GenerateRequest) (*StreamResponse, error) {
	return nil, fmt.Errorf("ai: Mistral provider not yet implemented")
}
