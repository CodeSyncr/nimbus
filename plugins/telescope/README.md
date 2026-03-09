# Telescope Plugin for Nimbus

Telescope provides insight into your local Nimbus development environment, inspired by [Laravel Telescope](https://laravel.com/docs/telescope).

## Features

- **Request watcher** — Records HTTP requests (method, path, status, duration)
- **Dashboard** — View recent activity at `/telescope`
- **In-memory storage** — Ring buffer (configurable max entries)

## Installation

```go
// bin/server.go
import "github.com/CodeSyncr/nimbus/plugins/telescope"

app.Use(telescope.New())
```

Add the request watcher middleware in `start/kernel.go`:

```go
if te := app.Plugin("telescope"); te != nil {
    if t, ok := te.(*telescope.Plugin); ok {
        app.Router.Use(t.RequestWatcher())
    }
}
```

## Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `TELESCOPE_ENABLED` | Enable in production | `false` |
| `TELESCOPE_PATH` | Dashboard URL path | `/telescope` |

## Dashboard

Access the dashboard at `http://localhost:3333/telescope` (or your app URL + `/telescope`).

- **Requests** — HTTP request log with status, method, path, duration
- **Exceptions** — (Coming soon) Panics and errors
- **Queries** — (Coming soon) Database query log
- **Logs** — (Coming soon) Application log entries

## Security

Telescope is disabled in production by default. Set `TELESCOPE_ENABLED=true` to enable in production, and restrict access via authorization.
