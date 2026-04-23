# Reverb (WebSocket broadcasting)

Laravel **Reverb**–style WebSocket channels for Nimbus. Pairs with **Transmit** (SSE): use SSE for one-way HTTP-friendly streams, and **Reverb** when you need interactive WebSocket subscriptions.

## Install

```bash
nimbus plugin install reverb
```

Or in `bin/server.go`:

```go
import "github.com/CodeSyncr/nimbus/plugins/reverb"

app.Use(reverb.New(nil))
```

## Configure

| Env | Purpose |
|-----|---------|
| `REDIS_URL` | Cross-instance fan-out (Pub/Sub). Without it, broadcasts are **single-process only**. |
| `REVERB_PATH` | WebSocket route (default `/reverb/ws`) |
| `REVERB_ALLOWED_ORIGINS` | Comma-separated `Origin` hosts allowed to upgrade (default: same host as request) |
| `REVERB_REDIS_CHANNEL` | Override Redis channel (default `nimbus:reverb:fanout`) |

## Server → clients

```go
import "context"

_ = reverb.Broadcast(context.Background(), "orders.1", map[string]any{
    "status": "shipped",
})
```

## Browser protocol

1. `GET` WebSocket `ws://host/reverb/ws` (or your `REVERB_PATH`).
2. Send JSON text frames:

```json
{"action":"subscribe","channel":"orders.1"}
{"action":"unsubscribe","channel":"orders.1"}
{"action":"ping"}
```

3. Server events:

- `connected` — handshake ok  
- `subscribed` / `unsubscribed`  
- `message` — `{ "event":"message", "channel":"...", "data": ... }`  
- `pong`

## Health

`GET` `{base}/health` (e.g. `/reverb/health` when WS path is `/reverb/ws`) returns `{"reverb":true,"redis_fanout":...}`.

## Advanced

`reverb.GetHub()` returns the live `*Hub` (or `nil`) if you need direct access.

## Optional gate

```go
reverb.New(&reverb.Config{
    Gate: func(c *http.Context) bool { return c.Auth != nil },
})
```

Prefer route-level middleware for session auth when possible.
