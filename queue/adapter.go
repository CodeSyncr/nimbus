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

// JobPayload is the serialized form of a job for storage.
type JobPayload struct {
	ID        string                 `json:"id"`
	JobName   string                 `json:"job"`
	Queue     string                 `json:"queue"`
	Payload   []byte                 `json:"payload"`
	Attempts  int                    `json:"attempts"`
	MaxRetries int                   `json:"max_retries"`
	Delay     time.Duration          `json:"delay"`
	RunAt     time.Time              `json:"run_at"`
	Meta      map[string]interface{} `json:"meta,omitempty"`
}
