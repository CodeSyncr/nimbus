package queue

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// ══════════════════════════════════════════════════════════════════
// Job Chains — run jobs sequentially, stop on failure
// ══════════════════════════════════════════════════════════════════

// Chain dispatches a sequence of jobs one after another.
// If any job in the chain fails (exhausts retries), the rest are skipped
// and the optional OnFailure callback is called.
type Chain struct {
	jobs      []Job
	queue     string
	onFailure func(ctx context.Context, failedJob Job, err error)
}

// NewChain creates a new job chain.
func NewChain(jobs ...Job) *Chain {
	return &Chain{
		jobs:  jobs,
		queue: "default",
	}
}

// OnQueue sets the queue for all jobs in the chain.
func (c *Chain) OnQueue(name string) *Chain {
	c.queue = name
	return c
}

// OnFailure sets a callback when a chain job fails.
func (c *Chain) OnFailure(fn func(ctx context.Context, failedJob Job, err error)) *Chain {
	c.onFailure = fn
	return c
}

// Dispatch executes the chain sequentially.
func (c *Chain) Dispatch(ctx context.Context) error {
	m := GetGlobal()
	if m == nil {
		return fmt.Errorf("queue: no global manager set")
	}

	for _, job := range c.jobs {
		err := job.Handle(ctx)
		if err != nil {
			if c.onFailure != nil {
				c.onFailure(ctx, job, err)
			}
			return fmt.Errorf("queue chain: job %q failed: %w", jobName(job), err)
		}
	}
	return nil
}

// DispatchAsync dispatches the chain as a single wrapper job via queue.
func (c *Chain) DispatchAsync(ctx context.Context) error {
	wrapper := &chainJob{
		chain: c,
	}
	m := GetGlobal()
	if m == nil {
		return fmt.Errorf("queue: no global manager set")
	}
	return m.Dispatch(wrapper).OnQueue(c.queue).Dispatch(ctx)
}

type chainJob struct {
	chain *Chain
}

func (j *chainJob) Handle(ctx context.Context) error {
	return j.chain.Dispatch(ctx)
}

// ══════════════════════════════════════════════════════════════════
// Job Batches — run jobs concurrently, track progress
// ══════════════════════════════════════════════════════════════════

// Batch allows dispatching a group of jobs and tracking their
// completion, with optional callbacks.
type Batch struct {
	ID          string
	jobs        []Job
	queue       string
	then        func(ctx context.Context, batch *Batch)
	catch       func(ctx context.Context, batch *Batch, err error)
	finally     func(ctx context.Context, batch *Batch)
	totalJobs   int32
	pendingJobs int32
	failedJobs  int32
	mu          sync.Mutex
	errors      []error
}

// NewBatch creates a new job batch.
func NewBatch(jobs ...Job) *Batch {
	return &Batch{
		ID:    uuid.New().String(),
		jobs:  jobs,
		queue: "default",
	}
}

// OnQueue sets the queue for all jobs in the batch.
func (b *Batch) OnQueue(name string) *Batch {
	b.queue = name
	return b
}

// Then sets a callback for when all non-failed jobs complete.
func (b *Batch) Then(fn func(ctx context.Context, batch *Batch)) *Batch {
	b.then = fn
	return b
}

// Catch sets a callback for when any job in the batch fails.
func (b *Batch) Catch(fn func(ctx context.Context, batch *Batch, err error)) *Batch {
	b.catch = fn
	return b
}

// Finally sets a callback that runs after all jobs complete (success or failure).
func (b *Batch) Finally(fn func(ctx context.Context, batch *Batch)) *Batch {
	b.finally = fn
	return b
}

// TotalJobs returns the total number of jobs in the batch.
func (b *Batch) TotalJobs() int32 { return atomic.LoadInt32(&b.totalJobs) }

// PendingJobs returns the number of jobs still pending.
func (b *Batch) PendingJobs() int32 { return atomic.LoadInt32(&b.pendingJobs) }

// FailedJobs returns the number of failed jobs.
func (b *Batch) FailedJobs() int32 { return atomic.LoadInt32(&b.failedJobs) }

// Finished returns true when all jobs have completed.
func (b *Batch) Finished() bool { return atomic.LoadInt32(&b.pendingJobs) == 0 }

// HasFailures returns true if any jobs failed.
func (b *Batch) HasFailures() bool { return atomic.LoadInt32(&b.failedJobs) > 0 }

// Errors returns all errors from failed jobs.
func (b *Batch) Errors() []error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.errors
}

