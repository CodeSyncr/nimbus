package ai

import (
	"fmt"

	openai "github.com/sashabaranov/go-openai"
)

// xAI uses OpenAI-compatible API at api.x.ai/v1
const xAIBaseURL = "https://api.x.ai/v1"

func newXAIProvider(cfg *Config) (*openAIProvider, error) {
	if cfg.XAIKey == "" {
		return nil, fmt.Errorf("ai: XAI_API_KEY is required for xAI provider")
	}
	clientConfig := openai.DefaultConfig(cfg.XAIKey)
	clientConfig.BaseURL = xAIBaseURL
	client := openai.NewClientWithConfig(clientConfig)
	model := cfg.Model
	if model == "" {
		model = "grok-2"
	}
	return &openAIProvider{client: client, model: model}, nil
}
