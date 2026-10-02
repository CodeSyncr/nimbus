/*
|--------------------------------------------------------------------------
| Queue Manager
|--------------------------------------------------------------------------
|
| Holds adapters, job registry, and provides Dispatch. Jobs are serialized
| to JSON and pushed to the adapter. Workers pop, deserialize, and run.
|
*/

package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"reflect"
	"sync"
	"time"

	"github.com/CodeSyncr/nimbus/tracing"
	"github.com/google/uuid"
)

var (
	globalManager *Manager
	globalMu      sync.RWMutex
)

// ObserverV2 is an optional extension interface for richer job lifecycle metadata.
// Observers can implement this in addition to Observer.
type ObserverV2 interface {
	JobProcessedV2(payload *JobPayload, duration time.Duration, err error)
}

// Observer can be used to observe queue lifecycle events (for dashboards
// like Horizon). It is optional and only called when set.
type Observer interface {
	JobDispatched(payload *JobPayload)
	JobProcessed(payload *JobPayload, err error)
}

var (
	observerMu sync.RWMutex
	observers  []Observer
)

// SetObserver replaces the observer list with a single observer (Horizon).
func SetObserver(o Observer) {
	observerMu.Lock()
	defer observerMu.Unlock()
	if o == nil {
		observers = nil
		return
	}
	observers = []Observer{o}
}

// AddObserver appends an observer (e.g. Telescope alongside Horizon).
func AddObserver(o Observer) {
	if o == nil {
		return
	}
	observerMu.Lock()
	defer observerMu.Unlock()
	observers = append(observers, o)
}

func eachObserver(fn func(Observer)) {
	observerMu.RLock()
	list := append([]Observer(nil), observers...)
	observerMu.RUnlock()
	for _, o := range list {
		if o != nil {
			fn(o)
		}
	}
}

func notifyProcessed(payload *JobPayload, duration time.Duration, err error) {
	eachObserver(func(o Observer) {
		if v2, ok := o.(ObserverV2); ok {
			v2.JobProcessedV2(payload, duration, err)
			return
		}
		o.JobProcessed(payload, err)
	})
}

// getObserver returns the first observer for legacy single-observer call sites.
func getObserver() Observer {
	observerMu.RLock()
	defer observerMu.RUnlock()
	if len(observers) == 0 {
		return nil
	}
	return observers[0]
}

// Manager manages adapters and job dispatch.
type Manager struct {
	adapter  Adapter
	registry map[string]func() Job
	mu       sync.RWMutex
}

// NewManager creates a manager with the given adapter. Pass nil to use SyncAdapter.
func NewManager(adapter Adapter) *Manager {
	m := &Manager{
		adapter:  adapter,
		registry: make(map[string]func() Job),
	}
	if adapter == nil {
		m.adapter = NewSyncAdapter(m)
	}
	m.registry["WithoutOverlapping"] = func() Job { return &WithoutOverlapping{manager: m} }
	return m
}

// Adapter returns the underlying queue adapter (for Horizon retry, etc.).
func (m *Manager) Adapter() Adapter { return m.adapter }

// Register registers a job type for deserialization. Call with a zero-value instance.
//
//	queue.Register(&jobs.SendEmail{})
func (m *Manager) Register(job Job) {
	m.mu.Lock()
	defer m.mu.Unlock()
	name := jobName(job)
	m.registry[name] = func() Job {
		return newJobInstance(job)
	}
}

// RegisterFunc registers a job by name with a constructor.
func (m *Manager) RegisterFunc(name string, fn func() Job) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.registry[name] = fn
}

// Dispatch enqueues a job. Returns a DispatchBuilder for options.
func (m *Manager) Dispatch(job Job) *DispatchBuilder {
	return &DispatchBuilder{
		manager:    m,
		job:        job,
		queue:      "default",
		delay:      0,
		maxRetries: 3,
	}
}

