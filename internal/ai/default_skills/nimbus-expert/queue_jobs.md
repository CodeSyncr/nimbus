# Queue & Jobs - Nimbus

Nimbus provides a robust background job processing system, allowing you to offload slow or non-critical tasks from the HTTP request cycle.

## Job Basics

A job is a Go struct that implements the `queue.Job` interface, primarily defining a `Handle(ctx)` method.

### Example Job

```go
type ProcessImage struct {
    ImageID uint
}

func (j *ProcessImage) Handle(ctx context.Context) error {
    // Process image logic...
    return nil
}

func (j *ProcessImage) Failed(ctx context.Context, err error) {
    // Handle persistent failure...
}
```

## Dispatching Jobs

Jobs are pushed to a queue using `queue.Dispatch()`.

```go
queue.Dispatch(&jobs.ProcessImage{ImageID: 42}).Dispatch(ctx)
```

## Drivers

-   **Memory**: Synchronous processing, ideal for local development.
-   **Redis**: High-performance, distributed processing for production.
-   **SQS**: Cloud-native queuing via Amazon SQS.

## Horizon Dashboard

Use the `horizon` plugin to monitor queue health, throughput, and failed jobs through a web interface.

## Job Registration

All jobs must be registered in `start/jobs.go` so the worker can deserialize and execute them.

```go
func RegisterQueueJobs() {
    queue.Register(&jobs.ProcessImage{})
}
```

## Best Practices

1.  **Idempotency**: Ensure jobs can be safely retried without side effects.
2.  **Small Payloads**: Serialize only necessary IDs instead of full model objects.
3.  **Retry Strategy**: Configure appropriate retry attempts for flaky external services.
4.  **Graceful Failure**: Implement the `Failed()` method to clean up or notify after ultimate failure.
