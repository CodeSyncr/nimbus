package queue

import (
	"context"
	"sync"
)

// Queue is a legacy in-memory worker pool. Prefer queue.Dispatch() with
// a configured Manager (Redis/Database) for production.
type Queue struct {
	mu      sync.Mutex
	jobs    chan jobEntry
	workers int
	ctx     context.Context
	cancel  context.CancelFunc
}

type jobEntry struct {
	job Job
}

// NewQueue creates a queue with n workers. Start with Run().
func NewQueue(workers int) *Queue {
	return &Queue{
		jobs:    make(chan jobEntry, 1024),
		workers: workers,
	}
}

// Push enqueues a job (non-blocking if buffer full; can backpressure).
func (q *Queue) Push(job Job) {
	q.mu.Lock()
	defer q.mu.Unlock()
	select {
	case q.jobs <- jobEntry{job: job}:
	default:
		// buffer full; could block or return error
	}
}

// Run starts the worker pool. Call in a goroutine or block.
func (q *Queue) Run(ctx context.Context) {
	q.ctx, q.cancel = context.WithCancel(ctx)
	for i := 0; i < q.workers; i++ {
		go q.worker()
	}
}

func (q *Queue) worker() {
	for {
		select {
		case <-q.ctx.Done():
			return
		case e := <-q.jobs:
			_ = e.job.Handle(q.ctx)
		}
	}
}

// Stop stops the queue.
func (q *Queue) Stop() {
	if q.cancel != nil {
		q.cancel()
	}
}
