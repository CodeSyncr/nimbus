# Pulse Plugin for Nimbus

Pulse is a lightweight production-friendly monitoring plugin for Nimbus.
It tracks aggregated request, query, queue, cache, and exception metrics.

## Install

```go
import "github.com/CodeSyncr/nimbus/plugins/pulse"

app.Use(pulse.NewPlugin())
```

## Routes

- `GET /pulse` — dashboard page
- `GET /pulse/metrics` — JSON metrics snapshot
- `GET /pulse/entries` — recent ring-buffer entries

## Middleware

Use the named middleware `pulse` (registered by the plugin) to record request timing:

```go
app.Router.Use(app.NamedMiddleware("pulse"))
```

Or wire directly if needed:

```go
p := pulse.NewPlugin()
app.Router.Use(p.Pulse.Middleware())
```

## Configuration

Defaults from `DefaultConfig()`:

- `max_entries`: `10000`
- `slow_query_threshold`: `100ms`
- `slow_request_threshold`: `500ms`

Use `pulse.NewPlugin(pulse.Config{...})` for custom thresholds.
