package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// MeilisearchEngine uses Meilisearch as the search backend.
type MeilisearchEngine struct {
	host   string
	apiKey string
	client *http.Client
}

// MeilisearchConfig holds Meilisearch connection settings.
type MeilisearchConfig struct {
	Host   string // e.g. "http://localhost:7700"
	APIKey string // Master or search API key
}

// NewMeilisearchEngine creates a Meilisearch-backed search engine.
func NewMeilisearchEngine(cfg MeilisearchConfig) *MeilisearchEngine {
	return &MeilisearchEngine{
		host:   cfg.Host,
		apiKey: cfg.APIKey,
		client: &http.Client{},
	}
}

func (e *MeilisearchEngine) doRequest(ctx context.Context, method, path string, body any) ([]byte, int, error) {
	var bodyReader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, e.host+path, bodyReader)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}

	return respBody, resp.StatusCode, nil
}

// Index adds or updates a document in Meilisearch.
func (e *MeilisearchEngine) Index(ctx context.Context, index string, id string, data map[string]any) error {
	doc := make(map[string]any)
	for k, v := range data {
		doc[k] = v
	}
	doc["id"] = id

	_, status, err := e.doRequest(ctx, "POST", "/indexes/"+url.PathEscape(index)+"/documents", []any{doc})
	if err != nil {
		return fmt.Errorf("search/meilisearch: index failed: %w", err)
	}
	if status >= 400 {
		return fmt.Errorf("search/meilisearch: index returned status %d", status)
	}
	return nil
}

// Delete removes a document from Meilisearch.
func (e *MeilisearchEngine) Delete(ctx context.Context, index string, id string) error {
	_, status, err := e.doRequest(ctx, "DELETE", "/indexes/"+url.PathEscape(index)+"/documents/"+url.PathEscape(id), nil)
	if err != nil {
		return fmt.Errorf("search/meilisearch: delete failed: %w", err)
	}
	if status >= 400 {
		return fmt.Errorf("search/meilisearch: delete returned status %d", status)
	}
	return nil
}

// Search performs a search query against Meilisearch.
func (e *MeilisearchEngine) Search(ctx context.Context, index string, query string, opts *Options) (*Results, error) {
	if opts == nil {
		opts = DefaultOptions()
	}
	if opts.Page < 1 {
		opts.Page = 1
	}
	if opts.PerPage < 1 {
		opts.PerPage = 20
	}

	searchBody := map[string]any{
		"q":      query,
		"limit":  opts.PerPage,
		"offset": (opts.Page - 1) * opts.PerPage,
	}

	if len(opts.Filters) > 0 {
		var filters []string
		for k, v := range opts.Filters {
			filters = append(filters, fmt.Sprintf("%s = %v", k, v))
		}
		searchBody["filter"] = filters
	}

	if opts.Sort != "" {
		searchBody["sort"] = []string{opts.Sort}
	}

	respBody, status, err := e.doRequest(ctx, "POST", "/indexes/"+url.PathEscape(index)+"/search", searchBody)
	if err != nil {
		return nil, fmt.Errorf("search/meilisearch: search failed: %w", err)
	}
	if status >= 400 {
		return nil, fmt.Errorf("search/meilisearch: search returned status %d: %s", status, string(respBody))
	}

	var resp struct {
		Hits               []map[string]any `json:"hits"`
		EstimatedTotalHits int64            `json:"estimatedTotalHits"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("search/meilisearch: failed to decode response: %w", err)
	}

	results := &Results{
		Total:   resp.EstimatedTotalHits,
		Page:    opts.Page,
		PerPage: opts.PerPage,
		Query:   query,
	}

	for _, hit := range resp.Hits {
		id, _ := hit["id"].(string)
		if id == "" {
			// Try numeric ID
			if numID, ok := hit["id"].(float64); ok {
				id = strconv.FormatFloat(numID, 'f', 0, 64)
			}
		}
		results.Hits = append(results.Hits, Result{
			ID:   id,
			Data: hit,
		})
	}

	return results, nil
}

// Flush removes all documents from an index in Meilisearch.
func (e *MeilisearchEngine) Flush(ctx context.Context, index string) error {
	_, status, err := e.doRequest(ctx, "DELETE", "/indexes/"+url.PathEscape(index)+"/documents", nil)
	if err != nil {
		return fmt.Errorf("search/meilisearch: flush failed: %w", err)
	}
	if status >= 400 {
		return fmt.Errorf("search/meilisearch: flush returned status %d", status)
	}
	return nil
}
