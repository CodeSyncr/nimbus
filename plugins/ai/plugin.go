/*
|--------------------------------------------------------------------------
| AI SDK Plugin for Nimbus
|--------------------------------------------------------------------------
|
| This plugin provides a unified API for interacting with AI providers
| (OpenAI, Anthropic, etc.), inspired by Laravel's AI SDK.
|
| Features:
|   - Provider abstraction (OpenAI, Anthropic with more coming)
|   - Text generation (sync and streaming)
|   - Agents with instructions and conversation context
|   - Structured output via JSON schema
|   - Config-driven API keys and model selection
|
| Usage:
|
|   // bin/server.go
|   app.Use(ai.New())
|
|   // In a handler
|   response, err := ai.Generate(c.Request().Context(), "Explain quantum computing")
|   stream, err := ai.Stream(c.Request().Context(), "Write a haiku about Go")
|
|   // With an agent
|   agent := ai.NewAgent("You are a helpful coding assistant.")
|   response, err := agent.Prompt(c.Request().Context(), "How do I use channels in Go?")
|
| Configuration (config/ai.go or .env):
|   AI_PROVIDER=openai
|   AI_MODEL=gpt-4o
|   OPENAI_API_KEY=sk-...
|   ANTHROPIC_API_KEY=sk-ant-...
|
*/

package ai

import (
	"fmt"
	"os"

	"github.com/CodeSyncr/nimbus"
)

var (
	_ nimbus.Plugin    = (*Plugin)(nil)
	_ nimbus.HasConfig = (*Plugin)(nil)
)

// Plugin integrates the AI SDK with Nimbus.
type Plugin struct {
	nimbus.BasePlugin
	client *Client
}

// New creates a new AI plugin instance.
func New() *Plugin {
	return &Plugin{
		BasePlugin: nimbus.BasePlugin{
			PluginName:    "ai",
			PluginVersion: "1.0.0",
		},
	}
}

// Register binds the AI client to the container.
func (p *Plugin) Register(app *nimbus.App) error {
	cfg := p.loadConfig(app)
	if cfg.Provider == "openai" && cfg.OpenAIKey == "" {
		return fmt.Errorf("ai: AI_PROVIDER=openai but OPENAI_API_KEY is not set. Add OPENAI_API_KEY to your .env, or remove AI_PROVIDER to use Ollama for local dev")
	}
	client, err := NewClient(cfg)
	if err != nil {
		return err
	}
	p.client = client
	setClient(client)
	app.Container.Singleton("ai.client", func() *Client { return client })
	return nil
}

// Boot performs post-registration setup (no-op for now).
func (p *Plugin) Boot(app *nimbus.App) error {
	return nil
}

// DefaultConfig returns the default configuration for the AI plugin.
func (p *Plugin) DefaultConfig() map[string]any {
	return map[string]any{
		"provider":   "openai",
		"model":      "gpt-4o",
		"timeout":    60,
		"max_tokens": 1024,
	}
}

func (p *Plugin) loadConfig(app *nimbus.App) *Config {
	cfg := &Config{
		Provider:  "openai",
		Model:     "gpt-4o",
		Timeout:   60,
		MaxTokens: 1024,
	}

	if pluginCfg := app.PluginConfig("ai"); pluginCfg != nil {
		if v, ok := pluginCfg["provider"].(string); ok && v != "" {
			cfg.Provider = v
		}
		if v, ok := pluginCfg["model"].(string); ok && v != "" {
			cfg.Model = v
		}
		if v, ok := pluginCfg["timeout"].(int); ok && v > 0 {
			cfg.Timeout = v
		}
		if v, ok := pluginCfg["max_tokens"].(int); ok && v > 0 {
			cfg.MaxTokens = v
		}
	}

	// Override from env
	cfg.OpenAIKey = os.Getenv("OPENAI_API_KEY")
	cfg.AnthropicKey = os.Getenv("ANTHROPIC_API_KEY")
	cfg.CohereKey = os.Getenv("COHERE_API_KEY")
	cfg.GeminiKey = os.Getenv("GEMINI_API_KEY")
	cfg.MistralKey = os.Getenv("MISTRAL_API_KEY")
	cfg.XAIKey = os.Getenv("XAI_API_KEY")
	cfg.JinaKey = os.Getenv("JINA_API_KEY")
	cfg.VoyageAIKey = os.Getenv("VOYAGEAI_API_KEY")
	cfg.ElevenLabsKey = os.Getenv("ELEVENLABS_API_KEY")
	if host := os.Getenv("OLLAMA_HOST"); host != "" {
		cfg.OllamaHost = host
	} else {
		cfg.OllamaHost = "http://localhost:11434"
	}
	if provider := os.Getenv("AI_PROVIDER"); provider != "" {
		cfg.Provider = provider
	}
	if model := os.Getenv("AI_MODEL"); model != "" {
		cfg.Model = model
	}

	// Fall back to Ollama when OpenAI is selected but no API key (only if provider not explicitly set)
	providerFromEnv := os.Getenv("AI_PROVIDER") != ""
	if cfg.Provider == "openai" && cfg.OpenAIKey == "" && !providerFromEnv {
		cfg.Provider = "ollama"
		if cfg.Model == "gpt-4o" {
			cfg.Model = "llama3.2"
		}
	}

	return cfg
}
