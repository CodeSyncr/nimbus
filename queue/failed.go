/*
|--------------------------------------------------------------------------
| Failed Jobs Store (Laravel Horizon style)
|--------------------------------------------------------------------------
|
| Stores permanently failed jobs for dashboard list, forget, and retry.
| Implementations: Redis (Horizon), optional DB later.
|
*/

package queue

import (
	"context"
	"sync"
	"time"
)

// FailedJobRecord is a single failed job entry for listing and retry.
type FailedJobRecord struct {
	ID         string    `json:"id"`
	UUID       string    `json:"uuid"`
	Queue      string    `json:"queue"`
	JobName    string    `json:"job_name"`
	Payload    []byte    `json:"payload"`
	Exception  string    `json:"exception"`
	FailedAt   time.Time `json:"failed_at"`
	Attempts   int       `json:"attempts"`
	MaxRetries int       `json:"max_retries"`
}

// FailedJobStore persists failed jobs for Horizon dashboard (list, forget, retry).
type FailedJobStore interface {
	// Push adds a failed job to the store.
	Push(ctx context.Context, payload *JobPayload, errMsg string) error
	// List returns all failed job records (e.g. for dashboard).
	List(ctx context.Context) ([]FailedJobRecord, error)
	// Get returns one record by ID/UUID.
	Get(ctx context.Context, id string) (*FailedJobRecord, error)
	// Forget removes a single failed job.
	Forget(ctx context.Context, id string) error
	// ForgetAll removes all failed jobs.
	ForgetAll(ctx context.Context) error
	// Retry re-enqueues the job and removes it from failed store.
	Retry(ctx context.Context, id string, enqueue func(ctx context.Context, payload *JobPayload) error) error
}

var (
	failedStoreMu sync.RWMutex
	failedStore   FailedJobStore
)

// SetFailedJobStore sets the global failed job store (e.g. from Horizon plugin).
func SetFailedJobStore(s FailedJobStore) {
	failedStoreMu.Lock()
	defer failedStoreMu.Unlock()
	failedStore = s
}

// GetFailedJobStore returns the global failed job store.
func GetFailedJobStore() FailedJobStore {
	failedStoreMu.RLock()
	defer failedStoreMu.RUnlock()
	return failedStore
}
