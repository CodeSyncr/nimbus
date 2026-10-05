# AI SDK Deep Dive - Nimbus

The `plugins/ai` package is a comprehensive AI orchestration layer for Go, integrated directly into the Nimbus framework.

## Architecture

Inspired by Vercel AI SDK and LangChain, it provides unified abstractions for:
-   **Text Generation & Streaming**
-   **Structured Output (Extraction)**
-   **Agents & Reasoning Loops**
-   **Function Calling (Tools)**
-   **Embeddings & Vector Stores**
-   **RAG Engine**
-   **Prompt Templates & Chains**
-   **Workflows (Pipelines)**
-   **Image & Video Generation**
-   **Observability & Cost Tracking**

## Core Components

### Clients & Providers
The `ai.Client` handles communication with multiple providers (OpenAI, Anthropic, Gemini, Ollama, etc.) through a unified interface. Providers support custom base URLs (`OPENAI_BASE_URL`, `ANTHROPIC_BASE_URL` / `ANTHROPIC_API_URL`) and automatic exponential backoff retries on transient network/gateway errors (`502`, `503`, `504`, `429`) as well as dropped connections/unexpected EOFs.

### Dedicated Media Routing
Image and video generation can be routed to separate providers, endpoints, and models via environment variables or `ai.Config`:
- `AI_IMAGE_PROVIDER`, `AI_IMAGE_MODEL`, `AI_IMAGE_API_KEY`, `AI_IMAGE_BASE_URL`
- `AI_VIDEO_PROVIDER`, `AI_VIDEO_MODEL`, `AI_VIDEO_API_KEY`, `AI_VIDEO_BASE_URL`
When unset, media builders fall back seamlessly to the primary text provider.

### Fallback Model
`AI_FALLBACK_MODEL` names the model to retry on when the main one fails (out of credit, overloaded). Give it its own account with `AI_FALLBACK_PROVIDER`, `AI_FALLBACK_API_KEY` and `AI_FALLBACK_BASE_URL`: any request whose model is the fallback model (`ai.WithModel(os.Getenv("AI_FALLBACK_MODEL"))`) is then routed there. Base URLs may be given with or without a trailing `/chat/completions`.

### Changing Models at Runtime
`ai.Reload()` rebuilds the global client from the current environment. To switch models, keys or URLs without a restart, `os.Setenv` the `AI_*` variables and call `ai.Reload()`. On error the previous client is kept.

### Streaming Responses
`c.SSEStream` lifts the server's read/write deadlines for that response, so long streamed agent replies are not cut off by `SERVER_WRITE_TIMEOUT`. While the handler is quiet it sends a `: keep-alive` comment every `http.SSEKeepAlive` (15s) so proxies such as Cloudflare do not drop the idle connection.

### Agents
Agents combine instructions, tools, and memory to perform complex tasks autonomously.
```go
agent := ai.NewAgent("You are a researcher").WithTools("search", "writer")
resp, _ := agent.Prompt(ctx, "Research Go concurrency")
```

### Tools
Register Go functions as tools that agents can call. Use the `ai.Tool` struct for registration.

### Vector Stores
Backends for storage and retrieval of embeddings:
-   **In-Memory**: For development.
-   **pgvector**: PostgreSQL integration.
-   **Qdrant / Pinecone**: Dedicated vector databases.

### RAG Engine
Combines vector stores and text generation for Retrieval-Augmented Generation.
```go
rag := ai.NewRAG(store).TopK(5)
answer, _ := rag.Ask(ctx, "What is goroutine?")
```

## Structured Output

Extract typed Go structs from strings using `ai.Extract[T](ctx, text)`.

## Observability

Built-in support for OpenTelemetry tracing and cost tracking with budget alerts.

## Best Practices

1.  **Use Streaming**: Prefer `Stream()` for long responses to improve user experience.
2.  **Granular Tools**: Define specialized tools rather than general-purpose ones.
3.  **Guardrails**: Apply `ai.Guardrails` to validate and filter the AI's output.
4.  **Cost Monitoring**: Enable cost tracking in production to stay within budget.

### Website Studio application architecture

In `nimbus-starter`, Vani generation uses a persisted `sites.Plan` with `PlanPage.Scenes`
(narrative beat, visual, layout, motion, evidence, transition). `generationGuidance`
provides one bounded creative contract rather than keyword-selected skill bundles.
The static HTML runtime uses locally bundled Motion JavaScript, not React JSX.

Studio chat uses the same concise instructions in lean and cached modes. Per-turn
results remain stable for prefix reuse. `memory.RecallSite` scopes recalled facts to
sessions owned by the user for the current website; `sites.ProjectMemory` carries
bounded design intent, not claims about current file contents. Context fingerprints
include changed profile and project intent. Compaction sees unsummarized history
before the replay cap and accounts for tool payloads. Cache counts are telemetry,
not evidence that an unreported cache is unavailable. Full application notes and
opt-in evaluation commands: `nimbus-starter/docs/engineering/vani-architecture.md`.
