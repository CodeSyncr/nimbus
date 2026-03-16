package nosql

import (
	"context"
	"fmt"
)

// ══════════════════════════════════════════════════════════════════
// NoSQL Query Builder — Fluent API for document queries
// ══════════════════════════════════════════════════════════════════
//
// Provides a chainable query builder similar to the SQL Query builder
// but adapted for document databases (MongoDB, DynamoDB, etc.).
//
// Usage:
//
//	var users []User
//	nosql.Query("mongo", "users").
//	    Where("age", ">=", 21).
//	    Where("active", true).
//	    Sort("name", nosql.Ascending).
//	    Limit(10).
//	    Get(ctx, &users)
//
//	count, _ := nosql.Query("mongo", "orders").
//	    Where("status", "pending").
//	    Count(ctx)

// Builder is a fluent query builder for NoSQL collections.
type Builder struct {
	driver     Driver
	collection string
	filters    Filter
	sortOrder  Sort
	limit      int64
	skip       int64
	projection Document
}

// Query starts a new query builder on a registered connection and collection.
//
//	nosql.Query("mongo", "users").Where("active", true).Get(ctx, &users)
func Query(connectionName, collectionName string) *Builder {
	return &Builder{
		driver:     Connection(connectionName),
		collection: collectionName,
		filters:    make(Filter),
		sortOrder:  make(Sort),
	}
}

// QueryOn starts a query builder on a specific driver instance.
//
//	nosql.QueryOn(mongoDriver, "users").Where("role", "admin").Get(ctx, &users)
func QueryOn(driver Driver, collectionName string) *Builder {
	return &Builder{
		driver:     driver,
		collection: collectionName,
		filters:    make(Filter),
		sortOrder:  make(Sort),
	}
}

// ── Filter Methods ──────────────────────────────────────────────

// Where adds an equality filter. Supports multiple call patterns:
//
//	Where("name", "Alice")            → {name: "Alice"}
//	Where("age", ">=", 21)            → {age: {$gte: 21}}
//	Where("status", "in", []string{"active", "pending"})
func (b *Builder) Where(field string, args ...any) *Builder {
	if len(args) == 1 {
		// Simple equality: Where("name", "Alice")
		b.filters[field] = args[0]
	} else if len(args) == 2 {
		// Operator: Where("age", ">=", 21)
		op := fmt.Sprintf("%v", args[0])
		value := args[1]
		b.filters[field] = operatorFilter(op, value)
	}
	return b
}

// WhereIn adds a $in filter.
//
//	WhereIn("status", "active", "pending", "review")
func (b *Builder) WhereIn(field string, values ...any) *Builder {
	b.filters[field] = Document{"$in": values}
	return b
}

// WhereNotIn adds a $nin filter.
func (b *Builder) WhereNotIn(field string, values ...any) *Builder {
	b.filters[field] = Document{"$nin": values}
	return b
}

// WhereExists adds an $exists filter.
//
//	WhereExists("deletedAt", false)  → documents without deletedAt field
func (b *Builder) WhereExists(field string, exists bool) *Builder {
	b.filters[field] = Document{"$exists": exists}
	return b
}

// WhereNull matches documents where a field is null or missing.
func (b *Builder) WhereNull(field string) *Builder {
	b.filters[field] = nil
	return b
}

// WhereNotNull matches documents where a field exists and is not null.
func (b *Builder) WhereNotNull(field string) *Builder {
	b.filters[field] = Document{"$ne": nil}
	return b
}

// WhereRegex matches documents where a field matches a regex pattern.
func (b *Builder) WhereRegex(field, pattern string) *Builder {
	b.filters[field] = Document{"$regex": pattern}
	return b
}

// WhereBetween adds a range filter (inclusive).
//
//	WhereBetween("age", 18, 30)
func (b *Builder) WhereBetween(field string, min, max any) *Builder {
	b.filters[field] = Document{"$gte": min, "$lte": max}
	return b
}

// WhereRaw sets a raw filter map for the given field, allowing
// full control over the MongoDB query operators.
//
//	WhereRaw("location", nosql.Document{"$near": ...})
func (b *Builder) WhereRaw(field string, raw Document) *Builder {
	b.filters[field] = raw
	return b
}

// OrWhere is not natively supported by all NoSQL backends but
// can be expressed with $or in MongoDB. This builds an $or clause.
//
//	OrWhere(nosql.Filter{"status": "active"}, nosql.Filter{"role": "admin"})
func (b *Builder) OrWhere(conditions ...Filter) *Builder {
	orSlice := make([]Document, len(conditions))
	for i, c := range conditions {
		orSlice[i] = Document(c)
	}
	b.filters["$or"] = orSlice
	return b
}

