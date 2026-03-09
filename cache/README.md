# Cache

Multi-tier distributed cache for Nimbus. Supports memory, Redis, Memcached, DynamoDB, and Cloudflare KV.

## Boot

```go
cache.Boot(nil)  // uses CACHE_DRIVER from .env
```

## Remember (getOrSet)

```go
user, err := cache.RememberT("user:1", 10*time.Minute, func() (User, error) {
    var u User
    err := database.Get().First(&u, 1).Error
    return u, err
})
```

## Get and Set

```go
cache.Set("app:settings", map[string]any{"theme": "dark"}, 5*time.Minute)
settings, ok := cache.Get("app:settings")

cache.SetForever("app:version", "2.0.0")  // never expires

if cache.Has("products:featured") { /* key exists */ }
if cache.Missing("products:featured") { /* key does not exist */ }

token, ok := cache.Pull("verify:token:123")  // get and delete in one call
```

## Namespaces

```go
usersCache := cache.Namespace("users")
usersCache.Set("42", user, 10*time.Minute)  // stores under "users:42"
usersCache.Clear()  // clears all "users:*" (Memory & Redis)
```

## Backends

| Driver | Env | Notes |
|--------|-----|-------|
| memory | (default) | Single process |
| redis | REDIS_URL | Prefix invalidation |
| memcached | MEMCACHED_SERVERS | Key limit 250 bytes |
| dynamodb | CACHE_DYNAMO_TABLE, AWS_REGION | Table: pk (string), val, ttl |
| cloudflare | CLOUDFLARE_* | Min TTL 60s |

## Invalidation

```go
cache.Delete("user:1")
cache.InvalidatePrefix("user:")  // Memory & Redis
```

## ORM integration

```go
database.RegisterHooks(db, "users", database.Hooks{
    AfterSave: func(db *gorm.DB) {
        if u, ok := db.Statement.Model.(*User); ok {
            cache.Delete(fmt.Sprintf("user:%d", u.ID))
        }
    },
})

// Query caching
database.CachedFind(db.Model(&User{}), "users:list", 10*time.Minute, &users)
```
