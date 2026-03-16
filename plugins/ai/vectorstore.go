/*
|--------------------------------------------------------------------------
| AI SDK — Vector Store Abstraction
|--------------------------------------------------------------------------
|
| A vector store persists document embeddings and supports
| similarity search. Pluggable backends include in-memory (dev),
| pgvector, Qdrant, and Redis.
|
| Usage:
|
|   store := ai.VectorStore("knowledge")
|   store.Add(ctx, "doc1", "How Go channels work")
|   results, err := store.Search(ctx, "goroutines", 5)
|
*/

package ai

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// ---------------------------------------------------------------------------
// VectorStore interface
// ---------------------------------------------------------------------------

// Document represents a stored vector with its metadata and text.
type Document struct {
	ID       string            `json:"id"`
	Text     string            `json:"text"`
	Vector   []float32         `json:"vector,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
	Score    float32           `json:"score,omitempty"` // populated on search results
}

// VectorStoreBackend is the interface for vector storage backends.
type VectorStoreBackend interface {
	// Add stores a document with its embedding vector.
	Add(ctx context.Context, doc Document) error

	// AddBatch stores multiple documents.
	AddBatch(ctx context.Context, docs []Document) error

	// Search finds the top-k most similar documents to the query vector.
	Search(ctx context.Context, vector []float32, topK int) ([]Document, error)

	// Delete removes a document by ID.
	Delete(ctx context.Context, id string) error

	// Count returns the total number of documents.
	Count(ctx context.Context) (int, error)
}

// ---------------------------------------------------------------------------
// VectorStore — high-level API
// ---------------------------------------------------------------------------

// Store wraps a backend and handles automatic embedding of text.
type Store struct {
	name    string
	backend VectorStoreBackend
}

// VectorStoreInstance creates a new Store for the given namespace.
// Uses the registered backend or defaults to in-memory.
func VectorStoreInstance(name string, backend ...VectorStoreBackend) *Store {
	var b VectorStoreBackend
	if len(backend) > 0 && backend[0] != nil {
		b = backend[0]
	} else {
		b = NewMemoryVectorStore()
	}
	return &Store{name: name, backend: b}
}

// Add embeds the text and stores it.
func (s *Store) Add(ctx context.Context, id, text string, metadata ...map[string]string) error {
	vector, err := Embed(ctx, text)
	if err != nil {
		return fmt.Errorf("ai: vector store %q: embed: %w", s.name, err)
	}
	doc := Document{
		ID:     id,
		Text:   text,
		Vector: vector,
	}
	if len(metadata) > 0 {
		doc.Metadata = metadata[0]
	}
	return s.backend.Add(ctx, doc)
}

// AddDocument stores a pre-embedded document.
func (s *Store) AddDocument(ctx context.Context, doc Document) error {
	return s.backend.Add(ctx, doc)
}

// AddBatch embeds and stores multiple texts.
func (s *Store) AddBatch(ctx context.Context, items []struct{ ID, Text string }) error {
	texts := make([]string, len(items))
	for i, item := range items {
		texts[i] = item.Text
	}

	resp, err := EmbedBatch(ctx, texts)
	if err != nil {
		return fmt.Errorf("ai: vector store %q: embed batch: %w", s.name, err)
	}

	docs := make([]Document, len(items))
	for i, item := range items {
		docs[i] = Document{
			ID:     item.ID,
			Text:   item.Text,
			Vector: resp.Embeddings[i],
		}
	}

	return s.backend.AddBatch(ctx, docs)
}

// Search embeds the query and finds the top-k most similar documents.
func (s *Store) Search(ctx context.Context, query string, topK int) ([]Document, error) {
	vector, err := Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("ai: vector store %q: embed query: %w", s.name, err)
	}
	return s.backend.Search(ctx, vector, topK)
}

// SearchVector finds similar documents using a pre-computed vector.
func (s *Store) SearchVector(ctx context.Context, vector []float32, topK int) ([]Document, error) {
	return s.backend.Search(ctx, vector, topK)
}

// Delete removes a document.
func (s *Store) Delete(ctx context.Context, id string) error {
	return s.backend.Delete(ctx, id)
}

// Count returns the number of stored documents.
func (s *Store) Count(ctx context.Context) (int, error) {
	return s.backend.Count(ctx)
}

// ---------------------------------------------------------------------------
// In-memory vector store (development / testing)
// ---------------------------------------------------------------------------

type memoryVectorStore struct {
	mu   sync.RWMutex
	docs map[string]Document
}

// NewMemoryVectorStore creates an in-memory vector store for
// development and testing. Not suitable for production.
func NewMemoryVectorStore() VectorStoreBackend {
	return &memoryVectorStore{docs: make(map[string]Document)}
}

func (m *memoryVectorStore) Add(_ context.Context, doc Document) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.docs[doc.ID] = doc
	return nil
}

func (m *memoryVectorStore) AddBatch(_ context.Context, docs []Document) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, doc := range docs {
		m.docs[doc.ID] = doc
	}
	return nil
}

func (m *memoryVectorStore) Search(_ context.Context, vector []float32, topK int) ([]Document, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	type scored struct {
		doc   Document
		score float32
	}
	var results []scored
	for _, doc := range m.docs {
		score := CosineSimilarity(vector, doc.Vector)
		results = append(results, scored{doc: doc, score: score})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	if topK > len(results) {
		topK = len(results)
	}

	docs := make([]Document, topK)
	for i := 0; i < topK; i++ {
		d := results[i].doc
		d.Score = results[i].score
		docs[i] = d
	}
	return docs, nil
}

func (m *memoryVectorStore) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.docs, id)
	return nil
}

func (m *memoryVectorStore) Count(_ context.Context) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.docs), nil
}
