/*
|--------------------------------------------------------------------------
| AI SDK — pgvector Vector Store Backend
|--------------------------------------------------------------------------
|
| Uses PostgreSQL + pgvector extension for production-grade vector
| similarity search. Requires pgvector to be installed on your
| PostgreSQL instance.
|
| Usage:
|
|   db, _ := gorm.Open(postgres.Open(dsn), &gorm.Config{})
|   backend := ai.NewPgvectorStore(db, ai.WithPgvectorTable("embeddings"))
|   store := ai.VectorStoreInstance("knowledge", backend)
|
*/

package ai

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// pgvector backend
// ---------------------------------------------------------------------------

// PgvectorRecord is the GORM model for the vector store table.
type PgvectorRecord struct {
	ID       string `gorm:"primarykey;size:255"`
	Text     string `gorm:"type:text"`
	Vector   string `gorm:"type:text"` // stored as text representation of float array
	Metadata string `gorm:"type:text"` // JSON metadata
}

type pgvectorStore struct {
	db        *gorm.DB
	table     string
	dimension int
	migrated  bool
}

// PgvectorOption configures the pgvector backend.
type PgvectorOption func(*pgvectorStore)

// WithPgvectorTable sets the table name (default: "ai_vectors").
func WithPgvectorTable(name string) PgvectorOption {
	return func(s *pgvectorStore) { s.table = name }
}

// WithPgvectorDimension sets the vector dimension (default: 1536 for OpenAI).
func WithPgvectorDimension(d int) PgvectorOption {
	return func(s *pgvectorStore) { s.dimension = d }
}

// NewPgvectorStore creates a pgvector-backed VectorStoreBackend.
// Requires the `pgvector` extension on PostgreSQL.
//
//	db, _ := gorm.Open(postgres.Open(dsn), &gorm.Config{})
//	backend := ai.NewPgvectorStore(db)
//	store := ai.VectorStoreInstance("docs", backend)
func NewPgvectorStore(db *gorm.DB, opts ...PgvectorOption) VectorStoreBackend {
	s := &pgvectorStore{
		db:        db,
		table:     "ai_vectors",
		dimension: 1536,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *pgvectorStore) ensureTable() error {
	if s.migrated {
		return nil
	}
	s.migrated = true

	// Enable pgvector extension.
	s.db.Exec("CREATE EXTENSION IF NOT EXISTS vector")

	// Create table with vector column.
	sql := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id VARCHAR(255) PRIMARY KEY,
			text TEXT NOT NULL DEFAULT '',
			vector vector(%d),
			metadata TEXT DEFAULT '{}'
		)
	`, s.table, s.dimension)
	return s.db.Exec(sql).Error
}

func (s *pgvectorStore) Add(ctx context.Context, doc Document) error {
	if err := s.ensureTable(); err != nil {
		return fmt.Errorf("ai: pgvector migrate: %w", err)
	}

	vecStr := vectorToString(doc.Vector)
	metaJSON := "{}"
	if doc.Metadata != nil {
		pairs := make([]string, 0, len(doc.Metadata))
		for k, v := range doc.Metadata {
			pairs = append(pairs, fmt.Sprintf(`"%s":"%s"`, k, v))
		}
		metaJSON = "{" + strings.Join(pairs, ",") + "}"
	}

	sql := fmt.Sprintf(
		`INSERT INTO %s (id, text, vector, metadata) VALUES (?, ?, ?::vector, ?)
		 ON CONFLICT (id) DO UPDATE SET text = EXCLUDED.text, vector = EXCLUDED.vector, metadata = EXCLUDED.metadata`,
		s.table,
	)
	return s.db.WithContext(ctx).Exec(sql, doc.ID, doc.Text, vecStr, metaJSON).Error
}

func (s *pgvectorStore) AddBatch(ctx context.Context, docs []Document) error {
	for _, doc := range docs {
		if err := s.Add(ctx, doc); err != nil {
			return err
		}
	}
	return nil
}

func (s *pgvectorStore) Search(ctx context.Context, vector []float32, topK int) ([]Document, error) {
	if err := s.ensureTable(); err != nil {
		return nil, fmt.Errorf("ai: pgvector migrate: %w", err)
	}

	vecStr := vectorToString(vector)
	sql := fmt.Sprintf(
		`SELECT id, text, metadata, 1 - (vector <=> ?::vector) AS score
		 FROM %s
		 ORDER BY vector <=> ?::vector
		 LIMIT ?`,
		s.table,
	)

	type result struct {
		ID       string
		Text     string
		Metadata string
		Score    float32
	}

	var results []result
	if err := s.db.WithContext(ctx).Raw(sql, vecStr, vecStr, topK).Scan(&results).Error; err != nil {
		return nil, fmt.Errorf("ai: pgvector search: %w", err)
	}

	docs := make([]Document, len(results))
	for i, r := range results {
		docs[i] = Document{
			ID:       r.ID,
			Text:     r.Text,
			Score:    r.Score,
			Metadata: parseMetadataJSON(r.Metadata),
		}
	}
	return docs, nil
}

func (s *pgvectorStore) Delete(ctx context.Context, id string) error {
	if err := s.ensureTable(); err != nil {
		return fmt.Errorf("ai: pgvector migrate: %w", err)
	}
	sql := fmt.Sprintf("DELETE FROM %s WHERE id = ?", s.table)
	return s.db.WithContext(ctx).Exec(sql, id).Error
}

func (s *pgvectorStore) Count(ctx context.Context) (int, error) {
	if err := s.ensureTable(); err != nil {
		return 0, fmt.Errorf("ai: pgvector migrate: %w", err)
	}
	var count int64
	sql := fmt.Sprintf("SELECT COUNT(*) FROM %s", s.table)
	if err := s.db.WithContext(ctx).Raw(sql).Scan(&count).Error; err != nil {
		return 0, fmt.Errorf("ai: pgvector count: %w", err)
	}
	return int(count), nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func vectorToString(v []float32) string {
	parts := make([]string, len(v))
	for i, val := range v {
		parts[i] = fmt.Sprintf("%f", val)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func parseMetadataJSON(s string) map[string]string {
	if s == "" || s == "{}" {
		return nil
	}
	// Simple JSON parsing for flat key-value metadata.
	m := make(map[string]string)
	s = strings.TrimPrefix(s, "{")
	s = strings.TrimSuffix(s, "}")
	for _, pair := range strings.Split(s, ",") {
		parts := strings.SplitN(pair, ":", 2)
		if len(parts) == 2 {
			key := strings.Trim(strings.TrimSpace(parts[0]), `"`)
			val := strings.Trim(strings.TrimSpace(parts[1]), `"`)
			if key != "" {
				m[key] = val
			}
		}
	}
	return m
}
