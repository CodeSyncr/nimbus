# Transmit Plugin for Nimbus

Server-Sent Events (SSE) for real-time server-to-client push. Inspired by [AdonisJS Transmit](https://docs.adonisjs.com/guides/digging-deeper/server-sent-events).

## Installation

Transmit is a default plugin when creating a new app with `nimbus new`. To add manually:

```bash
nimbus add transmit
```

Or in `bin/server.go`:

```go
import "github.com/CodeSyncr/nimbus/plugins/transmit"

app.Use(transmit.New(nil))
```

## Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `TRANSMIT_PATH` | Route prefix | `__transmit` |
| `TRANSMIT_PING_INTERVAL` | Keep-alive ping (e.g. `30s`, `1m`) | disabled |
| `TRANSMIT_TRANSPORT` | Multi-instance: `redis` | none |
| `REDIS_URL` | Redis URL (for transport) | `redis://localhost:6379` |
| `TRANSMIT_REDIS_CHANNEL` | Redis pub/sub channel | `transmit::broadcast` |

### Code-based config

```go
app.Use(transmit.New(&transmit.Config{
    Path:         "__transmit",
    PingInterval: "30s",
    Middleware:   []router.Middleware{authMiddleware},  // optional
    Transport:    transmit.NewRedisTransport(transmit.RedisTransportConfig{}),  // multi-instance
}))
```

## Routes

| Route | Method | Purpose |
|-------|--------|---------|
| `__transmit/events` | GET | Establishes SSE connection |
| `__transmit/subscribe` | POST | Subscribe to channel |
| `__transmit/unsubscribe` | POST | Unsubscribe from channel |

## Usage

### Broadcasting

```go
import "github.com/CodeSyncr/nimbus/plugins/transmit"

func createPost(c *http.Context) error {
    post := createPostFromRequest(c)
    transmit.Broadcast("posts", map[string]any{
        "id":    post.ID,
        "title": post.Title,
    })
    return c.JSON(201, post)
}

// Exclude sender from receiving their own message
transmit.BroadcastExcept("chats/1/messages", data, senderUID)
```

### Channel authorization

Restrict access to private channels:

```go
import (
    "fmt"
    "github.com/CodeSyncr/nimbus/http"
    "github.com/CodeSyncr/nimbus/plugins/transmit"
)

func init() {
    transmit.Authorize("users/:id", func(ctx *http.Context, params map[string]string) bool {
        userID, ok := ctx.Get("user_id")  // from your auth middleware
        return ok && fmt.Sprint(userID) == params["id"]
    })
}
```

### Lifecycle hooks

```go
transmit.OnConnect(func(uid string) { log.Printf("Client %s connected", uid) })
transmit.OnDisconnect(func(uid string) { log.Printf("Client %s disconnected", uid) })
transmit.OnSubscribe(func(uid, channel string) { /* ... */ })
transmit.OnUnsubscribe(func(uid, channel string) { /* ... */ })
transmit.OnBroadcast(func(channel string, payload any) { /* ... */ })
```

### Get subscribers

```go
uids := transmit.GetSubscribers("chats/1/messages")
```

## Client setup

Clients connect to `GET /__transmit/events`, receive a UID in the first message, then POST to subscribe:

```javascript
const es = new EventSource('/__transmit/events');
es.onmessage = (e) => {
  const data = JSON.parse(e.data);
  if (data.uid) {
    fetch('/__transmit/subscribe', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ channel: 'notifications', uid: data.uid })
    });
  } else {
    console.log('Message:', data);
  }
};
```

Or use [@adonisjs/transmit-client](https://www.npmjs.com/package/@adonisjs/transmit-client) with `baseUrl` pointing to your Nimbus server.

## Multi-instance (Redis transport)

When running multiple instances behind a load balancer, set `TRANSMIT_TRANSPORT=redis` and `REDIS_URL`. Broadcasts will sync across all instances via Redis Pub/Sub.

## Production

**Disable compression for `text/event-stream`** in your reverse proxy:

- **Nginx:** Exclude `text/event-stream` from `gzip_types`
- **Traefik:** `excludedcontenttypes=text/event-stream`
