package ai

import (
	"context"
	"fmt"
)

func newGeminiProvider(cfg *Config) (Provider, error) {
	if cfg.GeminiKey == "" {
		return nil, fmt.Errorf("ai: GEMINI_API_KEY is required for Gemini provider")
	}
	return &geminiProvider{apiKey: cfg.GeminiKey, model: cfg.Model}, nil
}

type geminiProvider struct {
	apiKey string
	model  string
}

func (p *geminiProvider) Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
	return nil, fmt.Errorf("ai: Gemini provider not yet implemented - use AI_PROVIDER=openai or ollama")
}

func (p *geminiProvider) Stream(ctx context.Context, req *GenerateRequest) (<-chan string, <-chan error) {
	errCh := make(chan error, 1)
	textCh := make(chan string)
	errCh <- fmt.Errorf("ai: Gemini provider not yet implemented")
	close(textCh)
	close(errCh)
	return textCh, errCh
}
