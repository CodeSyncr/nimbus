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
| `QUEUE_REDIS_VISIBILITY_TIMEOUT_SECONDS` | How long a Redis job stays leased without a worker heartbeat | `60` |
| `QUEUE_DB_LEASE_SECONDS` | How long a database job stays leased without a worker heartbeat | `120` |
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

Pass `RateLimitPerSec` and `RateLimitBurst` in `BootConfig` to throttle job processing. The limit is per queue. With the Redis driver it is kept in Redis, so it holds across all workers and instances; with other drivers each worker process gets the full limit.

## Delivery guarantees

- With `redis` and `database`, each job is handed to one worker at a time. While a job runs, its worker renews the job's lease every third of the lease period, so long jobs are not picked up twice.
- If a worker dies, its job returns to the queue after one lease period and the lost run **counts as an attempt**. A job that keeps killing its worker fails for good once it uses up its retries, instead of looping forever.
- Delivery is still at least once (a network partition can outlast a lease), so keep handlers idempotent.
- Return `queue.Release(delay)` from `Handle` to put a job back without counting an attempt.
- Database driver: finished jobs are deleted, and retries reuse their job's row.

## Batches, chains, unique jobs, overlap

All four work across workers and instances with the `redis` and `database` drivers: `queue.Boot` keeps their locks and progress in the same backend.

```go
// At boot, in every process that runs workers:
queue.RegisterBatch("import-users", queue.BatchCallbacks{
    Then:    func(ctx context.Context, b *queue.Batch) { /* all succeeded */ },
    Catch:   func(ctx context.Context, b *queue.Batch, err error) { /* one job failed */ },
    Finally: func(ctx context.Context, b *queue.Batch) { /* all done */ },
})

// Anywhere: queues every job and returns. Progress: queue.FindBatch(ctx, b.ID)
b := queue.NewBatch(jobs...).Named("import-users")
err := b.Dispatch(ctx)

// Each job is queued by the worker that finished the previous one.
queue.NewChain(&Fetch{}, &Transform{}, &Load{}).DispatchAsync(ctx)

// Skips the dispatch while an identical job is pending (lock released when it finishes).
queue.DispatchUnique(ctx, &RebuildIndex{TenantID: 7}) // implements UniqueID() / UniqueFor()

// Only one at a time per key; a blocked job is put back and retried shortly.
queue.Dispatch(queue.NewWithoutOverlapping(&SyncAccount{ID: 7}, "account-7")).Dispatch(ctx)
```

Jobs can also implement `OverlapKey() string` (`queue.NonOverlapping`) instead of using the wrapper. With the `sync` driver (or no manager), `Batch.Dispatch` runs the jobs concurrently in-process; `Batch.Run` always does.

## Production guide (recommended defaults)

Use this section as a starting baseline for reliable queue processing.

### 1) Use a durable driver in production

- Prefer `redis` or `database` (avoid `sync` in production).
- Run multiple workers (`nimbus queue:work`) behind a process supervisor.
- Set `QUEUE_BOOT_STRICT=true` to fail fast on invalid queue driver config.

### 2) Tune lease / visibility timeouts

- Redis (`QUEUE_REDIS_VISIBILITY_TIMEOUT_SECONDS`): start at `60`.
- Database (`QUEUE_DB_LEASE_SECONDS`): start at `120`.
- Workers heartbeat running jobs, so the timeout does not need to cover job runtime: it is how long a crashed worker's job waits before another worker takes it.
- Too low: a worker stalled longer than the lease (long GC pause, network blip) can lose its job to another worker.
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
