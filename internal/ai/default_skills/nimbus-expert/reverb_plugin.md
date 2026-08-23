# Reverb Plugin (WebSocket Broadcasting)

Reverb provides WebSocket channel broadcasting with optional Redis fan-out for multi-instance deployments.

## Installation
```bash
nimbus plugin install reverb
```

## Configuration
```go
app.Use(reverb.New(reverb.Config{
    Path:      "/reverb",
    Transport: "memory",  // "memory" or "redis"
    Redis:     redis.Options{Addr: "localhost:6379"},
}))
```

## Channel Types
| Type | Prefix | Auth | Use Case |
|------|--------|------|----------|
| Public | none | No | Global events |
| Private | `private-` | Yes | User-specific data |
| Presence | `presence-` | Yes | Who's online |

## Broadcasting
```go
reverb.Broadcast("orders", map[string]any{
    "event": "OrderCreated",
    "data":  order,
})

reverb.BroadcastToPrivate("private-user-42", map[string]any{
    "event": "NotificationReceived",
    "data":  notification,
})
```

## Channel Authorization
```go
reverb.AuthorizeChannel("private-user-*", func(ctx context.Context, channel string) bool {
    user := auth.UserFromContext(ctx)
    expectedID := strings.TrimPrefix(channel, "private-user-")
    return user != nil && user.GetID() == expectedID
})
```

## Client-Side (JavaScript)
```javascript
const ws = new WebSocket('ws://localhost:8080/reverb');
ws.onmessage = (event) => {
    const msg = JSON.parse(event.data);
    console.log(msg.event, msg.data);
};
// Subscribe to channel
ws.send(JSON.stringify({type: "subscribe", channel: "orders"}));
```

## Plugin Capabilities
Implements `HasRoutes`, `HasConfig`, `HasBindings`, `HasShutdown`.
