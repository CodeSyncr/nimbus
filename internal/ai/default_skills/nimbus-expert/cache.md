# Cache - Nimbus

The Nimbus Cache package provides a unified API for high-speed data retrieval across various backends.

## Core API

-   `Get(key)`: Retrieve a value.
-   `Set(key, value, ttl)`: Store a value with a time-to-live.
-   `Delete(key)`: Remove a value.
-   `Has(key)`: Check if a key exists.
-   `Remember(key, ttl, fn)`: The "cache or compute" pattern.
-   `RememberT[T](key, ttl, fn)`: Type-safe generic version of `Remember`.

## Drivers

-   **Memory**: Local in-memory storage (dev).
-   **Redis**: Shared fast storage (production).
-   **Memcached**: High-throughput distributed caching.
-   **DynamoDB/Cloudflare KV**: Cloud-native edge caching.

## Usage Patterns

### Cache or Compute (Remember)

```go
val, _ := cache.RememberT[User]("user:42", time.Hour, func() (User, error) {
    return db.First(&User{}, 42)
})
```

### Atomic Locks

Prevent cache stampedes (multiple processes trying to rebuild the same cache key simultaneously) using atomic locks.

```go
lock := cache.NewLock("calculate-stats", 30*time.Second)
if lock.Acquire() {
    defer lock.Release()
    // Perform expensive calculation
}
```

## Namespaces

Group related cache entries for easier management and bulk invalidation.

```go
productCache := cache.Namespace("products")
productCache.Set("1", p, time.Hour)
productCache.Clear() // Removes all "products:*" keys
```

## Best Practices

1.  **User-Scoped Keys**: Always include a user ID in the cache key for user-specific data.
2.  **Short TTLs**: Prefer shorter TTLs for dynamic data to avoid serving stale content.
3.  **Invalidate on Write**: Clear relevant cache keys immediately after updating the underlying data.
4.  **Use `Remember`**: It simplifies your code and handles concurrent misses gracefully.
