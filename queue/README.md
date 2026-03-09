# Queue Package for Nimbus

Background job processing (Laravel/AdonisJS style). Queue is **core**—no plugin needed. Call `queue.Boot()` in your app bootstrap.

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