// DispatchBuilder allows chaining dispatch options.
type DispatchBuilder struct {
	manager    *Manager
	job        Job
	queue      string
	delay      time.Duration
	maxRetries int
	priority   int
	meta       map[string]interface{}
	noop       bool // true when no global manager
}

func (b *DispatchBuilder) withMeta(key string, value interface{}) *DispatchBuilder {
	if b.meta == nil {
		b.meta = map[string]interface{}{}
	}
	b.meta[key] = value
	return b
}

// OnQueue sets the queue name.
func (b *DispatchBuilder) OnQueue(name string) *DispatchBuilder {
	b.queue = name
	return b
}

// Delay sets the delay before the job runs.
func (b *DispatchBuilder) Delay(d time.Duration) *DispatchBuilder {
	b.delay = d
	return b
}

// Retries sets max retry attempts.
func (b *DispatchBuilder) Retries(n int) *DispatchBuilder {
	b.maxRetries = n
	return b
}

// Priority sets job priority (1=highest, 10=lowest).
func (b *DispatchBuilder) Priority(n int) *DispatchBuilder {
	b.priority = n
	return b
}

// Dispatch executes the dispatch.
func (b *DispatchBuilder) Dispatch(ctx context.Context) error {
	if b.noop || b.manager == nil {
		return nil
	}
	payload, err := b.serialize()
	if err != nil {
		return err
	}
	// Carry the caller's trace to the worker.
	if tracing.SpanContextFromContext(ctx).IsValid() {
		var span *tracing.Span
		ctx, span = tracing.Start(ctx, "queue publish "+payload.JobName, tracing.WithKind(tracing.KindProducer),
			tracing.WithAttrs(map[string]any{"messaging.destination.name": payload.Queue, "messaging.message.id": payload.ID}))
		defer span.End()
		payload.Meta[metaTraceparent] = span.SpanContext().Traceparent()
	}
	if err := b.manager.adapter.Push(ctx, payload); err != nil {
		return err
	}
	eachObserver(func(o Observer) { o.JobDispatched(payload) })
	return nil
}

func (b *DispatchBuilder) serialize() (*JobPayload, error) {
	data, err := json.Marshal(b.job)
	if err != nil {
		return nil, fmt.Errorf("queue: marshal job: %w", err)
	}
	runAt := time.Now()
	if b.delay > 0 {
		runAt = runAt.Add(b.delay)
	}
	meta := map[string]interface{}{"priority": b.priority}
	for k, v := range b.meta {
		meta[k] = v
	}
	return &JobPayload{
		ID:         uuid.New().String(),
		JobName:    jobName(b.job),
		Queue:      b.queue,
		Payload:    data,
		Attempts:   0,
		MaxRetries: b.maxRetries,
		Delay:      b.delay,
		RunAt:      runAt,
		Meta:       meta,
	}, nil
}

// ReleaseError tells the worker to put the job back on the queue after
// Delay without counting an attempt. Return it from Handle with Release.
type ReleaseError struct {
	Delay time.Duration
}

func (e *ReleaseError) Error() string {
	return fmt.Sprintf("queue: job released for %s", e.Delay)
}

// Release returns an error that puts the job back on the queue after delay
// without counting it as a failed attempt.
func Release(delay time.Duration) error {
	return &ReleaseError{Delay: delay}
}

// transientMetaKeys are delivery-specific and never copied onto a requeued job.
var transientMetaKeys = []string{metaRedisLeaseToken, metaDBClaimToken, "sqs_receipt_handle",
	"redis_processing_key", "redis_inflight_key", "redis_raw_payload"}

func requeueCopy(p *JobPayload, delay time.Duration) *JobPayload {
	cp := *p
	cp.Meta = make(map[string]interface{}, len(p.Meta))
	for k, v := range p.Meta {
		cp.Meta[k] = v
	}
	for _, k := range transientMetaKeys {
		delete(cp.Meta, k)
	}
	cp.Delay = delay
	cp.RunAt = time.Now().Add(delay)
	return &cp
}

