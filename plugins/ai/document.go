/*
|--------------------------------------------------------------------------
| AI SDK — Document Processing
|--------------------------------------------------------------------------
|
| High-level primitives for processing documents with AI: chunking,
| summarization, Q&A over documents, and batch document ingestion
| into vector stores.
|
| Usage:
|
|   // Summarize
|   summary, err := ai.Summarize(ctx, longText)
|
|   // Chunk and ingest into a vector store
|   docs := ai.ChunkText(text, ai.ChunkSize(500), ai.ChunkOverlap(50))
|   for i, chunk := range docs {
|       store.Add(ctx, fmt.Sprintf("doc-%d", i), chunk)
|   }
|
|   // Q&A over a document
|   qa := ai.DocumentQA(longText)
|   answer, err := qa.Ask(ctx, "What is the main argument?")
|
*/

package ai

import (
	"context"
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// Text chunking
// ---------------------------------------------------------------------------

// ChunkOption configures text chunking.
type ChunkOption func(*chunkConfig)

type chunkConfig struct {
	size    int
	overlap int
	sep     string
}

// ChunkSize sets the target chunk size in characters.
func ChunkSize(n int) ChunkOption {
	return func(c *chunkConfig) { c.size = n }
}

// ChunkOverlap sets the overlap between consecutive chunks.
func ChunkOverlap(n int) ChunkOption {
	return func(c *chunkConfig) { c.overlap = n }
}

// ChunkSeparator sets the preferred split point (e.g. "\n\n" for paragraphs).
func ChunkSeparator(sep string) ChunkOption {
	return func(c *chunkConfig) { c.sep = sep }
}

// ChunkText splits text into chunks of roughly equal size.
func ChunkText(text string, opts ...ChunkOption) []string {
	cfg := &chunkConfig{
		size:    1000,
		overlap: 100,
		sep:     "\n\n",
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// If the text is short, return as-is.
	if len(text) <= cfg.size {
		return []string{text}
	}

	// Split by separator first, then merge into chunks.
	parts := strings.Split(text, cfg.sep)
	var chunks []string
	var current strings.Builder

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		if current.Len()+len(part)+len(cfg.sep) > cfg.size && current.Len() > 0 {
			chunks = append(chunks, current.String())

			// Handle overlap: keep the tail of the current chunk.
			tail := current.String()
			current.Reset()
			if cfg.overlap > 0 && len(tail) > cfg.overlap {
				current.WriteString(tail[len(tail)-cfg.overlap:])
			}
		}

		if current.Len() > 0 {
			current.WriteString(cfg.sep)
		}
		current.WriteString(part)
	}

	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}

	return chunks
}

// ---------------------------------------------------------------------------
// Summarize
// ---------------------------------------------------------------------------

// SummarizeOption configures summarization.
type SummarizeOption func(*summarizeConfig)

type summarizeConfig struct {
	model  string
	maxLen int
	style  string // "concise", "detailed", "bullet-points"
}

// SummarizeModel sets the model.
func SummarizeModel(m string) SummarizeOption {
	return func(c *summarizeConfig) { c.model = m }
}

// SummarizeMaxLength limits the summary length.
func SummarizeMaxLength(n int) SummarizeOption {
	return func(c *summarizeConfig) { c.maxLen = n }
}

// SummarizeStyle sets the summary style.
func SummarizeStyle(s string) SummarizeOption {
	return func(c *summarizeConfig) { c.style = s }
}

// Summarize generates a summary of the given text. For long texts,
// it uses a map-reduce approach: chunk → summarize each → combine.
func Summarize(ctx context.Context, text string, opts ...SummarizeOption) (string, error) {
	cfg := &summarizeConfig{style: "concise"}
	for _, opt := range opts {
		opt(cfg)
	}

	// For short texts, summarize directly.
	if len(text) < 8000 {
		return summarizeDirect(ctx, text, cfg)
	}

	// Map-reduce: chunk → summarize each → combine.
	chunks := ChunkText(text, ChunkSize(4000), ChunkOverlap(200))
	var summaries []string
	for _, chunk := range chunks {
		summary, err := summarizeDirect(ctx, chunk, cfg)
		if err != nil {
			return "", err
		}
		summaries = append(summaries, summary)
	}

	// Combine summaries.
	combined := strings.Join(summaries, "\n\n")
	if len(combined) < 4000 {
		return summarizeDirect(ctx, combined, &summarizeConfig{
			model: cfg.model,
			style: cfg.style,
		})
	}

	return combined, nil
}

func summarizeDirect(ctx context.Context, text string, cfg *summarizeConfig) (string, error) {
	style := cfg.style
	if style == "" {
		style = "concise"
	}

	prompt := fmt.Sprintf(
		"Summarize the following text in a %s manner:\n\n%s",
		style, text,
	)

	genOpts := []GenerateOption{
		WithSystem("You are a summarization assistant. Provide clear, accurate summaries."),
	}
	if cfg.model != "" {
		genOpts = append(genOpts, WithModel(cfg.model))
	}
	if cfg.maxLen > 0 {
		genOpts = append(genOpts, WithMaxTokens(cfg.maxLen))
	}

	resp, err := GetClient().Generate(ctx, prompt, genOpts...)
	if err != nil {
		return "", fmt.Errorf("ai: summarize: %w", err)
	}
	return resp.Text, nil
}

// ---------------------------------------------------------------------------
// Document Q&A
// ---------------------------------------------------------------------------

// DocQA answers questions about a document without a vector store.
// Best for single-document Q&A where the document fits in context.
type DocQA struct {
	text   string
	model  string
	system string
}

// DocumentQA creates a Q&A interface over a document.
func DocumentQA(text string) *DocQA {
	return &DocQA{
		text:   text,
		system: "You are a document analysis assistant. Answer questions based only on the provided document. If the answer is not in the document, say so.",
	}
}

// Model sets the model for Q&A.
func (d *DocQA) Model(m string) *DocQA {
	d.model = m
	return d
}

// SystemPrompt overrides the default system prompt.
func (d *DocQA) SystemPrompt(s string) *DocQA {
	d.system = s
	return d
}

// Ask answers a question about the document.
func (d *DocQA) Ask(ctx context.Context, question string) (string, error) {
	prompt := fmt.Sprintf("Document:\n%s\n\nQuestion: %s", d.text, question)

	opts := []GenerateOption{WithSystem(d.system)}
	if d.model != "" {
		opts = append(opts, WithModel(d.model))
	}

	resp, err := GetClient().Generate(ctx, prompt, opts...)
	if err != nil {
		return "", fmt.Errorf("ai: document-qa: %w", err)
	}
	return resp.Text, nil
}

// ---------------------------------------------------------------------------
// Batch document ingestion
// ---------------------------------------------------------------------------

// IngestDocuments chunks and embeds a collection of texts into a vector store.
func IngestDocuments(ctx context.Context, store *Store, documents map[string]string, opts ...ChunkOption) error {
	for id, text := range documents {
		chunks := ChunkText(text, opts...)
		for i, chunk := range chunks {
			chunkID := fmt.Sprintf("%s#%d", id, i)
			if err := store.Add(ctx, chunkID, chunk, map[string]string{
				"source": id,
				"chunk":  fmt.Sprintf("%d", i),
			}); err != nil {
				return fmt.Errorf("ai: ingest %s chunk %d: %w", id, i, err)
			}
		}
	}
	return nil
}
