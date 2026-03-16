package ai

import (
	"context"
	"fmt"
)

func newCohereProvider(cfg *Config) (Provider, error) {
	if cfg.CohereKey == "" {
		return nil, fmt.Errorf("ai: COHERE_API_KEY is required for Cohere provider")
	}
	return &cohereProvider{apiKey: cfg.CohereKey, model: cfg.Model}, nil
}

type cohereProvider struct {
	apiKey string
	model  string
}

func (p *cohereProvider) Name() string { return "cohere" }

func (p *cohereProvider) Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
	return nil, fmt.Errorf("ai: Cohere provider not yet implemented - use AI_PROVIDER=openai or ollama")
}

func (p *cohereProvider) Stream(ctx context.Context, req *GenerateRequest) (*StreamResponse, error) {
	return nil, fmt.Errorf("ai: Cohere provider not yet implemented")
}