func (m *Manager) isSync() bool {
	_, ok := m.adapter.(*SyncAdapter)
	return ok
}

// Process pops a job from the adapter, deserializes, and runs it.
func (m *Manager) Process(ctx context.Context, queue string) error {
	payload, err := m.adapter.Pop(ctx, queue)
	if err != nil || payload == nil {
		return err
	}
	// Acks and requeues must land even when the worker is shutting down.
	bg := context.WithoutCancel(ctx)
	ack := func() {
		if ca, ok := m.adapter.(CompletableAdapter); ok {
			_ = ca.Complete(bg, payload)
		}
	}
	job, err := m.deserialize(payload)
	if err != nil {
		notifyProcessed(payload, 0, err)
		m.finish(bg, payload, nil, err)
		ack()
		return err
	}
	if payload.Attempts > payload.MaxRetries {
		// Earlier deliveries were lost (the worker crashed or stalled past
		// its lease) often enough to use up the job's retries.
		err = fmt.Errorf("queue: job %q exceeded %d retries: its worker was lost or timed out", payload.JobName, payload.MaxRetries)
		m.fail(bg, job, payload, 0, err)
		ack()
		return err
	}

	ctx, span := m.startSpan(ctx, payload)
	defer span.End()
	bg = context.WithoutCancel(ctx) // so chained jobs continue the trace

	stopHeartbeat := m.heartbeat(ctx, payload)
	start := time.Now()
	err = m.handle(ctx, job)
	duration := time.Since(start)
	stopHeartbeat()
	if err != nil {
		span.RecordError(err)
	}

	var rel *ReleaseError
	if errors.As(err, &rel) {
		if pushErr := m.adapter.Push(bg, requeueCopy(payload, rel.Delay)); pushErr != nil {
			return fmt.Errorf("queue: release requeue failed: %w", pushErr)
		}
		ack()
		return nil
	}
	if err != nil {
		payload.Attempts++
		if payload.Attempts <= payload.MaxRetries {
			retryDelay := nextRetryDelay(payload.Attempts, payload.Delay)
			retry := requeueCopy(payload, retryDelay)
			if pushErr := m.adapter.Push(bg, retry); pushErr != nil {
				return fmt.Errorf("queue: retry requeue failed: %w", pushErr)
			}
			notifyRetried(retry, retryDelay)
			ack()
			return nil
		}
		m.fail(bg, job, payload, duration, err)
		ack()
		return err
	}
	notifyProcessed(payload, duration, nil)
	m.finish(bg, payload, job, nil)
	ack()
	return nil
}

const metaTraceparent = "traceparent"

// startSpan opens the consumer span for a job, continuing the trace of the
// request that dispatched it when there is one.
func (m *Manager) startSpan(ctx context.Context, payload *JobPayload) (context.Context, *tracing.Span) {
	opts := []tracing.StartOption{
		tracing.WithKind(tracing.KindConsumer),
		tracing.WithAttrs(map[string]any{
			"messaging.destination.name": payload.Queue,
			"messaging.message.id":       payload.ID,
			"messaging.message.attempt":  payload.Attempts + 1,
		}),
	}
	if tp, _ := payload.Meta[metaTraceparent].(string); tp != "" {
		if sc, err := tracing.ParseTraceparent(tp); err == nil {
			opts = append(opts, tracing.WithParent(sc))
		}
	}
	return tracing.Start(ctx, "queue process "+payload.JobName, opts...)
}

// handle runs the job, holding its overlap lock if it has one.
func (m *Manager) handle(ctx context.Context, job Job) error {
	no, ok := job.(NonOverlapping)
	if !ok {
		return job.Handle(ctx)
	}
	key := overlapLockKey(job, no.OverlapKey())
	expiry, release := overlapTimings(job)
	locker := GetLocker()
	token, got, err := locker.Acquire(ctx, key, expiry)
	if err != nil {
		return fmt.Errorf("queue: overlap lock: %w", err)
	}
	if !got {
		return Release(release)
	}
	defer func() { _ = locker.Release(context.WithoutCancel(ctx), key, token) }()
	return job.Handle(context.WithValue(ctx, overlapHeldKey{}, key))
}

