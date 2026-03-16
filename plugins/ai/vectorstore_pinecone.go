/*
|--------------------------------------------------------------------------
| AI SDK — Pinecone Vector Store Backend
|--------------------------------------------------------------------------
|
| Uses the Pinecone REST API for managed vector similarity search.
| Pinecone is a fully managed vector database (https://pinecone.io).
|
| Usage:
|
|   backend := ai.NewPineconeStore("https://index-xxx.svc.xxx.pinecone.io", "your-api-key")
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
// Pinecone backend
// ---------------------------------------------------------------------------

type pineconeStore struct {
	host      string
	apiKey    string
	namespace string
	client    *http.Client
}

// PineconeOption configures the Pinecone backend.
type PineconeOption func(*pineconeStore)

// WithPineconeNamespace sets the Pinecone namespace for multi-tenancy.
func WithPineconeNamespace(ns string) PineconeOption {
	return func(s *pineconeStore) { s.namespace = ns }
}

// NewPineconeStore creates a Pinecone-backed VectorStoreBackend.
//
//	backend := ai.NewPineconeStore(
//	    "https://my-index-xxx.svc.xxx.pinecone.io",
//	    "your-api-key",
//	)
//	store := ai.VectorStoreInstance("knowledge", backend)
func NewPineconeStore(host, apiKey string, opts ...PineconeOption) VectorStoreBackend {
	s := &pineconeStore{
		host:   host,
		apiKey: apiKey,
		client: &http.Client{Timeout: 30 * time.Second},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *pineconeStore) setHeaders(req *http.Request) {
	req.Header.Set("Api-Key", s.apiKey)
	req.Header.Set("Content-Type", "application/json")
}

func (s *pineconeStore) Add(ctx context.Context, doc Document) error {
	return s.AddBatch(ctx, []Document{doc})
}

func (s *pineconeStore) AddBatch(ctx context.Context, docs []Document) error {
	type pineconeVector struct {
		ID       string            `json:"id"`
		Values   []float32         `json:"values"`
		Metadata map[string]string `json:"metadata,omitempty"`
	}

	vectors := make([]pineconeVector, len(docs))
	for i, doc := range docs {
		meta := make(map[string]string)
		meta["text"] = doc.Text
		for k, v := range doc.Metadata {
			meta[k] = v
		}
		vectors[i] = pineconeVector{
			ID:       doc.ID,
			Values:   doc.Vector,
			Metadata: meta,
		}
	}

	body := map[string]any{"vectors": vectors}
	if s.namespace != "" {
		body["namespace"] = s.namespace
	}
	data, _ := json.Marshal(body)

	url := fmt.Sprintf("%s/vectors/upsert", s.host)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	s.setHeaders(req)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("ai: pinecone upsert: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ai: pinecone upsert: status %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func (s *pineconeStore) Search(ctx context.Context, vector []float32, topK int) ([]Document, error) {
	body := map[string]any{
		"vector":          vector,
		"topK":            topK,
		"includeMetadata": true,
	}
	if s.namespace != "" {
		body["namespace"] = s.namespace
	}
	data, _ := json.Marshal(body)

	url := fmt.Sprintf("%s/query", s.host)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	s.setHeaders(req)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ai: pinecone query: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ai: pinecone query: status %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Matches []struct {
			ID       string            `json:"id"`
			Score    float32           `json:"score"`
			Metadata map[string]string `json:"metadata"`
		} `json:"matches"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ai: pinecone query decode: %w", err)
	}

	docs := make([]Document, len(result.Matches))
	for i, m := range result.Matches {
		text := m.Metadata["text"]
		meta := make(map[string]string)
		for k, v := range m.Metadata {
			if k != "text" {
				meta[k] = v
			}
		}
		docs[i] = Document{
			ID:       m.ID,
			Text:     text,
			Score:    m.Score,
			Metadata: meta,
		}
	}
	return docs, nil
}

func (s *pineconeStore) Delete(ctx context.Context, id string) error {
	body := map[string]any{
		"ids": []string{id},
	}
	if s.namespace != "" {
		body["namespace"] = s.namespace
	}
	data, _ := json.Marshal(body)

	url := fmt.Sprintf("%s/vectors/delete", s.host)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	s.setHeaders(req)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("ai: pinecone delete: %w", err)
	}
	resp.Body.Close()
	return nil
}

func (s *pineconeStore) Count(ctx context.Context) (int, error) {
	body := map[string]any{}
	if s.namespace != "" {
		body["namespace"] = s.namespace
	}
	data, _ := json.Marshal(body)

	url := fmt.Sprintf("%s/describe_index_stats", s.host)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	s.setHeaders(req)

	resp, err := s.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("ai: pinecone count: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		TotalVectorCount int `json:"totalVectorCount"`
		Namespaces       map[string]struct {
			VectorCount int `json:"vectorCount"`
		} `json:"namespaces"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("ai: pinecone count decode: %w", err)
	}

	if s.namespace != "" {
		if ns, ok := result.Namespaces[s.namespace]; ok {
			return ns.VectorCount, nil
		}
		return 0, nil
	}
	return result.TotalVectorCount, nil
}
