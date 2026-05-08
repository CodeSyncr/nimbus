# Transmit Plugin (SSE Real-Time)

The Transmit plugin provides Server-Sent Events (SSE) for real-time server-to-client push communication.

## Installation
```bash
nimbus plugin install transmit
```

## Configuration
```go
// config/transmit.go
transmit.Config{
    Path:      "/__transmit",       // SSE endpoint
    Transport: "memory",            // "memory" or "redis"
    Redis:     redis.Options{...},  // Required for multi-instance
}
```

## Server-Side: Broadcasting

### Channel Model
```go
// Broadcast to a channel
transmit.Broadcast("notifications/user-42", map[string]any{
    "type":    "new_order",
    "message": "Order #123 received",
})

// Broadcast to all connected clients
transmit.BroadcastAll(map[string]any{"type": "maintenance", "eta": "5min"})
```

### Auth Hooks
```go
// Restrict channel subscriptions
transmit.AuthorizeChannel("private/*", func(ctx context.Context, channel string) bool {
    user := auth.UserFromContext(ctx)
    return user != nil
})
```

## Client-Side: Subscribing
```javascript
// JavaScript client
const source = new EventSource('/__transmit?channels=notifications/user-42');
source.onmessage = (event) => {
    const data = JSON.parse(event.data);
    console.log(data.type, data.message);
};
```

## Transport
- **Memory**: Single-instance only, zero config
- **Redis**: Multi-instance fan-out via Redis Pub/Sub

## Plugin Capabilities
Implements `HasRoutes`, `HasConfig`, `HasBindings`, `HasShutdown`.
