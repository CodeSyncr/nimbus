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
The `ai.Client` handles communication with multiple providers (OpenAI, Anthropic, Gemini, Ollama, etc.) through a unified interface. Providers support custom base URLs (`OPENAI_BASE_URL`, `ANTHROPIC_BASE_URL` / `ANTHROPIC_API_URL`) and automatic exponential backoff retries on transient network and gateway errors (`502`, `503`, `504`, `429`).

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
