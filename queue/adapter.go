/*
|--------------------------------------------------------------------------
| Queue Adapter Interface
|--------------------------------------------------------------------------
|
| Adapters implement job storage and retrieval. Sync runs immediately;
| Redis, Database, SQS persist jobs for distributed workers.
|
*/

package queue

import (
	"context"
	"errors"
	"time"
)

// Adapter enqueues and dequeues jobs for processing.
type Adapter interface {
	// Push adds a job to the queue. delay is 0 for immediate.
	Push(ctx context.Context, payload *JobPayload) error

	// Pop blocks until a job is available or ctx is done. Returns nil when done.
	Pop(ctx context.Context, queue string) (*JobPayload, error)

	// Len returns approximate number of pending jobs (best-effort).
	Len(ctx context.Context, queue string) (int, error)
}

// CompletableAdapter optionally deletes/acks a message after successful processing (e.g. SQS).
type CompletableAdapter interface {
	Adapter
	Complete(ctx context.Context, payload *JobPayload) error
}

// ErrLeaseLost is returned by LeaseExtender.ExtendLease when the job's lease
// already expired and the job may have been handed to another worker.
var ErrLeaseLost = errors.New("queue: job lease lost")

// LeaseExtender is implemented by adapters whose deliveries are leased for a
// limited time (Redis visibility timeout, database lease). While a job runs,
// the Manager calls ExtendLease every LeaseDuration()/3 so long jobs are not
// handed to a second worker, while a crashed worker's jobs still come back
// after one lease period.
type LeaseExtender interface {
	LeaseDuration() time.Duration
	ExtendLease(ctx context.Context, payload *JobPayload) error
}

// JobPayload is the serialized form of a job for storage.
type JobPayload struct {
	ID         string                 `json:"id"`
	JobName    string                 `json:"job"`
	Queue      string                 `json:"queue"`
	Payload    []byte                 `json:"payload"`
	Attempts   int                    `json:"attempts"`
	MaxRetries int                    `json:"max_retries"`
	Delay      time.Duration          `json:"delay"`
	RunAt      time.Time              `json:"run_at"`
	Meta       map[string]interface{} `json:"meta,omitempty"`
}
