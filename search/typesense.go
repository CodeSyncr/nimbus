package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
)

// TypesenseEngine uses Typesense as the search backend.
type TypesenseEngine struct {
	host   string
	apiKey string
	client *http.Client
}

// TypesenseConfig holds Typesense connection settings.
type TypesenseConfig struct {
	Host   string // e.g. "http://localhost:8108"
	APIKey string
}

// NewTypesenseEngine creates a Typesense-backed search engine.
func NewTypesenseEngine(cfg TypesenseConfig) *TypesenseEngine {
	return &TypesenseEngine{
		host:   cfg.Host,
		apiKey: cfg.APIKey,
		client: &http.Client{},
	}
}

func (e *TypesenseEngine) doRequest(ctx context.Context, method, path string, body any) ([]byte, int, error) {
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
	req.Header.Set("X-TYPESENSE-API-KEY", e.apiKey)

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	return respBody, resp.StatusCode, err
}

// Index adds or updates a document in Typesense.
func (e *TypesenseEngine) Index(ctx context.Context, index string, id string, data map[string]any) error {
	doc := make(map[string]any)
	for k, v := range data {
		doc[k] = v
	}
	doc["id"] = id

	_, status, err := e.doRequest(ctx, "POST", "/collections/"+index+"/documents?action=upsert", doc)
	if err != nil {
		return fmt.Errorf("search/typesense: index failed: %w", err)
	}
	if status >= 400 {
		return fmt.Errorf("search/typesense: index returned status %d", status)
	}
	return nil
}

// Delete removes a document from Typesense.
func (e *TypesenseEngine) Delete(ctx context.Context, index string, id string) error {
	_, status, err := e.doRequest(ctx, "DELETE", "/collections/"+index+"/documents/"+id, nil)
	if err != nil {
		return fmt.Errorf("search/typesense: delete failed: %w", err)
	}
	if status >= 400 {
		return fmt.Errorf("search/typesense: delete returned status %d", status)
	}
	return nil
}

// Search performs a search query against Typesense.
func (e *TypesenseEngine) Search(ctx context.Context, index string, query string, opts *Options) (*Results, error) {
	if opts == nil {
		opts = DefaultOptions()
	}
	if opts.Page < 1 {
		opts.Page = 1
	}
	if opts.PerPage < 1 {
		opts.PerPage = 20
	}

	params := fmt.Sprintf("/collections/%s/documents/search?q=%s&query_by=*&page=%d&per_page=%d",
		index, query, opts.Page, opts.PerPage)

	if opts.Sort != "" {
		params += "&sort_by=" + opts.Sort
	}

	respBody, status, err := e.doRequest(ctx, "GET", params, nil)
	if err != nil {
		return nil, fmt.Errorf("search/typesense: search failed: %w", err)
	}
	if status >= 400 {
		return nil, fmt.Errorf("search/typesense: search returned status %d: %s", status, string(respBody))
	}

	var resp struct {
		Found int64 `json:"found"`
		Hits  []struct {
			Document map[string]any `json:"document"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("search/typesense: failed to decode response: %w", err)
	}

	results := &Results{
		Total:   resp.Found,
		Page:    opts.Page,
		PerPage: opts.PerPage,
		Query:   query,
	}

	for _, hit := range resp.Hits {
		id, _ := hit.Document["id"].(string)
		if id == "" {
			if numID, ok := hit.Document["id"].(float64); ok {
				id = strconv.FormatFloat(numID, 'f', 0, 64)
			}
		}
		results.Hits = append(results.Hits, Result{
			ID:   id,
			Data: hit.Document,
		})
	}

	return results, nil
}

// Flush removes all documents from an index in Typesense (drops and recreates).
func (e *TypesenseEngine) Flush(ctx context.Context, index string) error {
	_, status, err := e.doRequest(ctx, "DELETE", "/collections/"+index, nil)
	if err != nil {
		return fmt.Errorf("search/typesense: flush failed: %w", err)
	}
	if status >= 400 {
		return fmt.Errorf("search/typesense: flush returned status %d", status)
	}
	return nil
}
