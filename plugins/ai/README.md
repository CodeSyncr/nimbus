# AI SDK Plugin for Nimbus

A comprehensive AI backend framework for Go — providing unified abstractions for text generation, streaming, embeddings, agents, RAG, structured output, workflows, image/video generation, and observability.

Inspired by [Laravel AI SDK](https://laravel.com/docs/12.x/ai-sdk), [Vercel AI SDK](https://sdk.vercel.ai/), and LangChain — but Go-native and framework-integrated.

## Installation

```bash
go get github.com/CodeSyncr/nimbus/plugins/ai
```

Add the plugin in your `bin/server.go`:

```go
app := nimbus.New()
app.Use(ai.New())
```

## Configuration

```env
AI_PROVIDER=openai       # openai | anthropic | gemini | mistral | cohere | xai | ollama
AI_MODEL=gpt-4o
OPENAI_API_KEY=sk-...
ANTHROPIC_API_KEY=sk-ant-...
```

| Variable | Description | Default |
|---|---|---|
| `AI_PROVIDER` | Provider backend | `openai` |
| `AI_MODEL` | Default model | `gpt-4o` |
| `OPENAI_API_KEY` | OpenAI API key | required for OpenAI |
| `ANTHROPIC_API_KEY` | Anthropic API key | required for Anthropic |
| `GEMINI_API_KEY` | Google Gemini API key | required for Gemini |
| `MISTRAL_API_KEY` | Mistral API key | required for Mistral |
| `COHERE_API_KEY` | Cohere API key | required for Cohere |
| `XAI_API_KEY` | xAI Grok API key | required for xAI |
| `OLLAMA_HOST` | Ollama server URL | `http://localhost:11434` |

---

## Core API

### Text Generation

```go
response, err := ai.Generate(ctx, "Explain quantum computing")
fmt.Println(response.Text)
fmt.Println(response.Usage.TotalTokens)
```

### Streaming

```go
textCh, errCh := ai.Stream(ctx, "Write a haiku about Go")
for text := range textCh {
    fmt.Print(text)
}
if err := <-errCh; err != nil {
    log.Fatal(err)
}
```

### Options

```go
resp, err := ai.Generate(ctx, "Hello",
    ai.WithModel("gpt-4o-mini"),
    ai.WithMaxTokens(500),
    ai.WithTemperature(0.7),
    ai.WithSystem("You are a pirate."),
)
```

---

## Structured Output (Extract)

Extract typed Go structs from unstructured text:

```go
type Invoice struct {
    Merchant string
    Amount   float64
    Date     string
}

invoice, err := ai.Extract[Invoice](ctx, receiptText)
// invoice.Merchant = "Starbucks"
// invoice.Amount = 5.50
```

Extract slices:

```go
items, err := ai.ExtractSlice[LineItem](ctx, invoiceText)
```

Classify text:

```go
label, err := ai.Classify(ctx, "I love this product!", []string{"positive", "negative", "neutral"})
// label = "positive"
```

---

## Agents

Agents combine instructions, tools, memory, and a reasoning loop:

```go
agent := ai.NewAgent("You are a Go programming expert").
    WithTools("weather", "calculator").
    MaxSteps(10)

response, err := agent.Prompt(ctx, "What's the weather in NYC and what's 42 * 73?")
```

### Streaming agent

```go
stream, err := agent.Stream(ctx, "Write a detailed explanation of Go channels")
for chunk := range stream.Chunks {
    fmt.Print(chunk.Text)
}
```

### Agent with memory

```go
agent := ai.NewAgent("You are a helpful assistant").
    WithMemory(ai.MemoryStore(), "session:user123")

// Conversation persists across calls:
agent.Prompt(ctx, "My name is Alice")
agent.Prompt(ctx, "What's my name?") // "Your name is Alice"
```

---

## Tools (Function Calling)

Register Go functions that agents can call:

```go
type WeatherInput struct {
    City string `description:"City name"`
}
type WeatherOutput struct {
    Temp    int
    Summary string
}

ai.RegisterTool(ai.Tool{
    Name:        "weather",
    Description: "Get current weather for a city",
    Run: func(ctx context.Context, in WeatherInput) (WeatherOutput, error) {
        return WeatherOutput{Temp: 72, Summary: "Sunny"}, nil
    },
})
```

Fluent builder:

```go
ai.NewTool("calculator").
    Desc("Evaluate a math expression").
    Handler(func(ctx context.Context, in CalcInput) (CalcOutput, error) { ... }).
    Register()
```

---

## Embeddings

```go
vector, err := ai.Embed(ctx, "hello world")

// Batch
resp, err := ai.EmbedBatch(ctx, []string{"hello", "world"})

// Similarity
score := ai.CosineSimilarity(vecA, vecB)
```

---

## Vector Store

```go
store := ai.VectorStoreInstance("knowledge")

store.Add(ctx, "doc1", "How Go channels work")
store.Add(ctx, "doc2", "Go concurrency patterns")

results, err := store.Search(ctx, "goroutines", 5)
for _, doc := range results {
    fmt.Printf("[%.2f] %s: %s\n", doc.Score, doc.ID, doc.Text)
}
```

Backends: in-memory (built-in), pgvector, Qdrant, Redis (bring your own `VectorStoreBackend`).

---

## RAG (Retrieval-Augmented Generation)

```go
rag := ai.NewRAG(store).
    TopK(5).
    MinScore(0.7).
    WithCitations(true)

answer, err := rag.Ask(ctx, "Explain Go concurrency")
fmt.Println(answer.Answer)
fmt.Println(answer.Sources) // source documents
```

Streaming RAG:

```go
stream, sources, err := rag.AskStream(ctx, "How do goroutines work?")
```

---

## Prompt Templates

```go
prompt := ai.Template("Summarize the following:\n\n{{.text}}")
resp, err := prompt.Generate(ctx, map[string]any{"text": article})
```

Few-shot prompting:

```go
classifier := ai.FewShot("Classify the sentiment of the text.").
    Add("I love this!", "positive").
    Add("This is terrible.", "negative")

resp, err := classifier.Generate(ctx, "The product is okay I guess.")
```

Composable chains:

```go
resp, err := ai.Chain(
    ai.SystemTemplate("You are a {{.role}} expert."),
    ai.Template("Explain {{.topic}}."),
).Generate(ctx, map[string]any{"role": "Go", "topic": "channels"})
```

---

## Workflows (Multi-Step Pipelines)

```go
wf := ai.NewWorkflow("content").
    Step("outline", generateOutline).
    Step("draft", writeDraft).
    Parallel("media",
        ai.StepFunc("images", genImages),
        ai.StepFunc("audio", genAudio),
    ).
    Step("final", finalize)

result, err := wf.Run(ctx, ai.WorkflowInput{"topic": "Go concurrency"})
```

Conditional branching:

```go
wf.Branch("route", func(wc *ai.WorkflowContext) string {
    if wc.GetString("quality") == "low" { return "rewrite" }
    return "publish"
}, map[string]ai.StepHandler{
    "rewrite": rewriteStep,
    "publish": publishStep,
})
```

---

## Image Generation

```go
img, err := ai.Image().
    Model("dall-e-3").
    Prompt("cyberpunk city at night").
    Size("1024x1024").
    Generate(ctx)
```

---

## Video Generation

```go
video, err := ai.Video().
    Model("kling-2.5").
    Prompt("camera pans over mountains").
    Duration(5).
    Generate(ctx)
```

---

## Document Processing

```go
// Summarize (auto map-reduce for long texts)
summary, err := ai.Summarize(ctx, longText, ai.SummarizeStyle("bullet-points"))

// Chunk text for ingestion
chunks := ai.ChunkText(text, ai.ChunkSize(500), ai.ChunkOverlap(50))

// Batch ingest into vector store
ai.IngestDocuments(ctx, store, map[string]string{
    "doc1": text1,
    "doc2": text2,
})

// Q&A over a single document
qa := ai.DocumentQA(contractText)
answer, err := qa.Ask(ctx, "What are the payment terms?")
```

---

## Guardrails

```go
g := ai.NewGuardrails().
    MaxLength(5000).
    BlockPatterns(`(?i)password`, `\b\d{16}\b`).
    SetContentFilter(ai.FilterPII).
    CustomCheck(func(output string) error {
        if strings.Contains(output, "TODO") {
            return fmt.Errorf("output contains TODO")
        }
        return nil
    })

resp, err := ai.Generate(ctx, prompt)
if err := g.Validate(resp.Text); err != nil {
    // response violated guardrails
}
```

---

## Observability

```go
// Log every request
ai.OnRequest(func(e ai.RequestEvent) {
    log.Printf("model=%s tokens=%d latency=%s", e.Model, e.Usage.TotalTokens, e.Latency)
})

// Track errors
ai.OnError(func(e ai.RequestEvent) {
    sentry.CaptureException(e.Error)
})

// Get aggregate metrics
metrics := ai.GetMetrics()
fmt.Println(ai.UsageReport())
```

---

## Memory

| Backend | Constructor | Use Case |
|---|---|---|
| In-memory | `ai.MemoryStore()` | Dev/testing |
| Redis | `ai.RedisMemory(rdb)` | Production, multi-instance |
| Database | `ai.DatabaseMemory(db)` | Persistent, auditable |
| Sliding window | `ai.SlidingWindowMemory(inner, 50)` | Keep last N messages |
| Summary | `ai.SummaryMemory(inner, 100)` | Auto-compress old messages |

### Redis Memory

```go
import "github.com/redis/go-redis/v9"

rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
mem := ai.RedisMemory(rdb,
    ai.WithRedisPrefix("myapp:ai:"),
    ai.WithRedisTTL(24 * time.Hour),
)
agent := ai.NewAgent("You are a helpful assistant").
    WithMemory(mem, "session:user123")
```

### Database Memory (GORM)

```go
mem := ai.DatabaseMemory(db) // auto-creates ai_conversations table
agent := ai.NewAgent("You are a helpful assistant").
    WithMemory(mem, "session:user123")
```

---

## Vector Store Backends

| Backend | Constructor | Use Case |
|---|---|---|
| In-memory | `ai.NewMemoryVectorStore()` | Dev/testing |
| pgvector | `ai.NewPgvectorStore(db)` | PostgreSQL production |
| Qdrant | `ai.NewQdrantStore(url, collection)` | Dedicated vector DB |
| Pinecone | `ai.NewPineconeStore(host, apiKey)` | Managed cloud |

### pgvector (PostgreSQL)

```go
import "gorm.io/driver/postgres"

db, _ := gorm.Open(postgres.Open(dsn), &gorm.Config{})
backend := ai.NewPgvectorStore(db,
    ai.WithPgvectorTable("embeddings"),
    ai.WithPgvectorDimension(1536),
)
store := ai.VectorStoreInstance("knowledge", backend)
```

### Qdrant

```go
backend := ai.NewQdrantStore("http://localhost:6333", "documents",
    ai.WithQdrantDimension(1536),
)
store := ai.VectorStoreInstance("knowledge", backend)
```

### Pinecone

```go
backend := ai.NewPineconeStore(
    "https://my-index-xxx.svc.xxx.pinecone.io",
    os.Getenv("PINECONE_API_KEY"),
    ai.WithPineconeNamespace("production"),
)
store := ai.VectorStoreInstance("knowledge", backend)
```

---

## Tracing (OpenTelemetry)

```go
// Enable tracing — auto-instruments all AI operations
ai.EnableTracing(ai.TracingConfig{
    ServiceName:     "my-app",
    RecordPrompts:   false, // disable in prod for privacy
    RecordResponses: false,
})

// Add exporters
ai.AddSpanExporter(&ai.LogExporter{})          // dev: print to console
ai.AddSpanExporter(&ai.OTLPExporter{           // prod: send to collector
    Endpoint: "http://localhost:4318/v1/traces",
})

// Inspect recent spans
spans := ai.GetTraceSpans(10)

// Propagate trace context
ctx = ai.WithTraceContext(ctx, ai.TraceContext{TraceID: "abc123"})
```

---

## Cost Tracking

```go
// Enable cost tracking with budget alerts
ai.EnableCostTracking(ai.CostConfig{
    MonthlyBudget: 500.00,
    AlertThresholds: []float64{50, 75, 90, 100},
    OnBudgetAlert: func(usage ai.CostSummary) {
        log.Printf("⚠️ AI budget at %.0f%% ($%.2f / $%.2f)",
            usage.BudgetPercent, usage.TotalCost, usage.MonthlyBudget)
    },
})

// Get dashboard data (JSON-serializable)
dashboard := ai.GetCostDashboard()

// Per-model and per-provider breakdowns
for _, m := range dashboard.CostByModel {
    fmt.Printf("%s: %d reqs, $%.4f\n", m.Model, m.Requests, m.TotalCost)
}

// Formatted report
fmt.Println(ai.CostReport())

// Set custom pricing for fine-tuned models
ai.SetModelPricing("ft:gpt-4o-mini:my-org", ai.ModelPricing{
    PromptPer1K: 0.0003, CompletionPer1K: 0.0012,
})
```

---

## Model Evaluation & Benchmarking

```go
// Define a test suite
suite := ai.NewEvalSuite("qa-quality").
    AddCase("greeting", "Say hello in a friendly way",
        ai.ExpectContains("hello"),
        ai.ExpectMinLength(10),
    ).
    AddCase("math", "What is 15 * 23?",
        ai.ExpectContains("345"),
    ).
    AddCase("json_output", "Return a JSON object with name and age",
        ai.ExpectJSON(),
    ).
    AddCaseWithSystem("role", "You are a pirate", "Introduce yourself",
        ai.ExpectContains("arr"),
    )

// Run against default model
report := suite.Run(ctx)
fmt.Println(report.Summary())

// Compare multiple models
comparison := ai.CompareModels(ctx, suite,
    "gpt-4o", "gpt-4o-mini", "claude-3-5-sonnet-20241022",
)
fmt.Println(comparison.Summary())

// Use LLM-as-judge for subjective quality
suite.AddCase("creative", "Write a haiku about Go",
    ai.LLMJudge("creativity and adherence to haiku format"),
    ai.ExpectMinLength(10),
)

// Custom scoring function
suite.AddCase("format", "List 3 items",
    ai.CustomCheck("has_list", func(resp string) (float64, string) {
        lines := strings.Split(resp, "\n")
        if len(lines) >= 3 { return 1.0, "has 3+ lines" }
        return float64(len(lines)) / 3.0, fmt.Sprintf("only %d lines", len(lines))
    }),
)
```

---

## HTTP Middleware

```go
// Rate-limit AI endpoints
router.Use(ai.RateLimit(10, time.Second))

// Guard against cost overruns
router.Use(ai.CostGuard(100000)) // max 100K tokens per request

// Request logging
router.Use(ai.Logger())
```

---

## Architecture

```
plugins/ai/
├── types.go               # Core types: Message, Request/Response, StreamChunk, Usage
├── providers.go           # Provider interface, registry, factory pattern
├── adapter.go             # Legacy provider adapters
├── client_v2.go           # Client with observability, guardrails
├── config.go              # Configuration
├── plugin.go              # Nimbus plugin integration
│
├── agent_v2.go            # Agent runtime with tool loops, memory, streaming
├── memory.go              # In-memory, sliding window, summary memory
├── memory_redis.go        # Redis + GORM database memory backends
├── tool.go                # Tool system (registration, schema gen, execution)
│
├── extract.go             # Structured output: Extract[T], ExtractSlice[T], Classify
├── template.go            # Prompt templates, chains, few-shot
├── document.go            # Document processing: chunking, summarization, Q&A
│
├── embed.go               # Embedding facade
├── vectorstore.go         # Vector store abstraction + in-memory backend
├── vectorstore_pgvector.go # pgvector backend (PostgreSQL)
├── vectorstore_qdrant.go  # Qdrant backend
├── vectorstore_pinecone.go # Pinecone backend
├── rag.go                 # RAG engine
│
├── workflow.go            # Multi-step workflow engine
├── image.go               # Image generation builder
├── video.go               # Video generation builder
│
├── middleware.go          # HTTP middleware + observability hooks + metrics
├── guardrails.go          # Output validation guardrails
├── tracing.go             # OpenTelemetry tracing integration
├── cost.go                # Cost tracking dashboard
├── eval.go                # Model evaluation & benchmarking
│
├── openai.go              # OpenAI provider
├── anthropic.go           # Anthropic provider
├── gemini.go              # Gemini provider
├── mistral.go             # Mistral provider
├── cohere.go              # Cohere provider
├── ollama.go              # Ollama provider
└── xai.go                 # xAI provider (OpenAI-compatible)
```

---

## Extending with Custom Providers

```go
func init() {
    ai.RegisterProvider("my-provider", func(cfg *ai.Config) (ai.Provider, error) {
        return &myProvider{}, nil
    })
}

type myProvider struct{}

func (p *myProvider) Name() string { return "my-provider" }
func (p *myProvider) Generate(ctx context.Context, req *ai.GenerateRequest) (*ai.GenerateResponse, error) { ... }
func (p *myProvider) Stream(ctx context.Context, req *ai.GenerateRequest) (*ai.StreamResponse, error) { ... }

// Optional: implement ai.EmbeddingProvider, ai.ImageProvider, ai.VideoProvider
```

---

## Providers

| Provider | Generate | Stream | Embeddings | Image |
|----------|----------|--------|------------|-------|
| OpenAI | ✅ | ✅ | ✅ | ✅ |
| xAI (Grok) | ✅ | ✅ | — | — |
| Ollama | ✅ | ✅ | — | — |
| Anthropic | ✅ | ✅ | — | — |
| Gemini | ✅ | ✅ | — | — |
| Mistral | ✅ | ✅ | — | — |
| Cohere | ✅ | ✅ | — | — |

## Roadmap

- [x] Structured output (Extract)
- [x] Tool/function calling
- [x] Agents with reasoning loops
- [x] Embeddings & vector store
- [x] RAG engine
- [x] Prompt templates & chains
- [x] Multi-step workflows
- [x] Image & video generation
- [x] Observability & metrics
- [x] Guardrails
- [x] Agent memory
- [x] Document processing
- [x] MCP (Model Context Protocol) — see [plugins/mcp](../mcp/README.md)
- [x] Redis/database-backed memory
- [x] pgvector / Qdrant / Pinecone backends
- [x] OpenTelemetry tracing integration
- [x] Cost tracking dashboard
- [x] Model evaluation & benchmarking
- [ ] Multi-modal (vision, audio) support
- [ ] Fine-tuning management API
- [ ] A/B testing for prompts
- [ ] Prompt versioning & registry
