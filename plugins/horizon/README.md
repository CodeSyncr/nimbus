# Horizon Plugin for Nimbus (Laravel Horizon 1:1 style)

Horizon provides a queue dashboard and code-driven configuration for Nimbus,
inspired by [Laravel Horizon](https://laravel.com/docs/horizon). It surfaces
queue metrics, failed job management (list/forget/retry), and optional
dashboard authorization.

## Features

- **Dashboard** at `/horizon`: dispatched/processed/failed counts, per-queue stats, failed jobs list
- **Failed jobs** (when Redis is configured): list, forget one, forget all, retry — persisted in Redis
- **Dashboard authorization**: optional `Gate` so only allowed users can access Horizon in production
- **Code-driven config**: environments, supervisors, balance strategy, tries, timeout, backoff (for future worker scaling)
- **Job interfaces**: optional `Tags() []string` and `Silenced() bool` on jobs for Laravel-style tagging and silencing
- **JSON API**: `/horizon/metrics`, `/horizon/failed` (list), POST `/horizon/failed/:id/forget`, `/horizon/failed/forget-all`, `/horizon/failed/:id/retry`

## Installation

```go
import "github.com/CodeSyncr/nimbus/plugins/horizon"

// Basic (in-memory stats only)
app.Use(horizon.New())

// With Redis failed store and dashboard gate (Laravel-style)
app.Use(horizon.NewWithOptions(horizon.Options{
    RedisURL: os.Getenv("REDIS_URL"),
    Gate: func(c *http.Context) bool {
        // e.g. only allow admin users
        return c.User() != nil && c.User().Email == "admin@example.com"
    },
}))
```

Ensure `queue.Boot()` is called (e.g. in `bin/server.go`) and that jobs are registered. Use Redis as the queue driver to persist failed jobs.

## Configuration

| Variable          | Description                              | Default     |
|-------------------|------------------------------------------|-------------|
| `HORIZON_ENABLED` | Allow dashboard in production            | `false`     |
| `HORIZON_PATH`    | Dashboard base path                      | `/horizon`  |
| `REDIS_URL`       | Used by Options.RedisURL for failed store| —           |

Without `RedisURL`, the dashboard still shows in-memory stats but failed jobs are not persisted (no list/forget/retry).

## Horizon Config (workers)

Use `Config` for Laravel-style worker configuration (environments, supervisors, tries, timeout, backoff). Pass it when creating the plugin or use `DefaultConfig()`:

```go
cfg := horizon.DefaultConfig()
// Customize: cfg.Environments["production"].Supervisors["supervisor-1"].MaxProcesses = 20
app.Use(horizon.NewWithOptions(horizon.Options{ Config: &cfg, RedisURL: os.Getenv("REDIS_URL") }))
```

See `config.go` for `SupervisorConfig` (Balance: "auto"|"simple"|"false", Processes, MinProcesses, MaxProcesses, Tries, Timeout, Backoff).

## CLI (Laravel-style)

From your app root:

- `nimbus horizon forget [id]` — forget a failed job by ID
- `nimbus horizon forget --all` — forget all failed jobs
- `nimbus horizon clear [--queue=name]` — clear pending jobs from a queue (default: `default`)

These delegate to `go run . horizon forget ...` / `go run . horizon clear ...`. Your app must handle these in `main.go` so that the Horizon plugin and queue are booted, then call the failed store or queue clear. Example for `horizon forget`:

```go
if len(os.Args) >= 3 && os.Args[1] == "horizon" && os.Args[2] == "forget" {
    app := bin.Boot()
    if err := app.Boot(); err != nil { os.Exit(1) }
    store := queue.GetFailedJobStore()
    if store == nil { fmt.Println("Failed job store not configured"); os.Exit(1) }
    ctx := context.Background()
    if len(os.Args) > 3 && os.Args[3] == "--all" {
        _ = store.ForgetAll(ctx)
        fmt.Println("All failed jobs forgotten.")
    } else if len(os.Args) > 3 {
        _ = store.Forget(ctx, os.Args[3])
        fmt.Println("Forgotten:", os.Args[3])
    }
    return
}
```

Similar handling for `horizon clear` (use `queue.GetGlobal().Adapter()` and clear the queue if the adapter supports it, or document that clear is best-effort).

## Job tags and silenced

Implement on your job type:

- `Tags() []string` — for Horizon dashboard (e.g. `return []string{"mail", "user:" + strconv.FormatUint(uint64(j.UserID), 10)}`)
- `Silenced() bool` — return `true` to hide from completed jobs list (config or per-job)

## Metrics API

- `GET /horizon` — HTML dashboard
- `GET /horizon/metrics` — JSON snapshot of stats
- `GET /horizon/failed` — JSON list of failed jobs (when Redis store configured)
- `POST /horizon/failed/:id/forget` — forget one
- `POST /horizon/failed/forget-all` — forget all
- `POST /horizon/failed/:id/retry` — re-queue and forget

All routes respect the authorization `Gate` when set.
