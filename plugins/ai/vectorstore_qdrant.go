/*
|--------------------------------------------------------------------------
| AI SDK — Qdrant Vector Store Backend
|--------------------------------------------------------------------------
|
| Uses the Qdrant REST API for scalable vector similarity search.
| Qdrant is an open-source vector database (https://qdrant.tech).
|
| Usage:
|
|   backend := ai.NewQdrantStore("http://localhost:6333", "my_collection")
|   store := ai.VectorStoreInstance("knowledge", backend)
|
*/

package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ---------------------------------------------------------------------------
// Qdrant backend
// ---------------------------------------------------------------------------

type qdrantStore struct {
	baseURL    string
	collection string
	apiKey     string
	dimension  int
	client     *http.Client
	ensured    bool
}

// QdrantOption configures the Qdrant backend.
type QdrantOption func(*qdrantStore)

// WithQdrantAPIKey sets the Qdrant API key for cloud deployments.
func WithQdrantAPIKey(key string) QdrantOption {
	return func(s *qdrantStore) { s.apiKey = key }
}

// WithQdrantDimension sets the vector dimension (default: 1536).
func WithQdrantDimension(d int) QdrantOption {
	return func(s *qdrantStore) { s.dimension = d }
}

// NewQdrantStore creates a Qdrant-backed VectorStoreBackend.
//
//	backend := ai.NewQdrantStore("http://localhost:6333", "documents")
//	store := ai.VectorStoreInstance("knowledge", backend)
func NewQdrantStore(baseURL, collection string, opts ...QdrantOption) VectorStoreBackend {
	s := &qdrantStore{
		baseURL:    baseURL,
		collection: collection,
		dimension:  1536,
		client:     &http.Client{Timeout: 30 * time.Second},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *qdrantStore) ensureCollection(ctx context.Context) error {
	if s.ensured {
		return nil
	}
	s.ensured = true

	// Check if collection exists.
	url := fmt.Sprintf("%s/collections/%s", s.baseURL, s.collection)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	s.setHeaders(req)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("ai: qdrant check collection: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode == 200 {
		return nil // exists
	}

	// Create collection.
	body := map[string]any{
		"vectors": map[string]any{
			"size":     s.dimension,
			"distance": "Cosine",
		},
	}
	data, _ := json.Marshal(body)
	url = fmt.Sprintf("%s/collections/%s", s.baseURL, s.collection)
	req, _ = http.NewRequestWithContext(ctx, "PUT", url, bytes.NewReader(data))
	s.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err = s.client.Do(req)
	if err != nil {
		return fmt.Errorf("ai: qdrant create collection: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("ai: qdrant create collection: status %d", resp.StatusCode)
	}
	return nil
}

func (s *qdrantStore) setHeaders(req *http.Request) {
	if s.apiKey != "" {
		req.Header.Set("api-key", s.apiKey)
	}
}

func (s *qdrantStore) Add(ctx context.Context, doc Document) error {
	return s.AddBatch(ctx, []Document{doc})
}

func (s *qdrantStore) AddBatch(ctx context.Context, docs []Document) error {
	if err := s.ensureCollection(ctx); err != nil {
		return err
	}

	type point struct {
		ID      string            `json:"id"`
		Vector  []float32         `json:"vector"`
		Payload map[string]string `json:"payload"`
	}

	points := make([]point, len(docs))
	for i, doc := range docs {
		payload := make(map[string]string)
		payload["text"] = doc.Text
		for k, v := range doc.Metadata {
			payload[k] = v
		}
		points[i] = point{
			ID:      doc.ID,
			Vector:  doc.Vector,
			Payload: payload,
		}
	}

	body := map[string]any{"points": points}
	data, _ := json.Marshal(body)

	url := fmt.Sprintf("%s/collections/%s/points", s.baseURL, s.collection)
	req, _ := http.NewRequestWithContext(ctx, "PUT", url, bytes.NewReader(data))
	s.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("ai: qdrant add: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ai: qdrant add: status %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func (s *qdrantStore) Search(ctx context.Context, vector []float32, topK int) ([]Document, error) {
	if err := s.ensureCollection(ctx); err != nil {
		return nil, err
	}

	body := map[string]any{
		"vector":       vector,
		"limit":        topK,
		"with_payload": true,
	}
	data, _ := json.Marshal(body)

	url := fmt.Sprintf("%s/collections/%s/points/search", s.baseURL, s.collection)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	s.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ai: qdrant search: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ai: qdrant search: status %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Result []struct {
			ID      string            `json:"id"`
			Score   float32           `json:"score"`
			Payload map[string]string `json:"payload"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ai: qdrant search decode: %w", err)
	}

	docs := make([]Document, len(result.Result))
	for i, r := range result.Result {
		text := r.Payload["text"]
		meta := make(map[string]string)
		for k, v := range r.Payload {
			if k != "text" {
				meta[k] = v
			}
		}
		docs[i] = Document{
			ID:       r.ID,
			Text:     text,
			Score:    r.Score,
			Metadata: meta,
		}
	}
	return docs, nil
}

func (s *qdrantStore) Delete(ctx context.Context, id string) error {
	if err := s.ensureCollection(ctx); err != nil {
		return err
	}

	body := map[string]any{
		"points": []string{id},
	}
	data, _ := json.Marshal(body)

	url := fmt.Sprintf("%s/collections/%s/points/delete", s.baseURL, s.collection)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	s.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("ai: qdrant delete: %w", err)
	}
	resp.Body.Close()
	return nil
}

func (s *qdrantStore) Count(ctx context.Context) (int, error) {
	if err := s.ensureCollection(ctx); err != nil {
		return 0, err
	}

	url := fmt.Sprintf("%s/collections/%s", s.baseURL, s.collection)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	s.setHeaders(req)

	resp, err := s.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("ai: qdrant count: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Result struct {
			PointsCount int `json:"points_count"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("ai: qdrant count decode: %w", err)
	}
	return result.Result.PointsCount, nil
}
