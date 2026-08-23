# Horizon Plugin (Queue Dashboard)

Horizon provides a web-based dashboard for monitoring and managing queue workers.

## Installation
```bash
nimbus plugin install horizon
```

## Features
- Real-time queue metrics (throughput, wait times)
- Failed job monitoring and retry
- Job search and filtering
- Worker status and scaling
- Historical statistics

## Configuration
```go
app.Use(horizon.New(horizon.Config{
    Path:     "/horizon",           // Dashboard URL
    Auth:     horizonAuthMiddleware, // Optional: restrict access
    Database: db,                   // For storing metrics
}))
```

## Dashboard Panels
| Panel | Description |
|-------|-------------|
| Overview | Active jobs, throughput, runtime |
| Recent Jobs | List of recent job executions |
| Failed Jobs | Failed jobs with error details, retry button |
| Workers | Active workers and their current jobs |
| Metrics | Historical charts (jobs/min, avg runtime) |

## Access Control
```go
func horizonAuthMiddleware(next router.HandlerFunc) router.HandlerFunc {
    return func(c *http.Context) error {
        user := auth.UserFromContext(c.Ctx())
        if user == nil || user.GetID() != "admin" {
            return c.JSON(403, map[string]string{"error": "forbidden"})
        }
        return next(c)
    }
}
```

## Plugin Capabilities
Implements `HasRoutes`, `HasViews`, `HasConfig`, `HasMigrations`.
