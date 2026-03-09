package queue

import (
	"context"
	"sync"
)

// SyncAdapter runs jobs immediately in the same process. No persistence.
// Use for development/testing when you don't need a separate worker.
type SyncAdapter struct {
	mu      sync.Mutex
	manager *Manager
}

// NewSyncAdapter creates a sync adapter. Requires manager for job deserialization.
func NewSyncAdapter(m *Manager) *SyncAdapter {
	return &SyncAdapter{manager: m}
}

// Push runs the job immediately by marshaling and processing.
func (s *SyncAdapter) Push(ctx context.Context, payload *JobPayload) error {
	job, err := s.manager.deserialize(payload)
	if err != nil {
		return err
	}
	return job.Handle(ctx)
}

// Pop always blocks (sync has no persisted jobs). Use Redis/Database for workers.
func (s *SyncAdapter) Pop(ctx context.Context, queue string) (*JobPayload, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// Len always returns 0.
func (s *SyncAdapter) Len(ctx context.Context, queue string) (int, error) {
	return 0, nil
}

var _ Adapter = (*SyncAdapter)(nil)
