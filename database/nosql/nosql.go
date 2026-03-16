package nosql

import (
	"context"
	"sync"
	"time"
)

// ══════════════════════════════════════════════════════════════════
// NoSQL Driver Interface
// ══════════════════════════════════════════════════════════════════
//
// Nimbus provides a unified interface for NoSQL databases (MongoDB,
// DynamoDB, Redis-as-datastore, etc.). The interface is inspired by
// AdonisJS Lucid but adapted for Go and document databases.
//
// Usage:
//
//	store := nosql.Connection("mongo")
//	store.Collection("users").InsertOne(ctx, user)
//	store.Collection("users").Find(ctx, nosql.Filter{"email": "alice@example.com"}, &result)

// Filter is a key-value map used for query conditions.
type Filter map[string]any

// Document is a generic document representation.
type Document map[string]any

// SortOrder represents sort direction.
type SortOrder int

const (
	Ascending  SortOrder = 1
	Descending SortOrder = -1
)

// Sort represents sort options.
type Sort map[string]SortOrder

// ── Driver Interface ────────────────────────────────────────────

// Driver is the top-level NoSQL driver (one per database connection).
type Driver interface {
	// Name returns the driver name (e.g. "mongodb", "dynamodb", "redis").
	Name() string

	// Collection returns a Collection handle for the given name.
	Collection(name string) Collection

	// Ping checks if the connection is alive.
	Ping(ctx context.Context) error

	// Close closes the connection.
	Close(ctx context.Context) error

	// Database returns a driver scoped to a specific database (for multi-DB engines like MongoDB).
	Database(name string) Driver

	// DropDatabase drops the entire database (use with caution).
	DropDatabase(ctx context.Context) error
}

// ── Collection Interface ────────────────────────────────────────

// Collection provides CRUD operations on a document collection.
type Collection interface {
	// Name returns the collection name.
	Name() string

	// ── Insert ──────────────────────────────────────────────

	// InsertOne inserts a single document and returns a Result.
	InsertOne(ctx context.Context, doc any) (*InsertResult, error)

	// InsertMany inserts multiple documents.
	InsertMany(ctx context.Context, docs []any) (*InsertManyResult, error)

	// ── Find ────────────────────────────────────────────────

	// FindOne finds a single document matching the filter and decodes into dest.
	FindOne(ctx context.Context, filter Filter, dest any) error

	// Find finds all documents matching the filter and decodes into dest (slice).
	Find(ctx context.Context, filter Filter, dest any, opts ...FindOption) error

	// FindByID finds a document by its ID (string or primitive).
	FindByID(ctx context.Context, id any, dest any) error

	// Count returns the number of documents matching the filter.
	Count(ctx context.Context, filter Filter) (int64, error)

	// Exists returns true if any document matches the filter.
	Exists(ctx context.Context, filter Filter) (bool, error)

	// ── Update ──────────────────────────────────────────────

	// UpdateOne updates a single document matching the filter.
	UpdateOne(ctx context.Context, filter Filter, update any) (*UpdateResult, error)

	// UpdateMany updates all documents matching the filter.
	UpdateMany(ctx context.Context, filter Filter, update any) (*UpdateResult, error)

	// UpdateByID updates a document by its ID.
	UpdateByID(ctx context.Context, id any, update any) (*UpdateResult, error)

	// Upsert inserts or updates a document matching the filter.
	Upsert(ctx context.Context, filter Filter, doc any) (*UpdateResult, error)

	// ── Delete ──────────────────────────────────────────────

	// DeleteOne deletes a single document matching the filter.
	DeleteOne(ctx context.Context, filter Filter) (*DeleteResult, error)

	// DeleteMany deletes all documents matching the filter.
	DeleteMany(ctx context.Context, filter Filter) (*DeleteResult, error)

	// DeleteByID deletes a document by its ID.
	DeleteByID(ctx context.Context, id any) (*DeleteResult, error)

	// ── Aggregation ─────────────────────────────────────────

	// Aggregate runs an aggregation pipeline.
	Aggregate(ctx context.Context, pipeline any, dest any) error

	// Distinct returns distinct values for a field.
	Distinct(ctx context.Context, field string, filter Filter) ([]any, error)

	// ── Index ───────────────────────────────────────────────

	// CreateIndex creates an index on the collection.
	CreateIndex(ctx context.Context, keys Document, opts ...IndexOption) (string, error)

	// DropIndex drops an index by name.
	DropIndex(ctx context.Context, name string) error

	// ── Collection Management ───────────────────────────────

	// Drop drops the entire collection.
	Drop(ctx context.Context) error
}