// ── Sort / Limit / Skip ─────────────────────────────────────────

// Sort adds a sort order for a field.
//
//	Sort("created_at", nosql.Descending).Sort("name", nosql.Ascending)
func (b *Builder) Sort(field string, order SortOrder) *Builder {
	b.sortOrder[field] = order
	return b
}

// Latest sorts by a field descending (default: "created_at").
func (b *Builder) Latest(field ...string) *Builder {
	f := "created_at"
	if len(field) > 0 {
		f = field[0]
	}
	b.sortOrder[f] = Descending
	return b
}

// Oldest sorts by a field ascending (default: "created_at").
func (b *Builder) Oldest(field ...string) *Builder {
	f := "created_at"
	if len(field) > 0 {
		f = field[0]
	}
	b.sortOrder[f] = Ascending
	return b
}

// Limit limits the number of results.
func (b *Builder) Limit(n int64) *Builder {
	b.limit = n
	return b
}

// Skip skips N documents (for pagination).
func (b *Builder) Skip(n int64) *Builder {
	b.skip = n
	return b
}

// Select specifies which fields to include in results.
//
//	Select("name", "email", "age")
func (b *Builder) Select(fields ...string) *Builder {
	if b.projection == nil {
		b.projection = make(Document)
	}
	for _, f := range fields {
		b.projection[f] = 1
	}
	return b
}

// Exclude specifies which fields to exclude from results.
//
//	Exclude("password", "internal_notes")
func (b *Builder) Exclude(fields ...string) *Builder {
	if b.projection == nil {
		b.projection = make(Document)
	}
	for _, f := range fields {
		b.projection[f] = 0
	}
	return b
}

// ── Terminal Methods (Execute) ──────────────────────────────────

// Get executes the query and decodes all matching documents into dest.
func (b *Builder) Get(ctx context.Context, dest any) error {
	if b.driver == nil {
		return fmt.Errorf("nosql: no driver configured")
	}
	coll := b.driver.Collection(b.collection)
	return coll.Find(ctx, b.filters, dest, b.buildFindOption())
}

// First returns the first matching document.
func (b *Builder) First(ctx context.Context, dest any) error {
	if b.driver == nil {
		return fmt.Errorf("nosql: no driver configured")
	}
	coll := b.driver.Collection(b.collection)
	return coll.FindOne(ctx, b.filters, dest)
}

// FindByID finds a single document by ID.
func (b *Builder) FindByID(ctx context.Context, id any, dest any) error {
	if b.driver == nil {
		return fmt.Errorf("nosql: no driver configured")
	}
	coll := b.driver.Collection(b.collection)
	return coll.FindByID(ctx, id, dest)
}

// Count returns the number of matching documents.
func (b *Builder) Count(ctx context.Context) (int64, error) {
	if b.driver == nil {
		return 0, fmt.Errorf("nosql: no driver configured")
	}
	coll := b.driver.Collection(b.collection)
	return coll.Count(ctx, b.filters)
}

// Exists returns true if any matching document exists.
func (b *Builder) Exists(ctx context.Context) (bool, error) {
	if b.driver == nil {
		return false, fmt.Errorf("nosql: no driver configured")
	}
	coll := b.driver.Collection(b.collection)
	return coll.Exists(ctx, b.filters)
}

// ── Mutation Methods ────────────────────────────────────────────

// Insert inserts a single document.
func (b *Builder) Insert(ctx context.Context, doc any) (*InsertResult, error) {
	if b.driver == nil {
		return nil, fmt.Errorf("nosql: no driver configured")
	}
	return b.driver.Collection(b.collection).InsertOne(ctx, doc)
}

// InsertMany inserts multiple documents.
func (b *Builder) InsertMany(ctx context.Context, docs []any) (*InsertManyResult, error) {
	if b.driver == nil {
		return nil, fmt.Errorf("nosql: no driver configured")
	}
	return b.driver.Collection(b.collection).InsertMany(ctx, docs)
}

// Update updates the first document matching the current filters.
func (b *Builder) Update(ctx context.Context, update any) (*UpdateResult, error) {
	if b.driver == nil {
		return nil, fmt.Errorf("nosql: no driver configured")
	}
	return b.driver.Collection(b.collection).UpdateOne(ctx, b.filters, update)
}

