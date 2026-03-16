/*
|--------------------------------------------------------------------------
| AI SDK — RAG (Retrieval-Augmented Generation) Engine
|--------------------------------------------------------------------------
|
| Combines vector search with text generation to answer questions
| grounded in a knowledge base. The pipeline is:
|
|   1. Embed the query
|   2. Vector search for relevant documents
|   3. Inject retrieved context into the prompt
|   4. Generate an answer
|
| Usage:
|
|   rag := ai.NewRAG(store)      // store is a *ai.Store
|   answer, err := rag.Ask(ctx, "How do Go channels work?")
|
|   // Advanced:
|   rag := ai.NewRAG(store).
|       TopK(10).
|       MinScore(0.7).
|       SystemPrompt("You are a Go documentation assistant.").
|       WithCitations(true)
|   answer, err := rag.Ask(ctx, "Explain goroutines")
|
*/

package ai

import (
	"context"
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// RAG engine
// ---------------------------------------------------------------------------

// RAG is a retrieval-augmented generation engine.
type RAG struct {
	store       *Store
	topK        int
	minScore    float32
	system      string
	model       string
	citations   bool
	preprocess  func(query string) string
	postprocess func(answer string, docs []Document) string
	client      *Client
}

// NewRAG creates a RAG engine backed by the given vector store.
func NewRAG(store *Store) *RAG {
	return &RAG{
		store:  store,
		topK:   5,
		system: "You are a helpful assistant. Answer the question based on the provided context. If the context doesn't contain enough information, say so.",
	}
}

// TopK sets the number of documents to retrieve.
func (r *RAG) TopK(k int) *RAG {
	r.topK = k
	return r
}

// MinScore sets the minimum similarity score threshold.
func (r *RAG) MinScore(score float32) *RAG {
	r.minScore = score
	return r
}

// SystemPrompt overrides the default system prompt.
func (r *RAG) SystemPrompt(s string) *RAG {
	r.system = s
	return r
}

// Model overrides the default model.
func (r *RAG) Model(m string) *RAG {
	r.model = m
	return r
}

// WithCitations enables source citation in the response.
func (r *RAG) WithCitations(enabled bool) *RAG {
	r.citations = enabled
	return r
}

// WithClient overrides the default global AI client.
func (r *RAG) WithClient(c *Client) *RAG {
	r.client = c
	return r
}

// OnPreprocess sets a function to transform the query before embedding.
func (r *RAG) OnPreprocess(fn func(string) string) *RAG {
	r.preprocess = fn
	return r
}

// OnPostprocess sets a function to transform the answer after generation.
func (r *RAG) OnPostprocess(fn func(string, []Document) string) *RAG {
	r.postprocess = fn
	return r
}

// ---------------------------------------------------------------------------
// Ask — the main RAG pipeline
// ---------------------------------------------------------------------------

// RAGResponse wraps the generated answer with source documents.
type RAGResponse struct {
	Answer  string     `json:"answer"`
	Sources []Document `json:"sources"`
	Usage   *Usage     `json:"usage,omitempty"`
}

// Ask runs the full RAG pipeline: embed → search → augment → generate.
func (r *RAG) Ask(ctx context.Context, question string, opts ...GenerateOption) (*RAGResponse, error) {
	query := question
	if r.preprocess != nil {
		query = r.preprocess(query)
	}

	// Step 1–2: Vector search.
	docs, err := r.store.Search(ctx, query, r.topK)
	if err != nil {
		return nil, fmt.Errorf("ai: rag: search: %w", err)
	}

	// Filter by minimum score.
	if r.minScore > 0 {
		filtered := docs[:0]
		for _, d := range docs {
			if d.Score >= r.minScore {
				filtered = append(filtered, d)
			}
		}
		docs = filtered
	}

	// Step 3: Build augmented prompt.
	prompt := r.buildPrompt(question, docs)

	// Step 4: Generate answer.
	client := r.client
	if client == nil {
		client = GetClient()
	}

	genOpts := []GenerateOption{WithSystem(r.system)}
	if r.model != "" {
		genOpts = append(genOpts, WithModel(r.model))
	}
	genOpts = append(genOpts, opts...)

	resp, err := client.Generate(ctx, prompt, genOpts...)
	if err != nil {
		return nil, fmt.Errorf("ai: rag: generate: %w", err)
	}

	answer := resp.Text
	if r.postprocess != nil {
		answer = r.postprocess(answer, docs)
	}

	return &RAGResponse{
		Answer:  answer,
		Sources: docs,
		Usage:   resp.Usage,
	}, nil
}

// AskStream runs the RAG pipeline but streams the final answer.
func (r *RAG) AskStream(ctx context.Context, question string, opts ...GenerateOption) (*StreamResponse, []Document, error) {
	query := question
	if r.preprocess != nil {
		query = r.preprocess(query)
	}

	docs, err := r.store.Search(ctx, query, r.topK)
	if err != nil {
		return nil, nil, fmt.Errorf("ai: rag: search: %w", err)
	}

	if r.minScore > 0 {
		filtered := docs[:0]
		for _, d := range docs {
			if d.Score >= r.minScore {
				filtered = append(filtered, d)
			}
		}
		docs = filtered
	}

	prompt := r.buildPrompt(question, docs)

	client := r.client
	if client == nil {
		client = GetClient()
	}

	genOpts := []GenerateOption{WithSystem(r.system)}
	if r.model != "" {
		genOpts = append(genOpts, WithModel(r.model))
	}
	genOpts = append(genOpts, opts...)

	sr, err := client.StreamRequest(ctx, &GenerateRequest{
		Messages:  []Message{{Role: RoleUser, Content: prompt}},
		Model:     r.model,
		MaxTokens: 4096,
		Stream:    true,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("ai: rag: stream: %w", err)
	}
	return sr, docs, nil
}

// ---------------------------------------------------------------------------
// Prompt building
// ---------------------------------------------------------------------------

func (r *RAG) buildPrompt(question string, docs []Document) string {
	var b strings.Builder
	b.WriteString("Answer the following question based on the provided context.\n\n")

	if len(docs) > 0 {
		b.WriteString("Context:\n")
		for i, doc := range docs {
			b.WriteString(fmt.Sprintf("[%d] %s", i+1, doc.Text))
			if doc.ID != "" {
				b.WriteString(fmt.Sprintf(" (source: %s)", doc.ID))
			}
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	} else {
		b.WriteString("No relevant context was found.\n\n")
	}

	if r.citations {
		b.WriteString("Include source references [1], [2], etc. in your answer.\n\n")
	}

	b.WriteString("Question: ")
	b.WriteString(question)

	return b.String()
}
