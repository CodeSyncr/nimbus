package database

import (
	"gorm.io/gorm"
)

// Query wraps GORM's query builder for Lucid-style fluent API.
// Use db.Table("posts") or Model.Query() to start.
type Query struct {
	db *gorm.DB
}

// From starts a query on the given table (returns plain objects).
func From(db *gorm.DB, table string) *Query {
	return &Query{db: db.Table(table)}
}

// Query returns a query builder for the model's table.
func QueryFor(db *gorm.DB, model any) *Query {
	return &Query{db: db.Model(model)}
}

// Where adds a condition. Supports: Where("status", "published"), Where("id", ">", 5).
func (q *Query) Where(query any, args ...any) *Query {
	q.db = q.db.Where(query, args...)
	return q
}

// OrWhere adds an OR condition.
func (q *Query) OrWhere(query any, args ...any) *Query {
	q.db = q.db.Or(query, args...)
	return q
}

// WhereNotNull adds WHERE column IS NOT NULL.
func (q *Query) WhereNotNull(column string) *Query {
	q.db = q.db.Where(column + " IS NOT NULL")
	return q
}

// WhereNull adds WHERE column IS NULL.
func (q *Query) WhereNull(column string) *Query {
	q.db = q.db.Where(column + " IS NULL")
	return q
}

// Select specifies columns to fetch.
func (q *Query) Select(columns ...string) *Query {
	q.db = q.db.Select(columns)
	return q
}

// OrderBy adds ORDER BY (use "created_at desc" or "name asc").
func (q *Query) OrderBy(value string) *Query {
	q.db = q.db.Order(value)
	return q
}

// Limit sets the maximum number of rows.
func (q *Query) Limit(limit int) *Query {
	q.db = q.db.Limit(limit)
	return q
}

// Offset sets the number of rows to skip.
func (q *Query) Offset(offset int) *Query {
	q.db = q.db.Offset(offset)
	return q
}

// Get executes the query and scans into dest (slice or single struct).
func (q *Query) Get(dest any) error {
	return q.db.Find(dest).Error
}

// First returns the first record (ORDER BY primary key).
func (q *Query) First(dest any) error {
	return q.db.First(dest).Error
}

// DB returns the underlying GORM DB for advanced usage.
func (q *Query) DB() *gorm.DB {
	return q.db
}
