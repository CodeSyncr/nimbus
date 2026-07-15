package ai

import (
	"fmt"

	openai "github.com/sashabaranov/go-openai"
)

// Mistral exposes an OpenAI-compatible Chat Completions API at api.mistral.ai/v1,
// so it reuses the OpenAI provider with a different base URL (same pattern as xAI).
const mistralBaseURL = "https://api.mistral.ai/v1"

func newMistralProvider(cfg *Config) (*openAIProvider, error) {
	if cfg.MistralKey == "" {
		return nil, fmt.Errorf("ai: MISTRAL_API_KEY is required for Mistral provider")
	}
	clientConfig := openai.DefaultConfig(cfg.MistralKey)
	clientConfig.BaseURL = mistralBaseURL
	client := openai.NewClientWithConfig(clientConfig)
	model := cfg.Model
	if model == "" {
		model = "mistral-large-latest"
	}
	return &openAIProvider{client: client, model: model}, nil
}
