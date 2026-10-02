package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/CodeSyncr/nimbus/tracing"
	"github.com/google/uuid"
)

// Meta keys the Manager uses to carry chain, batch and lock state on a job.
const (
	metaChain       = "chain"
	metaChainName   = "chain_name"
	metaBatchID     = "batch_id"
	metaUniqueKey   = "unique_key"
	metaUniqueToken = "unique_token"
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
	name      string
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

// Named names the chain. DispatchAsync needs a name when OnFailure is set,
// so workers can find the callback (see RegisterChain).
func (c *Chain) Named(name string) *Chain {
	c.name = name
	return c
}

// OnFailure sets a callback when a chain job fails.
func (c *Chain) OnFailure(fn func(ctx context.Context, failedJob Job, err error)) *Chain {
	c.onFailure = fn
	return c
}

// Dispatch runs the chain sequentially in the calling goroutine.
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

// chainLink is a not-yet-dispatched job of an async chain.
type chainLink struct {
	Job     string          `json:"job"`
	Payload json.RawMessage `json:"payload"`
}

// DispatchAsync queues the first job; each job's worker queues the next one
// when it succeeds. Every job must be registered with queue.Register.
//
// OnFailure runs on the worker that saw the failure, so register it there
// with RegisterChain under the chain's name.
func (c *Chain) DispatchAsync(ctx context.Context) error {
	m := GetGlobal()
	if m == nil {
		return fmt.Errorf("queue: no global manager set")
	}
	if len(c.jobs) == 0 {
		return nil
	}
	if c.onFailure != nil {
		if c.name == "" {
			return errors.New("queue: chain with OnFailure needs Named(...) to dispatch async")
		}
		RegisterChain(c.name, c.onFailure)
	}
	links := make([]chainLink, 0, len(c.jobs)-1)
	for _, job := range c.jobs[1:] {
		data, err := json.Marshal(job)
		if err != nil {
			return fmt.Errorf("queue: marshal chain job %q: %w", jobName(job), err)
		}
		links = append(links, chainLink{Job: jobName(job), Payload: data})
	}
	b := m.Dispatch(c.jobs[0]).OnQueue(c.queue)
	if err := setChainMeta(b, links, c.name); err != nil {
		return err
	}
	return b.Dispatch(ctx)
}

func setChainMeta(b *DispatchBuilder, links []chainLink, name string) error {
	if len(links) > 0 {
		data, err := json.Marshal(links)
		if err != nil {
			return err
		}
		b.withMeta(metaChain, string(data))
	}
	if name != "" {
		b.withMeta(metaChainName, name)
	}
	return nil
}

var (
	chainFailuresMu sync.RWMutex
	chainFailures   = map[string]func(ctx context.Context, failedJob Job, err error){}
)

// RegisterChain registers the failure callback for async chains with this
// name. Call it at boot in every worker process.
func RegisterChain(name string, onFailure func(ctx context.Context, failedJob Job, err error)) {
	chainFailuresMu.Lock()
	defer chainFailuresMu.Unlock()
	if onFailure == nil {
		delete(chainFailures, name)
		return
	}
	chainFailures[name] = onFailure
}

// continueChain queues the next link of an async chain after its job
// succeeded, or runs the chain's failure callback after it failed for good.
func (m *Manager) continueChain(ctx context.Context, payload *JobPayload, job Job, jobErr error) error {
	raw, _ := payload.Meta[metaChain].(string)
	name, _ := payload.Meta[metaChainName].(string)
	if jobErr != nil {
		if name == "" {
			return nil
		}
		chainFailuresMu.RLock()
		fn := chainFailures[name]
		chainFailuresMu.RUnlock()
		if fn != nil {
			fn(ctx, job, jobErr)
		} else {
			log.Printf("[queue] chain %q failed but no failure callback is registered in this process (queue.RegisterChain)", name)
		}
		return nil
	}
	if raw == "" {
		return nil
	}
	var links []chainLink
	if err := json.Unmarshal([]byte(raw), &links); err != nil || len(links) == 0 {
		return err
	}
	next := &JobPayload{
		ID:         uuid.New().String(),
		JobName:    links[0].Job,
		Queue:      payload.Queue,
		Payload:    links[0].Payload,
		MaxRetries: payload.MaxRetries,
		RunAt:      time.Now(),
		Meta:       map[string]interface{}{},
	}
	if len(links) > 1 {
		data, err := json.Marshal(links[1:])
		if err != nil {
			return err
		}
		next.Meta[metaChain] = string(data)
	}
	if name != "" {
		next.Meta[metaChainName] = name
	}
	if sc := tracing.SpanContextFromContext(ctx); sc.IsValid() {
		next.Meta[metaTraceparent] = sc.Traceparent()
	}
	if err := m.adapter.Push(ctx, next); err != nil {
		return fmt.Errorf("queue: dispatch next chain job: %w", err)
	}
	eachObserver(func(o Observer) { o.JobDispatched(next) })
	return nil
}

// ══════════════════════════════════════════════════════════════════
// Job Batches — run jobs concurrently, track progress
// ══════════════════════════════════════════════════════════════════

// Batch dispatches a group of jobs and runs callbacks when they finish.
//
// With a real queue driver (redis, database, ...), Dispatch queues every job
// and returns; workers record progress in the BatchStore and whichever one
// finishes the last job runs the callbacks. Callbacks are found by batch
// name, so give the batch a name and register its callbacks at boot in
// every worker process with RegisterBatch. With the sync driver (or no
// manager), Dispatch runs the jobs concurrently in-process, like Run.
type Batch struct {
	ID          string
	name        string
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

// BatchCallbacks are the callbacks of a named batch.
type BatchCallbacks struct {
	// Then runs once when every job finished and none failed.
	Then func(ctx context.Context, batch *Batch)
	// Catch runs for each job that fails for good.
	Catch func(ctx context.Context, batch *Batch, err error)
	// Finally runs once when every job finished, failed or not.
	Finally func(ctx context.Context, batch *Batch)
}

var (
	batchCallbacksMu sync.RWMutex
	batchCallbacks   = map[string]BatchCallbacks{}
)

// RegisterBatch registers callbacks for batches with this name. Call it at
// boot in every process that runs queue workers.
func RegisterBatch(name string, cb BatchCallbacks) {
	batchCallbacksMu.Lock()
	defer batchCallbacksMu.Unlock()
	batchCallbacks[name] = cb
}

func lookupBatchCallbacks(name string) (BatchCallbacks, bool) {
	batchCallbacksMu.RLock()
	defer batchCallbacksMu.RUnlock()
	cb, ok := batchCallbacks[name]
	return cb, ok
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

// Named names the batch. Workers look callbacks up by this name.
func (b *Batch) Named(name string) *Batch {
	b.name = name
	return b
}

// Name returns the batch name.
func (b *Batch) Name() string { return b.name }

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

// Refresh reloads a durable batch's counters from the BatchStore.
func (b *Batch) Refresh(ctx context.Context) error {
	st, err := GetBatchStore().Find(ctx, b.ID)
	if err != nil {
		return err
	}
	b.apply(*st)
	return nil
}

func (b *Batch) apply(st BatchState) {
	b.name = st.Name
	atomic.StoreInt32(&b.totalJobs, int32(st.Total))
	atomic.StoreInt32(&b.pendingJobs, int32(max(st.Pending, 0)))
	atomic.StoreInt32(&b.failedJobs, int32(st.Failed))
	b.mu.Lock()
	b.errors = b.errors[:0]
	for _, e := range st.Errors {
		b.errors = append(b.errors, errors.New(e))
	}
	b.mu.Unlock()
}

func (b *Batch) callbacks() BatchCallbacks {
	return BatchCallbacks{Then: b.then, Catch: b.catch, Finally: b.finally}
}

func (b *Batch) hasCallbacks() bool {
	return b.then != nil || b.catch != nil || b.finally != nil
}

// Dispatch queues the batch's jobs (see Batch). It returns once every job
// is queued; use FindBatch or Refresh to follow progress.
func (b *Batch) Dispatch(ctx context.Context) error {
	m := GetGlobal()
	if m == nil || m.isSync() {
		return b.Run(ctx)
	}
	if b.hasCallbacks() {
		if b.name == "" {
			return errors.New("queue: batch with Then/Catch/Finally needs Named(...) so workers can find its callbacks (register them with queue.RegisterBatch at boot)")
		}
		RegisterBatch(b.name, b.callbacks())
	}

	total := len(b.jobs)
	atomic.StoreInt32(&b.totalJobs, int32(total))
	atomic.StoreInt32(&b.pendingJobs, int32(total))
	store := GetBatchStore()
	if err := store.Create(ctx, BatchState{
		ID:        b.ID,
		Name:      b.name,
		Total:     total,
		Pending:   total,
		CreatedAt: time.Now(),
	}); err != nil {
		return fmt.Errorf("queue: create batch: %w", err)
	}
	if total == 0 {
		b.finish(ctx, b.callbacks())
		return nil
	}
	for i, job := range b.jobs {
		err := m.Dispatch(job).OnQueue(b.queue).withMeta(metaBatchID, b.ID).Dispatch(ctx)
		if err != nil {
			// Count jobs that never made it onto the queue as failed, so the
			// batch still finishes once the queued ones do.
			err = fmt.Errorf("queue: dispatch batch job %q: %w", jobName(job), err)
			for j := i; j < total; j++ {
				m.recordBatch(ctx, b.ID, fmt.Sprintf("undispatched-%d", j), nil, err)
			}
			return err
		}
	}
	return nil
}

// Run runs all jobs in the batch concurrently in this process and blocks
// until they finish. Nothing is persisted.
func (b *Batch) Run(ctx context.Context) error {
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
	b.finish(ctx, b.callbacks())
	return nil
}

func (b *Batch) finish(ctx context.Context, cb BatchCallbacks) {
	if !b.HasFailures() && cb.Then != nil {
		cb.Then(ctx, b)
	}
	if cb.Finally != nil {
		cb.Finally(ctx, b)
	}
	runAfterBatchHooks(ctx, b)
}

// recordBatch counts a batch job's final outcome and, on the worker that
// finished the batch, runs its callbacks.
func (m *Manager) recordBatch(ctx context.Context, batchID, jobID string, job Job, jobErr error) {
	upd, err := GetBatchStore().Record(ctx, batchID, jobID, jobErr)
	if err != nil {
		log.Printf("[queue] batch %s: record job %s: %v", batchID, jobID, err)
		return
	}
	if !upd.Counted {
		return
	}
	b := &Batch{ID: batchID}
	b.apply(upd.State)
	cb, ok := lookupBatchCallbacks(upd.State.Name)
	if !ok && upd.State.Name != "" && (jobErr != nil || upd.Finished) {
		log.Printf("[queue] batch %q has no callbacks registered in this process (queue.RegisterBatch)", upd.State.Name)
	}
	if jobErr != nil && cb.Catch != nil {
		name := "job"
		if job != nil {
			name = fmt.Sprintf("job %q", jobName(job))
		}
		cb.Catch(ctx, b, fmt.Errorf("%s: %w", name, jobErr))
	}
	if upd.Finished {
		b.finish(ctx, cb)
	}
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

	// UniqueFor returns the longest the unique lock is held. The lock is
	// released earlier when the job finishes (succeeds or fails for good).
	// Zero or less means one hour.
	UniqueFor() time.Duration
}

func uniqueLockKey(job UniqueJob) string {
	return "nimbus:unique:" + jobName(job) + ":" + job.UniqueID()
}

func uniqueTTL(job UniqueJob) time.Duration {
	if d := job.UniqueFor(); d > 0 {
		return d
	}
	return time.Hour
}

// IsUniqueLocked reports whether a job with the same UniqueID is pending.
func IsUniqueLocked(job UniqueJob) bool {
	ctx := context.Background()
	key := uniqueLockKey(job)
	token, ok, err := GetLocker().Acquire(ctx, key, time.Second)
	if err != nil || !ok {
		return err == nil
	}
	_ = GetLocker().Release(ctx, key, token)
	return false
}

// AcquireUniqueLock takes the unique lock for a job until UniqueFor passes
// or ReleaseUniqueLock is called.
func AcquireUniqueLock(job UniqueJob) bool {
	_, ok, err := GetLocker().Acquire(context.Background(), uniqueLockKey(job), uniqueTTL(job))
	return err == nil && ok
}

// ReleaseUniqueLock releases the unique lock for a job.
func ReleaseUniqueLock(job UniqueJob) {
	_ = GetLocker().Release(context.Background(), uniqueLockKey(job), "")
}

// DispatchUnique dispatches a job unless one with the same UniqueID is
// already pending. The lock lives in the configured Locker (Redis or the
// database with those drivers), so it holds across instances.
func DispatchUnique(ctx context.Context, job UniqueJob) error {
	m := GetGlobal()
	if m == nil {
		return fmt.Errorf("queue: no global manager set")
	}
	key := uniqueLockKey(job)
	token, ok, err := GetLocker().Acquire(ctx, key, uniqueTTL(job))
	if err != nil {
		return fmt.Errorf("queue: unique lock: %w", err)
	}
	if !ok {
		return nil // Already queued, skip silently
	}
	err = m.Dispatch(job).withMeta(metaUniqueKey, key).withMeta(metaUniqueToken, token).Dispatch(ctx)
	if err != nil && !m.isSync() {
		_ = GetLocker().Release(ctx, key, token) // Release on dispatch failure
		return err
	}
	return err
}

func releaseUniqueFromMeta(ctx context.Context, payload *JobPayload) {
	key, _ := payload.Meta[metaUniqueKey].(string)
	token, _ := payload.Meta[metaUniqueToken].(string)
	if key != "" && token != "" {
		_ = GetLocker().Release(ctx, key, token)
	}
}

// ══════════════════════════════════════════════════════════════════
// WithoutOverlapping — prevent concurrent execution
// ══════════════════════════════════════════════════════════════════

// NonOverlapping is implemented by jobs that must not run at the same time
// as another job with the same OverlapKey. When a worker picks one up while
// another is running, it puts it back on the queue (without counting an
// attempt) and tries again later. The lock lives in the configured Locker,
// so it holds across workers and instances.
type NonOverlapping interface {
	Job
	OverlapKey() string
}

// OverlapOptions optionally tunes a NonOverlapping job.
type OverlapOptions interface {
	// OverlapExpiresAfter bounds how long the lock is held if the worker
	// dies mid-job. Default 10 minutes.
	OverlapExpiresAfter() time.Duration
	// OverlapReleaseAfter is how long a blocked job waits before it is
	// tried again. Default 3 seconds.
	OverlapReleaseAfter() time.Duration
}

const (
	defaultOverlapExpiry  = 10 * time.Minute
	defaultOverlapRelease = 3 * time.Second
)

type overlapHeldKey struct{}

func overlapLockKey(job Job, key string) string {
	return "nimbus:overlap:" + jobName(job) + ":" + key
}

func overlapTimings(job Job) (expiry, release time.Duration) {
	expiry, release = defaultOverlapExpiry, defaultOverlapRelease
	if o, ok := job.(OverlapOptions); ok {
		if d := o.OverlapExpiresAfter(); d > 0 {
			expiry = d
		}
		if d := o.OverlapReleaseAfter(); d > 0 {
			release = d
		}
	}
	return expiry, release
}

// WithoutOverlapping wraps a Job so that only one job with the same
// uniqueID runs at a time, across all workers. Dispatch it like any job;
// the inner job must be registered with queue.Register.
type WithoutOverlapping struct {
	inner        Job
	uniqueID     string
	expireAfter  time.Duration
	releaseAfter time.Duration
	manager      *Manager
}

// NewWithoutOverlapping wraps a job to prevent overlapping execution.
func NewWithoutOverlapping(job Job, uniqueID string) *WithoutOverlapping {
	return &WithoutOverlapping{
		inner:    job,
		uniqueID: uniqueID,
	}
}

// ExpireAfter bounds how long the lock is held if a worker dies mid-job.
func (w *WithoutOverlapping) ExpireAfter(d time.Duration) *WithoutOverlapping {
	w.expireAfter = d
	return w
}

// ReleaseAfter sets how long a blocked job waits before it is tried again.
func (w *WithoutOverlapping) ReleaseAfter(d time.Duration) *WithoutOverlapping {
	w.releaseAfter = d
	return w
}

// Inner returns the wrapped job.
func (w *WithoutOverlapping) Inner() Job { return w.inner }

// OverlapKey implements NonOverlapping.
func (w *WithoutOverlapping) OverlapKey() string {
	return jobName(w.inner) + ":" + w.uniqueID
}

// OverlapExpiresAfter implements OverlapOptions.
func (w *WithoutOverlapping) OverlapExpiresAfter() time.Duration { return w.expireAfter }

// OverlapReleaseAfter implements OverlapOptions.
func (w *WithoutOverlapping) OverlapReleaseAfter() time.Duration { return w.releaseAfter }

// Handle runs the inner job. Workers take the lock before calling Handle;
// called directly, Handle waits for the lock itself.
func (w *WithoutOverlapping) Handle(ctx context.Context) error {
	if w.inner == nil {
		return errors.New("queue: WithoutOverlapping has no inner job")
	}
	if ctx.Value(overlapHeldKey{}) != nil {
		return w.inner.Handle(ctx)
	}
	key := overlapLockKey(w, w.OverlapKey())
	expiry, _ := overlapTimings(w)
	locker := GetLocker()
	for {
		token, ok, err := locker.Acquire(ctx, key, expiry)
		if err != nil {
			return err
		}
		if ok {
			defer func() { _ = locker.Release(context.WithoutCancel(ctx), key, token) }()
			return w.inner.Handle(ctx)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

type withoutOverlappingJSON struct {
	Job          string          `json:"job"`
	ID           string          `json:"id"`
	Payload      json.RawMessage `json:"payload"`
	ExpireAfter  time.Duration   `json:"expire_after,omitempty"`
	ReleaseAfter time.Duration   `json:"release_after,omitempty"`
}

// MarshalJSON lets the wrapper travel through a queue.
func (w *WithoutOverlapping) MarshalJSON() ([]byte, error) {
	if w.inner == nil {
		return nil, errors.New("queue: WithoutOverlapping has no inner job")
	}
	data, err := json.Marshal(w.inner)
	if err != nil {
		return nil, err
	}
	return json.Marshal(withoutOverlappingJSON{
		Job:          jobName(w.inner),
		ID:           w.uniqueID,
		Payload:      data,
		ExpireAfter:  w.expireAfter,
		ReleaseAfter: w.releaseAfter,
	})
}

// UnmarshalJSON rebuilds the inner job from the manager's job registry.
func (w *WithoutOverlapping) UnmarshalJSON(data []byte) error {
	var v withoutOverlappingJSON
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	m := w.manager
	if m == nil {
		m = GetGlobal()
	}
	if m == nil {
		return errors.New("queue: WithoutOverlapping needs a manager to decode its inner job")
	}
	m.mu.RLock()
	fn, ok := m.registry[v.Job]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("queue: unknown job %q (register with queue.Register)", v.Job)
	}
	inner := fn()
	if err := json.Unmarshal(v.Payload, inner); err != nil {
		return fmt.Errorf("queue: unmarshal job %q: %w", v.Job, err)
	}
	w.inner, w.uniqueID = inner, v.ID
	w.expireAfter, w.releaseAfter = v.ExpireAfter, v.ReleaseAfter
	return nil
}

var _ NonOverlapping = (*WithoutOverlapping)(nil)
var _ OverlapOptions = (*WithoutOverlapping)(nil)