// ── Result Types ────────────────────────────────────────────────

// InsertResult is returned by InsertOne.
type InsertResult struct {
	InsertedID any
}

// InsertManyResult is returned by InsertMany.
type InsertManyResult struct {
	InsertedIDs []any
}

// UpdateResult is returned by Update operations.
type UpdateResult struct {
	MatchedCount  int64
	ModifiedCount int64
	UpsertedCount int64
	UpsertedID    any
}

// DeleteResult is returned by Delete operations.
type DeleteResult struct {
	DeletedCount int64
}

// ── Options ─────────────────────────────────────────────────────

// FindOption modifies Find behavior.
type FindOption struct {
	// Sort specifies the sort order.
	Sort Sort

	// Limit limits the number of documents returned.
	Limit int64

	// Skip skips a number of documents.
	Skip int64

	// Projection selects which fields to return (1 = include, 0 = exclude).
	Projection Document
}

// IndexOption modifies CreateIndex behavior.
type IndexOption struct {
	// Unique makes the index unique.
	Unique bool

	// Name overrides the default index name.
	Name string

	// ExpireAfterSeconds sets a TTL (for TTL indexes).
	ExpireAfterSeconds *int32

	// Sparse creates a sparse index (only indexes documents that have the indexed field).
	Sparse bool
}

// ── Connection Manager ──────────────────────────────────────────

// connections holds registered NoSQL drivers.
var connections = struct {
	mu    sync.Mutex
	store map[string]Driver
}{store: make(map[string]Driver)}

// Register registers a named NoSQL connection.
//
//	nosql.Register("mongo", mongoDriver)
//	nosql.Register("dynamo", dynamoDriver)
func Register(name string, driver Driver) {
	connections.mu.Lock()
	defer connections.mu.Unlock()
	connections.store[name] = driver
}

// Connection returns a registered NoSQL driver by name.
func Connection(name string) Driver {
	connections.mu.Lock()
	defer connections.mu.Unlock()
	return connections.store[name]
}

// MustConnection returns a registered driver or panics.
func MustConnection(name string) Driver {
	d := Connection(name)
	if d == nil {
		panic("nosql: connection " + name + " not registered")
	}
	return d
}

// CloseAll closes all registered NoSQL connections.
func CloseAll(ctx context.Context) error {
	connections.mu.Lock()
	defer connections.mu.Unlock()

	var lastErr error
	for name, d := range connections.store {
		if err := d.Close(ctx); err != nil {
			lastErr = err
			_ = name
		}
	}
	connections.store = make(map[string]Driver)
	return lastErr
}

// ── Base Model ──────────────────────────────────────────────────

// Model is the base model for NoSQL documents.
// Embed it in your structs for standard fields — mirrors database.Model
// on the SQL side so the pattern stays consistent across your app.
//
//	type Post struct {
//	    nosql.Model
//	    Title string
//	}
type Model struct {
	ID        string     `bson:"_id,omitempty" json:"id"`
	CreatedAt time.Time  `bson:"created_at"   json:"created_at"`
	UpdatedAt time.Time  `bson:"updated_at"   json:"updated_at"`
	DeletedAt *time.Time `bson:"deleted_at,omitempty" json:"deleted_at,omitempty"`
}

// ── Deprecated aliases (kept for backward compatibility) ────────

// BaseDocument is an alias for Model. Deprecated: use nosql.Model instead.
type BaseDocument = Model

// TimestampedDocument is an alias for Model. Deprecated: use nosql.Model instead.
type TimestampedDocument = Model
