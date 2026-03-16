package database

import (
	"fmt"

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

// WhereIn adds WHERE column IN (values...).
func (q *Query) WhereIn(column string, values ...any) *Query {
	q.db = q.db.Where(fmt.Sprintf("%s IN ?", column), values)
	return q
}

// WhereNotIn adds WHERE column NOT IN (values...).
func (q *Query) WhereNotIn(column string, values ...any) *Query {
	q.db = q.db.Where(fmt.Sprintf("%s NOT IN ?", column), values)
	return q
}

// WhereBetween adds WHERE column BETWEEN low AND high.
func (q *Query) WhereBetween(column string, low, high any) *Query {
	q.db = q.db.Where(fmt.Sprintf("%s BETWEEN ? AND ?", column), low, high)
	return q
}

// WhereLike adds WHERE column LIKE pattern.
func (q *Query) WhereLike(column, pattern string) *Query {
	q.db = q.db.Where(fmt.Sprintf("%s LIKE ?", column), pattern)
	return q
}

// WhereRaw adds a raw WHERE clause.
func (q *Query) WhereRaw(sql string, args ...any) *Query {
	q.db = q.db.Where(sql, args...)
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

// ── Join Helpers ────────────────────────────────────────────────

// Join adds an INNER JOIN clause.
func (q *Query) Join(query string, args ...any) *Query {
	q.db = q.db.Joins(query, args...)
	return q
}

// LeftJoin adds a LEFT JOIN clause.
func (q *Query) LeftJoin(table, condition string) *Query {
	q.db = q.db.Joins(fmt.Sprintf("LEFT JOIN %s ON %s", table, condition))
	return q
}

// RightJoin adds a RIGHT JOIN clause.
func (q *Query) RightJoin(table, condition string) *Query {
	q.db = q.db.Joins(fmt.Sprintf("RIGHT JOIN %s ON %s", table, condition))
	return q
}

// ── Grouping & Having ───────────────────────────────────────────

// GroupBy adds GROUP BY columns.
func (q *Query) GroupBy(columns ...string) *Query {
	for _, col := range columns {
		q.db = q.db.Group(col)
	}
	return q
}

// Having adds a HAVING clause.
func (q *Query) Having(query string, args ...any) *Query {
	q.db = q.db.Having(query, args...)
	return q
}

// ── Aggregate Functions ─────────────────────────────────────────

// Count returns the count of matching records.
func (q *Query) Count() (int64, error) {
	var count int64
	err := q.db.Count(&count).Error
	return count, err
}

// Sum returns the SUM of a column.
func (q *Query) Sum(column string) (float64, error) {
	var result float64
	err := q.db.Select(fmt.Sprintf("COALESCE(SUM(%s), 0)", column)).Scan(&result).Error
	return result, err
}

// Avg returns the AVG of a column.
func (q *Query) Avg(column string) (float64, error) {
	var result float64
	err := q.db.Select(fmt.Sprintf("COALESCE(AVG(%s), 0)", column)).Scan(&result).Error
	return result, err
}

// Max returns the MAX of a column.
func (q *Query) Max(column string) (float64, error) {
	var result float64
	err := q.db.Select(fmt.Sprintf("MAX(%s)", column)).Scan(&result).Error
	return result, err
}

// Min returns the MIN of a column.
func (q *Query) Min(column string) (float64, error) {
	var result float64
	err := q.db.Select(fmt.Sprintf("MIN(%s)", column)).Scan(&result).Error
	return result, err
}

// ── Eager Loading ───────────────────────────────────────────────

// Preload eager-loads an association. Chain for multiple:
//
//	query.Preload("User").Preload("Comments").Get(&posts)
func (q *Query) Preload(name string, args ...any) *Query {
	q.db = q.db.Preload(name, args...)
	return q
}

// ── Distinct ────────────────────────────────────────────────────

// Distinct adds DISTINCT to the query.
func (q *Query) Distinct(columns ...string) *Query {
	if len(columns) > 0 {
		q.db = q.db.Distinct(columns)
	} else {
		q.db = q.db.Distinct()
	}
	return q
}

// ── Scopes ──────────────────────────────────────────────────────

// Scopes applies one or more scopes to the query.
func (q *Query) Scopes(scopes ...func(*gorm.DB) *gorm.DB) *Query {
	q.db = q.db.Scopes(scopes...)
	return q
}

// ── Unscoped ────────────────────────────────────────────────────

// Unscoped removes soft delete filter.
func (q *Query) Unscoped() *Query {
	q.db = q.db.Unscoped()
	return q
}

// ── Terminal Methods ────────────────────────────────────────────

// Get executes the query and scans into dest (slice or single struct).
func (q *Query) Get(dest any) error {
	return q.db.Find(dest).Error
}

// First returns the first record (ORDER BY primary key).
func (q *Query) First(dest any) error {
	return q.db.First(dest).Error
}

// Last returns the last record (ORDER BY primary key DESC).
func (q *Query) Last(dest any) error {
	return q.db.Last(dest).Error
}

// Find finds records matching primary key(s).
func (q *Query) Find(dest any, conds ...any) error {
	return q.db.Find(dest, conds...).Error
}

// FirstOrFail returns the first record or returns a record-not-found error.
func (q *Query) FirstOrFail(dest any) error {
	err := q.db.First(dest).Error
	if err != nil {
		return fmt.Errorf("record not found")
	}
	return nil
}

// Create inserts a new record.
func (q *Query) Create(value any) error {
	return q.db.Create(value).Error
}

// Update updates a single column.
func (q *Query) Update(column string, value any) error {
	return q.db.Update(column, value).Error
}

// Updates updates multiple columns with a map or struct.
func (q *Query) Updates(values any) error {
	return q.db.Updates(values).Error
}

// Delete deletes matching records (soft delete if model has DeletedAt).
func (q *Query) Delete(value any, conds ...any) error {
	return q.db.Delete(value, conds...).Error
}

// Pluck retrieves a single column as a slice.
func (q *Query) Pluck(column string, dest any) error {
	return q.db.Pluck(column, dest).Error
}

// Exists returns true if any matching record exists.
func (q *Query) Exists() (bool, error) {
	var count int64
	err := q.db.Count(&count).Error
	return count > 0, err
}

// Paginate returns paginated results.
func (q *Query) Paginate(dest any, page, perPage int) (*Paginator, error) {
	return Paginate(q.db, dest, page, perPage)
}

// ── Raw Queries ─────────────────────────────────────────────────

// Raw executes a raw SQL query and scans into dest.
func Raw(db *gorm.DB, sql string, dest any, args ...any) error {
	return db.Raw(sql, args...).Scan(dest).Error
}

// Exec executes a raw SQL statement (INSERT, UPDATE, DELETE).
func Exec(db *gorm.DB, sql string, args ...any) error {
	return db.Exec(sql, args...).Error
}

// DB returns the underlying GORM DB for advanced usage.
func (q *Query) DB() *gorm.DB {
	return q.db
}
