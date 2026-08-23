# Scheduler - Nimbus

Nimbus allows you to define cron-like scheduled tasks directly in your application code, avoiding the need for complex external crontab management.

## Defining Tasks

Scheduled tasks are registered in `start/schedule.go`.

```go
func RegisterSchedule(s *schedule.Scheduler) {
    s.EveryMinute("health-check", func(ctx context.Context) error {
        // Run health check...
        return nil
    })

    s.Daily("03:00", "daily-report", func(ctx context.Context) error {
        // Generate and send daily report...
        return nil
    })
}
```

## Running the Scheduler

Use the `schedule:run` CLI command to start the scheduler process.

```bash
nimbus schedule:run
```

## Features

-   **Named Tasks**: Each task has a unique name for identification and listing (`schedule:list`).
-   **Fluent Interval API**: Use methods like `EveryMinute()`, `EveryFiveMinutes()`, `Hourly()`, and `Daily("HH:MM")`.
-   **Recovery**: Tasks automatically recover from panics.
-   **Graceful Shutdown**: The scheduler stops cleanly upon receiving termination signals.

## Best Practices

1.  **Idempotency**: Ensure tasks can safely be re-run in case of a scheduler restart.
2.  **Logging**: Use the `logger` package to track task completion and errors.
3.  **Offload Heavy Tasks**: Use the scheduler to dispatch background jobs (`queue`) rather than executing heavy logic inline.
4.  **Separate Process**: Run the scheduler in its own container or process in production.
