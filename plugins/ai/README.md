# AI SDK Plugin for Nimbus

A unified API for interacting with AI providers (OpenAI, Anthropic, etc.), inspired by [Laravel's AI SDK](https://laravel.com/docs/12.x/ai-sdk).

## Installation

```bash
go get github.com/CodeSyncr/nimbus/plugins/ai
```

Add the plugin in your `bin/server.go`:

```go
import (
    "github.com/CodeSyncr/nimbus"
    "github.com/CodeSyncr/nimbus/plugins/ai"
)

func main() {
    app := nimbus.New()
    app.Use(ai.New())
    // ...
}
```

## Configuration

Set environment variables in your `.env`:

```env
AI_PROVIDER=openai
AI_MODEL=gpt-4o
OPENAI_API_KEY=sk-...
```

| Variable | Description | Default |
|----------|-------------|---------|
| `AI_PROVIDER` | Provider: `openai`, `xai`, `ollama`, `anthropic`, `gemini`, `mistral`, `cohere` | `openai` |
| `AI_MODEL` | Model to use | `gpt-4o` |
| `OPENAI_API_KEY` | OpenAI API key | required for OpenAI |
| `ANTHROPIC_API_KEY` | Anthropic API key | required for Anthropic |
| `COHERE_API_KEY` | Cohere API key | required for Cohere |
| `ELEVENLABS_API_KEY` | ElevenLabs API key (TTS, future) | — |
| `GEMINI_API_KEY` | Google Gemini API key | required for Gemini |
| `MISTRAL_API_KEY` | Mistral AI API key | required for Mistral |
| `OLLAMA_HOST` | Ollama server URL | `http://localhost:11434` |
| `XAI_API_KEY` | xAI Grok API key | required for xAI |
| `JINA_API_KEY` | Jina embeddings (future) | — |
| `VOYAGEAI_API_KEY` | Voyage AI embeddings (future) | — |

## Usage

### Simple text generation

```go
import "github.com/CodeSyncr/nimbus/plugins/ai"

func myHandler(c *http.Context) error {
    response, err := ai.Generate(c.Request().Context(), "Explain quantum computing in simple terms")
    if err != nil {
        return err
    }
    return c.JSON(200, map[string]string{"answer": response.Text})
}
```

### Streaming

```go
func streamHandler(c *http.Context) error {
    c.Response().Header().Set("Content-Type", "text/event-stream")
    c.Response().Header().Set("Cache-Control", "no-cache")
    c.Response().Header().Set("Connection", "keep-alive")

    stream, errCh := ai.Stream(c.Request().Context(), "Write a haiku about Go")
    flusher := c.Response().(http.Flusher)

    for chunk := range stream {
        fmt.Fprint(c.Response(), chunk)
        flusher.Flush()
    }
    if err := <-errCh; err != nil {
        return err
    }
    return nil
}
```

### Agents

Agents encapsulate instructions (system prompt) and optional conversation context:

```go
agent := ai.NewAgent("You are a helpful coding assistant specializing in Go.")
response, err := agent.Prompt(c.Request().Context(), "How do I use channels for concurrency?")
```

With conversation history:

```go
agent := ai.NewAgent("You are a sales coach.").
    WithMessages([]ai.Message{
        {Role: "user", Content: "I have a call with a prospect tomorrow."},
        {Role: "assistant", Content: "Great! What's their company and role?"},
    })
response, err := agent.Prompt(c.Request().Context(), "They're the CTO at a 50-person startup.")
```

### Options

```go
response, err := ai.Generate(ctx, "Summarize this", 
    ai.WithModel("gpt-4o-mini"),
    ai.WithMaxTokens(500),
    ai.WithTemperature(0.7),
    ai.WithSystem("You are a concise summarizer."),
)
```

## Providers

| Provider | Status |
|----------|--------|
| OpenAI | Supported |
| xAI (Grok) | Supported (OpenAI-compatible API) |
| Ollama | Supported (local, no key) |
| Anthropic | Config ready, implementation planned |
| Gemini | Config ready, implementation planned |
| Mistral | Config ready, implementation planned |
| Cohere | Config ready, implementation planned |
| ElevenLabs | Config ready (TTS, future) |
| Jina | Config ready (embeddings, future) |
| VoyageAI | Config ready (embeddings, future) |

## Roadmap

- [ ] Anthropic provider
- [ ] Structured output (JSON schema)
- [ ] Tool/function calling
- [ ] Embeddings
- [x] MCP (Model Context Protocol) — see [plugins/mcp](../mcp/README.md)