// fail records a job that will not be retried.
func (m *Manager) fail(ctx context.Context, job Job, payload *JobPayload, duration time.Duration, err error) {
	if fj, ok := job.(FailedJob); ok {
		fj.Failed(ctx, err)
	}
	// Laravel Horizon: record in failed job store for dashboard (list/forget/retry)
	if store := GetFailedJobStore(); store != nil {
		_ = store.Push(ctx, payload, err.Error())
	}
	notifyProcessed(payload, duration, err)
	m.finish(ctx, payload, job, err)
}

// finish runs once per job when it succeeded or failed for good: it frees
// the unique lock, advances an async chain and counts it toward its batch.
func (m *Manager) finish(ctx context.Context, payload *JobPayload, job Job, jobErr error) {
	if payload.Meta == nil {
		return
	}
	releaseUniqueFromMeta(ctx, payload)
	if err := m.continueChain(ctx, payload, job, jobErr); err != nil {
		log.Printf("[queue] chain after job %s: %v", payload.ID, err)
	}
	if id, _ := payload.Meta[metaBatchID].(string); id != "" {
		m.recordBatch(ctx, id, payload.ID, job, jobErr)
	}
}

// heartbeat keeps the job's lease alive while it runs, for adapters whose
// deliveries expire. The returned func stops it.
func (m *Manager) heartbeat(ctx context.Context, payload *JobPayload) func() {
	le, ok := m.adapter.(LeaseExtender)
	if !ok {
		return func() {}
	}
	lease := le.LeaseDuration()
	if lease <= 0 {
		return func() {}
	}
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		t := time.NewTicker(lease / 3)
		defer t.Stop()
		bg := context.WithoutCancel(ctx)
		for {
			select {
			case <-done:
				return
			case <-t.C:
				err := le.ExtendLease(bg, payload)
				if errors.Is(err, ErrLeaseLost) {
					log.Printf("[queue] job %s (%s) lost its lease while running; it may run again elsewhere", payload.ID, payload.JobName)
					return
				}
				if err != nil {
					log.Printf("[queue] extend lease for job %s: %v", payload.ID, err)
				}
			}
		}
	}()
	return func() {
		close(done)
		wg.Wait()
	}
}

func nextRetryDelay(attempt int, base time.Duration) time.Duration {
	if base <= 0 {
		base = time.Second
	}
	if attempt < 1 {
		attempt = 1
	}
	delay := base
	for i := 1; i < attempt; i++ {
		if delay >= time.Minute {
			delay = time.Minute
			break
		}
		delay *= 2
		if delay > time.Minute {
			delay = time.Minute
			break
		}
	}
	// Add small jitter so retries do not stampede at once.
	jitter := time.Duration(time.Now().UnixNano()%int64(250*time.Millisecond) + 1)
	return delay + jitter
}

func (m *Manager) deserialize(p *JobPayload) (Job, error) {
	m.mu.RLock()
	fn, ok := m.registry[p.JobName]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("queue: unknown job %q (register with queue.Register)", p.JobName)
	}
	job := fn()
	if err := json.Unmarshal(p.Payload, job); err != nil {
		return nil, fmt.Errorf("queue: unmarshal job %q: %w", p.JobName, err)
	}
	return job, nil
}

func jobName(job Job) string {
	t := reflect.TypeOf(job)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t.Name()
}

func newJobInstance(job Job) Job {
	t := reflect.TypeOf(job)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
		return reflect.New(t).Interface().(Job)
	}
	return reflect.New(t).Elem().Interface().(Job)
}

// SetGlobal sets the global manager.
func SetGlobal(m *Manager) {
	globalMu.Lock()
	defer globalMu.Unlock()
	globalManager = m
}

// GetGlobal returns the global manager.
func GetGlobal() *Manager {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return globalManager
}
