// Package search provides full-text search capabilities similar to Laravel
// Scout. It supports multiple drivers: PostgreSQL full-text search (built-in),
// Meilisearch, Typesense, and Algolia.
//
// Models implement the Searchable interface to enable indexing and searching.
//
// # Quick start
//
//	engine := search.NewPostgresEngine(db)
//	results, _ := engine.Search(ctx, "videos", "gopro 4k sunset", nil)
package search

import (
	"context"
	"fmt"
	"sync"
)

// ── Searchable interface ────────────────────────────────────────

// Searchable is implemented by models that should be indexed for search.
type Searchable interface {
	// SearchIndex returns the index/table name for search.
	SearchIndex() string
	// SearchID returns the unique identifier for this record.
	SearchID() string
	// SearchData returns the data to index (map of field→value).
	SearchData() map[string]any
}

// ── Search Result ───────────────────────────────────────────────

// Result represents a single search result.
type Result struct {
	ID    string         `json:"id"`
	Score float64        `json:"score"`
	Data  map[string]any `json:"data"`
}

// Results holds search results and metadata.
type Results struct {
	Hits    []Result `json:"hits"`
	Total   int64    `json:"total"`
	Page    int      `json:"page"`
	PerPage int      `json:"per_page"`
	Query   string   `json:"query"`
}

// ── Search Options ──────────────────────────────────────────────

// Options configures a search query.
type Options struct {
	// Page number (1-based).
	Page int
	// PerPage results per page.
	PerPage int
	// Filters are key=value filters applied before text search.
	Filters map[string]any
	// Sort column (e.g. "created_at", "-score" for descending).
	Sort string
}

// DefaultOptions returns sensible defaults.
func DefaultOptions() *Options {
	return &Options{
		Page:    1,
		PerPage: 20,
	}
}

// ── Engine interface ────────────────────────────────────────────

// Engine is the interface that all search drivers implement.
type Engine interface {
	// Index adds or updates a document in the search index.
	Index(ctx context.Context, index string, id string, data map[string]any) error
	// Delete removes a document from the search index.
	Delete(ctx context.Context, index string, id string) error
	// Search performs a full-text search query.
	Search(ctx context.Context, index string, query string, opts *Options) (*Results, error)
	// Flush removes all documents from an index.
	Flush(ctx context.Context, index string) error
}

// ── Engine Registry ─────────────────────────────────────────────

var (
	mu      sync.RWMutex
	engines = make(map[string]Engine)
	def     string
)

// Register adds a named engine.
func Register(name string, engine Engine) {
	mu.Lock()
	defer mu.Unlock()
	engines[name] = engine
	if def == "" {
		def = name
	}
}

// Use returns the named engine.
func Use(name string) (Engine, error) {
	mu.RLock()
	defer mu.RUnlock()
	e, ok := engines[name]
	if !ok {
		return nil, fmt.Errorf("search: engine %q not registered", name)
	}
	return e, nil
}

// Default returns the default engine.
func Default() Engine {
	mu.RLock()
	defer mu.RUnlock()
	return engines[def]
}

// ── Convenience functions ───────────────────────────────────────

// IndexRecord indexes a Searchable model using the default engine.
func IndexRecord(ctx context.Context, record Searchable) error {
	e := Default()
	if e == nil {
		return fmt.Errorf("search: no default engine registered")
	}
	return e.Index(ctx, record.SearchIndex(), record.SearchID(), record.SearchData())
}

// DeleteRecord removes a Searchable model from the default engine.
func DeleteRecord(ctx context.Context, record Searchable) error {
	e := Default()
	if e == nil {
		return fmt.Errorf("search: no default engine registered")
	}
	return e.Delete(ctx, record.SearchIndex(), record.SearchID())
}

// Query performs a search using the default engine.
func Query(ctx context.Context, index, query string, opts *Options) (*Results, error) {
	e := Default()
	if e == nil {
		return nil, fmt.Errorf("search: no default engine registered")
	}
	return e.Search(ctx, index, query, opts)
}
