# NoSQL - Nimbus

Nimbus provides a unified interface for NoSQL databases, primarily supporting MongoDB while allowing for other drivers.

## Unified Interface

The `nosql.Driver` and `nosql.Collection` interfaces define standard CRUD operations across different NoSQL backends.

### Key Operations

-   `InsertOne(ctx, doc)`, `InsertMany(ctx, docs)`
-   `FindOne(ctx, filter, dest)`, `Find(ctx, filter, dest, opts...)`
-   `UpdateOne(ctx, filter, update)`, `Upsert(ctx, filter, doc)`
-   `DeleteOne(ctx, filter)`, `DeleteMany(ctx, filter)`
-   `Count(ctx, filter)`, `Exists(ctx, filter)`

## MongoDB Support

The MongoDB driver (`nosql.MongoDriver`) implements the unified interface and provides access to MongoDB-specific features like aggregation.

### Usage

```go
store := nosql.Connection("mongo")
userCollection := store.Collection("users")

// Find a user
var user User
err := userCollection.FindOne(ctx, nosql.Filter{"email": "alice@example.com"}, &user)
```

## Model Pattern

Similar to the SQL side, NoSQL models should embed `nosql.Model` to include common fields like `ID` (string/BSON object ID), `CreatedAt`, and `UpdatedAt`.

```go
type Post struct {
    nosql.Model `bson:",inline"`
    Title       string `bson:"title"`
    Content     string `bson:"content"`
}
```

## Redis Integration

Redis is used across the framework for caching, sessions, rate limiting, and background queues.

-   **Datastore**: Can be used via the `nosql.Driver` for simple key-value storage.
-   **Cache**: Used by the `cache` package for high-speed data access.
-   **Queue**: Default backend for background job processing.
-   **SSE**: Transport layer for real-time notifications.

## Best Practices

1.  **Use Filters and Sorts**: Use `nosql.Filter` and `nosql.Sort` types for consistency.
2.  **Explicit Context**: Always pass a `context.Context` for cancellation and timeout support.
3.  **Model Embeds**: Always embed `nosql.Model` to maintain framework-wide consistency.
