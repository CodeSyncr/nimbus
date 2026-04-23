# Queue Package for Nimbus

Background job processing (Laravel-inspired). Queue is **core**—no plugin needed. Call `queue.Boot()` in your app bootstrap.

## Installation

Queue is initialized in `bin/server.go` when creating a new app. Ensure you have:

```go
import "github.com/CodeSyncr/nimbus/queue"

queue.Boot(&queue.BootConfig{RegisterJobs: start.RegisterQueueJobs})
```

## Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `QUEUE_DRIVER` | `sync`, `redis`, `database`, `sqs`, `kafka` | `sync` |
| `REDIS_URL` | Redis URL (for redis driver) | `redis://localhost:6379` |
| `QUEUE_REDIS_VISIBILITY_TIMEOUT_SECONDS` | Redis in-flight lease timeout before reclaim | `60` |
| `QUEUE_DB_LEASE_SECONDS` | Database processing lease before reclaim | `120` |
| `QUEUE_BOOT_STRICT` | Fail boot on unknown driver values | `false` |
| `SQS_QUEUE_URL` | AWS SQS queue URL | — |
| `KAFKA_BROKERS` | Kafka brokers (comma-separated) | — |
| `KAFKA_TOPIC` | Kafka topic | `nimbus-queue` |
| `KAFKA_GROUP_ID` | Consumer group ID | `nimbus-queue` |

**Drivers:**
- `sync` — Runs jobs immediately (no worker). Useful for dev.
- `redis` — Redis lists. Requires `REDIS_URL`.
- `database` — GORM. Uses `database.Get()`.
- `sqs` — AWS SQS.
- `kafka` — Apache Kafka.

## Defining jobs

Implement the `queue.Job` interface:

```go
package jobs

import (
    "context"
    "github.com/CodeSyncr/nimbus/queue"
)

type SendEmail struct {
    UserID  int
    Subject string
}

func (j *SendEmail) Handle(ctx context.Context) error {
    // Send email...
    return nil
}
```

Optional `Failed` for cleanup when job fails permanently:

```go
func (j *SendEmail) Failed(ctx context.Context, err error) {
    log.Printf("SendEmail failed for user %d: %v", j.UserID, err)
}
```

## Dispatching jobs

```go
import "github.com/CodeSyncr/nimbus/queue"

queue.Dispatch(&jobs.SendEmail{UserID: 12, Subject: "Welcome"}).Dispatch(ctx)

// Delayed
queue.Dispatch(&jobs.SendEmail{...}).Delay(5 * time.Minute).Dispatch(ctx)

// Specific queue
queue.Dispatch(&jobs.Report{}).OnQueue("reports").Dispatch(ctx)
```

## Registering jobs

In `start/jobs.go` (or equivalent):

```go
package start

import (
    "github.com/CodeSyncr/nimbus/queue"
    "myapp/jobs"
)

func RegisterQueueJobs() {
    queue.Register(&jobs.SendEmail{})
    queue.Register(&jobs.ProcessVideo{})
}
```

## Running the worker

```bash
nimbus queue:work
```

Or from your app:

```go
queue.RunWorker(ctx, "default")
```

## Rate limiting

Pass `RateLimitPerSec` and `RateLimitBurst` in `BootConfig` to throttle job processing.

## Production guide (recommended defaults)

Use this section as a starting baseline for reliable queue processing.

### 1) Use a durable driver in production

- Prefer `redis` or `database` (avoid `sync` in production).
- Run multiple workers (`nimbus queue:work`) behind a process supervisor.
- Set `QUEUE_BOOT_STRICT=true` to fail fast on invalid queue driver config.

### 2) Tune lease / visibility timeouts

- Redis (`QUEUE_REDIS_VISIBILITY_TIMEOUT_SECONDS`): start at `60`.
- Database (`QUEUE_DB_LEASE_SECONDS`): start at `120`.
- Rule of thumb: timeout should be at least `2x` your p95 job runtime.
- Too low: duplicate work from premature reclaim.
- Too high: slow recovery when workers crash.

You can also set these in code:

```go
queue.Boot(&queue.BootConfig{
    Driver:                 "redis",
    RedisURL:               "redis://localhost:6379",
    RedisVisibilityTimeout: 60 * time.Second,
    DatabaseLeaseDuration:  120 * time.Second,
    RegisterJobs:           start.RegisterQueueJobs,
})
```

### 3) Retry policy

- Default retry backoff is exponential with jitter.
- Keep retries bounded (`Retries(n)` per job).
- Ensure job handlers are idempotent (safe to run more than once).

### 4) Observe the right signals

Nimbus now exports queue counters (via Horizon metrics):

- `nimbus_queue_jobs_dispatched_total`
- `nimbus_queue_jobs_processed_total`
- `nimbus_queue_jobs_failed_total`
- `nimbus_queue_jobs_retried_total`
- `nimbus_queue_jobs_reclaimed_total`

Prometheus-formatted endpoint (when Horizon is enabled):

- `GET /horizon/api/metrics/prometheus`

### 5) Suggested alert thresholds (starting point)

- **Retry spike:** retried/processed ratio > 5% for 5-10 minutes.
- **Reclaim activity:** reclaimed > 0 sustained for 10+ minutes.
- **Failure rate:** failed/processed ratio > 1-2% for 5+ minutes.
- **Backlog growth:** queue length rising continuously without recovery.

Tune thresholds per workload after collecting a week of baseline data.