// Dispatch runs all jobs in the batch concurrently.
func (b *Batch) Dispatch(ctx context.Context) error {
	total := int32(len(b.jobs))
	atomic.StoreInt32(&b.totalJobs, total)
	atomic.StoreInt32(&b.pendingJobs, total)

	var wg sync.WaitGroup
	wg.Add(int(total))

	for _, job := range b.jobs {
		go func(j Job) {
			defer wg.Done()
			err := j.Handle(ctx)
			if err != nil {
				atomic.AddInt32(&b.failedJobs, 1)
				b.mu.Lock()
				b.errors = append(b.errors, fmt.Errorf("job %q: %w", jobName(j), err))
				b.mu.Unlock()
				if b.catch != nil {
					b.catch(ctx, b, err)
				}
			}
			atomic.AddInt32(&b.pendingJobs, -1)
		}(job)
	}

	wg.Wait()

	if !b.HasFailures() && b.then != nil {
		b.then(ctx, b)
	}
	if b.finally != nil {
		b.finally(ctx, b)
	}

	return nil
}

// ══════════════════════════════════════════════════════════════════
// Unique Jobs — prevent duplicate jobs from being queued
// ══════════════════════════════════════════════════════════════════

// UniqueJob interface — jobs implementing this will be deduplicated.
type UniqueJob interface {
	Job

	// UniqueID returns a unique identifier for this job instance.
	// Jobs with the same UniqueID will not be dispatched while one is pending.
	UniqueID() string

	// UniqueFor returns the duration to hold the unique lock.
	// After this duration, the same job can be dispatched again.
	UniqueFor() time.Duration
}

// uniqueJobLocks is an in-memory lock store. For production, use Redis or DB.
var (
	uniqueJobLocks   = make(map[string]time.Time)
	uniqueJobLocksMu sync.Mutex
)

// IsUniqueLocked checks if a unique job is currently locked.
func IsUniqueLocked(job UniqueJob) bool {
	uniqueJobLocksMu.Lock()
	defer uniqueJobLocksMu.Unlock()

	key := jobName(job) + ":" + job.UniqueID()
	if expiry, ok := uniqueJobLocks[key]; ok {
		if time.Now().Before(expiry) {
			return true
		}
		// Expired, clean up
		delete(uniqueJobLocks, key)
	}
	return false
}

// AcquireUniqueLock acquires the unique lock for a job.
func AcquireUniqueLock(job UniqueJob) bool {
	uniqueJobLocksMu.Lock()
	defer uniqueJobLocksMu.Unlock()

	key := jobName(job) + ":" + job.UniqueID()
	if expiry, ok := uniqueJobLocks[key]; ok {
		if time.Now().Before(expiry) {
			return false // Already locked
		}
	}
	uniqueJobLocks[key] = time.Now().Add(job.UniqueFor())
	return true
}

// ReleaseUniqueLock releases the unique lock for a job.
func ReleaseUniqueLock(job UniqueJob) {
	uniqueJobLocksMu.Lock()
	defer uniqueJobLocksMu.Unlock()
	key := jobName(job) + ":" + job.UniqueID()
	delete(uniqueJobLocks, key)
}

// DispatchUnique dispatches a job only if it's not already queued.
func DispatchUnique(ctx context.Context, job UniqueJob) error {
	if !AcquireUniqueLock(job) {
		return nil // Already queued, skip silently
	}

	m := GetGlobal()
	if m == nil {
		return fmt.Errorf("queue: no global manager set")
	}

	err := m.Dispatch(job).Dispatch(ctx)
	if err != nil {
		ReleaseUniqueLock(job) // Release on dispatch failure
		return err
	}
	return nil
}

// ══════════════════════════════════════════════════════════════════
// WithoutOverlapping — prevent concurrent execution
// ══════════════════════════════════════════════════════════════════

// WithoutOverlapping wraps a Job so that concurrent executions of the
// same job (by UniqueID) are prevented. Only one instance runs at a time.
type WithoutOverlapping struct {
	inner    Job
	uniqueID string
	mu       sync.Mutex
}

// NewWithoutOverlapping wraps a job to prevent overlapping execution.
func NewWithoutOverlapping(job Job, uniqueID string) *WithoutOverlapping {
	return &WithoutOverlapping{
		inner:    job,
		uniqueID: uniqueID,
	}
}

// Handle runs the inner job if no other instance with the same ID is running.
func (w *WithoutOverlapping) Handle(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.inner.Handle(ctx)
}
