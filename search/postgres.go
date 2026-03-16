package search

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// PostgresEngine uses PostgreSQL's built-in tsvector full-text search.
// It stores searchable data in a `search_index` table with a GIN index.
type PostgresEngine struct {
	db *gorm.DB
}

// searchDocument is the model for the search_index table.
type searchDocument struct {
	ID        uint   `gorm:"primaryKey"`
	IndexName string `gorm:"index:idx_search_index_name;size:100;not null"`
	DocID     string `gorm:"index:idx_search_doc;size:255;not null"`
	Data      string `gorm:"type:jsonb"`
	Content   string `gorm:"type:text"`                                   // Concatenated text fields for FTS
	SearchVec string `gorm:"type:tsvector;index:idx_search_fts,type:gin"` // Postgres tsvector
}

func (searchDocument) TableName() string { return "search_index" }

// NewPostgresEngine creates a PostgreSQL-backed search engine.
// It auto-migrates the search_index table.
func NewPostgresEngine(db *gorm.DB) *PostgresEngine {
	_ = db.AutoMigrate(&searchDocument{})
	// Create the tsvector GIN index and trigger (idempotent)
	db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_search_fts ON search_index USING gin(to_tsvector('english', content));
	`)
	return &PostgresEngine{db: db}
}

// Index adds or updates a document in the search index.
func (e *PostgresEngine) Index(ctx context.Context, index string, id string, data map[string]any) error {
	// Serialize data to JSON
	dataJSON, _ := json.Marshal(data)

	// Concatenate all string values for full-text search
	var parts []string
	for _, v := range data {
		switch val := v.(type) {
		case string:
			parts = append(parts, val)
		case fmt.Stringer:
			parts = append(parts, val.String())
		}
	}
	content := strings.Join(parts, " ")

	var existing searchDocument
	err := e.db.WithContext(ctx).
		Where("index_name = ? AND doc_id = ?", index, id).
		First(&existing).Error

	if err == gorm.ErrRecordNotFound {
		// Insert
		doc := searchDocument{
			IndexName: index,
			DocID:     id,
			Data:      string(dataJSON),
			Content:   content,
		}
		return e.db.WithContext(ctx).Create(&doc).Error
	} else if err != nil {
		return err
	}

	// Update
	return e.db.WithContext(ctx).Model(&existing).Updates(map[string]any{
		"data":    string(dataJSON),
		"content": content,
	}).Error
}

// Delete removes a document from the search index.
func (e *PostgresEngine) Delete(ctx context.Context, index string, id string) error {
	return e.db.WithContext(ctx).
		Where("index_name = ? AND doc_id = ?", index, id).
		Delete(&searchDocument{}).Error
}

// Search performs a full-text search using PostgreSQL's to_tsquery.
func (e *PostgresEngine) Search(ctx context.Context, index string, query string, opts *Options) (*Results, error) {
	if opts == nil {
		opts = DefaultOptions()
	}
	if opts.Page < 1 {
		opts.Page = 1
	}
	if opts.PerPage < 1 {
		opts.PerPage = 20
	}

	// Convert search query to tsquery format (split words with &)
	words := strings.Fields(query)
	for i, w := range words {
		// Prefix matching with :*
		words[i] = w + ":*"
	}
	tsquery := strings.Join(words, " & ")

	offset := (opts.Page - 1) * opts.PerPage

	// Count total
	var total int64
	countQuery := e.db.WithContext(ctx).Model(&searchDocument{}).
		Where("index_name = ?", index)
	if query != "" {
		countQuery = countQuery.Where("to_tsvector('english', content) @@ to_tsquery('english', ?)", tsquery)
	}
	if err := countQuery.Count(&total).Error; err != nil {
		return nil, fmt.Errorf("search: count failed: %w", err)
	}

	// Fetch results
	var docs []searchDocument
	fetchQuery := e.db.WithContext(ctx).
		Where("index_name = ?", index)
	if query != "" {
		fetchQuery = fetchQuery.
			Where("to_tsvector('english', content) @@ to_tsquery('english', ?)", tsquery).
			Order(fmt.Sprintf("ts_rank(to_tsvector('english', content), to_tsquery('english', '%s')) DESC", strings.ReplaceAll(tsquery, "'", "''")))
	}

	if opts.Sort != "" {
		fetchQuery = fetchQuery.Order(opts.Sort)
	}

	if err := fetchQuery.Offset(offset).Limit(opts.PerPage).Find(&docs).Error; err != nil {
		return nil, fmt.Errorf("search: query failed: %w", err)
	}

	results := &Results{
		Total:   total,
		Page:    opts.Page,
		PerPage: opts.PerPage,
		Query:   query,
	}

	for _, doc := range docs {
		var data map[string]any
		_ = json.Unmarshal([]byte(doc.Data), &data)
		results.Hits = append(results.Hits, Result{
			ID:   doc.DocID,
			Data: data,
		})
	}

	return results, nil
}

// Flush removes all documents from an index.
func (e *PostgresEngine) Flush(ctx context.Context, index string) error {
	return e.db.WithContext(ctx).
		Where("index_name = ?", index).
		Delete(&searchDocument{}).Error
}