// UpdateMany updates all documents matching the current filters.
func (b *Builder) UpdateMany(ctx context.Context, update any) (*UpdateResult, error) {
	if b.driver == nil {
		return nil, fmt.Errorf("nosql: no driver configured")
	}
	return b.driver.Collection(b.collection).UpdateMany(ctx, b.filters, update)
}

// Upsert inserts or updates a document matching the current filters.
func (b *Builder) Upsert(ctx context.Context, doc any) (*UpdateResult, error) {
	if b.driver == nil {
		return nil, fmt.Errorf("nosql: no driver configured")
	}
	return b.driver.Collection(b.collection).Upsert(ctx, b.filters, doc)
}

// Delete deletes the first document matching the current filters.
func (b *Builder) Delete(ctx context.Context) (*DeleteResult, error) {
	if b.driver == nil {
		return nil, fmt.Errorf("nosql: no driver configured")
	}
	return b.driver.Collection(b.collection).DeleteOne(ctx, b.filters)
}

// DeleteMany deletes all documents matching the current filters.
func (b *Builder) DeleteMany(ctx context.Context) (*DeleteResult, error) {
	if b.driver == nil {
		return nil, fmt.Errorf("nosql: no driver configured")
	}
	return b.driver.Collection(b.collection).DeleteMany(ctx, b.filters)
}

// ── Aggregation ─────────────────────────────────────────────────

// Aggregate runs an aggregation pipeline on the collection.
func (b *Builder) Aggregate(ctx context.Context, pipeline any, dest any) error {
	if b.driver == nil {
		return fmt.Errorf("nosql: no driver configured")
	}
	return b.driver.Collection(b.collection).Aggregate(ctx, pipeline, dest)
}

// Distinct returns distinct values for a field matching current filters.
func (b *Builder) Distinct(ctx context.Context, field string) ([]any, error) {
	if b.driver == nil {
		return nil, fmt.Errorf("nosql: no driver configured")
	}
	return b.driver.Collection(b.collection).Distinct(ctx, field, b.filters)
}

// ── Pagination ──────────────────────────────────────────────────

// Paginate returns paginated results with metadata.
func (b *Builder) Paginate(ctx context.Context, dest any, page, perPage int64) (*NoSQLPaginator, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}

	// Count total
	total, err := b.Count(ctx)
	if err != nil {
		return nil, err
	}

	// Fetch page
	b.skip = (page - 1) * perPage
	b.limit = perPage
	if err := b.Get(ctx, dest); err != nil {
		return nil, err
	}

	lastPage := total / int64(perPage)
	if total%int64(perPage) > 0 {
		lastPage++
	}
	if lastPage < 1 {
		lastPage = 1
	}

	return &NoSQLPaginator{
		Data:        dest,
		Total:       total,
		PerPage:     perPage,
		CurrentPage: page,
		LastPage:    lastPage,
	}, nil
}

// NoSQLPaginator holds paginated NoSQL results.
type NoSQLPaginator struct {
	Data        any   `json:"data"`
	Total       int64 `json:"total"`
	PerPage     int64 `json:"per_page"`
	CurrentPage int64 `json:"current_page"`
	LastPage    int64 `json:"last_page"`
}

// HasMore returns true if there are more pages.
func (p *NoSQLPaginator) HasMore() bool {
	return p.CurrentPage < p.LastPage
}

// ── Helpers ─────────────────────────────────────────────────────

func (b *Builder) buildFindOption() FindOption {
	opt := FindOption{
		Limit: b.limit,
		Skip:  b.skip,
	}
	if len(b.sortOrder) > 0 {
		opt.Sort = b.sortOrder
	}
	if len(b.projection) > 0 {
		opt.Projection = b.projection
	}
	return opt
}

// operatorFilter converts a string operator to a MongoDB-style filter.
func operatorFilter(op string, value any) Document {
	switch op {
	case "=", "==", "eq":
		return Document{"$eq": value}
	case "!=", "<>", "ne":
		return Document{"$ne": value}
	case ">", "gt":
		return Document{"$gt": value}
	case ">=", "gte":
		return Document{"$gte": value}
	case "<", "lt":
		return Document{"$lt": value}
	case "<=", "lte":
		return Document{"$lte": value}
	case "in":
		return Document{"$in": value}
	case "nin", "not_in":
		return Document{"$nin": value}
	case "regex":
		return Document{"$regex": value}
	case "exists":
		return Document{"$exists": value}
	default:
		return Document{"$eq": value}
	}
}
