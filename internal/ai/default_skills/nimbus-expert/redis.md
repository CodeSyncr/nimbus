# Redis - Nimbus

Nimbus provides a `redis` package that is a thin wrapper around [go-redis/v9](https://github.com/redis/go-redis), the standard Go Redis client. The same client underlies the queue, cache, session, Transmit, and Horizon subsystems.

## Import path
```go
import "github.com/CodeSyncr/nimbus/redis"
```

## Creating a client

```go
// Using options struct
rdb := redis.NewClient(&redis.Options{
    Addr:     "localhost:6379",
    Password: "",
    DB:       0,
})

// Using a redis:// URL
opt, err := redis.ParseURL(os.Getenv("REDIS_URL"))
rdb := redis.NewClient(opt)
```

## Registering as a container singleton (recommended)

```go
// bin/server.go
rdb := redis.NewClient(&redis.Options{Addr: os.Getenv("REDIS_ADDR")})
app.Container.Singleton("redis", func() *redis.Client { return rdb })

// Resolving in routes.go
rdb := app.Container.MustMake("redis").(*redis.Client)
```

## Core operations

```go
ctx := context.Background()

// Strings
rdb.Set(ctx, "key", "value", 0)                         // no expiry
rdb.Set(ctx, "otp", "123456", 5*time.Minute)            // with TTL
val, err := rdb.Get(ctx, "key").Result()
if err == redis.Nil { /* key missing */ }

// Increment
rdb.Incr(ctx, "counter")
rdb.IncrBy(ctx, "score", 10)

// Delete / exists
rdb.Del(ctx, "key")
n, _ := rdb.Exists(ctx, "key").Result() // 1 = exists

// Expiry
rdb.Expire(ctx, "key", 30*time.Minute)
ttl, _ := rdb.TTL(ctx, "key").Result()
```

## Hashes

```go
rdb.HSet(ctx, "user:1", "name", "Virk", "email", "virk@example.com")
name, _ := rdb.HGet(ctx, "user:1", "name").Result()
all, _  := rdb.HGetAll(ctx, "user:1").Result() // map[string]string
rdb.HDel(ctx, "user:1", "email")
```

## Lists

```go
rdb.LPush(ctx, "notifications", "msg1", "msg2")
rdb.RPush(ctx, "queue", "job1")
val, _ := rdb.LPop(ctx, "list").Result()
items, _ := rdb.LRange(ctx, "list", 0, -1).Result()
```

## Sets & Sorted Sets

```go
// Sets
rdb.SAdd(ctx, "tags", "go", "nimbus")
members, _ := rdb.SMembers(ctx, "tags").Result()

// Sorted sets (leaderboard)
rdb.ZAdd(ctx, "scores",
    redis.Z{Score: 100, Member: "alice"},
    redis.Z{Score: 85, Member: "bob"},
)
top, _ := rdb.ZRevRangeWithScores(ctx, "scores", 0, 9).Result()
```

## Pub/Sub

```go
// Subscribe
pubsub := rdb.Subscribe(ctx, "events")
defer pubsub.Close()
for msg := range pubsub.Channel() {
    fmt.Println(msg.Payload)
}

// Publish
rdb.Publish(ctx, "events", "order_created:42")
```

## Error handling

```go
val, err := rdb.Get(ctx, "key").Result()
switch {
case err == redis.Nil:
    // key does not exist
case err != nil:
    // connection or command error
default:
    // use val
}
```

## Type aliases

The `redis` package re-exports go-redis types as type aliases:
- `redis.Client` = `goredis.Client`
- `redis.Options` = `goredis.Options`
- `redis.Z`       = `goredis.Z` (sorted set member)
- `redis.PubSub`  = `goredis.PubSub`
- `redis.Nil`     = `goredis.Nil` (sentinel for missing keys)

## `.env` convention
```
REDIS_URL=redis://localhost:6379
# With password:
# REDIS_URL=redis://:password@host:6379/0
```
