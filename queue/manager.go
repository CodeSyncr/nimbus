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
	"fmt"
	"reflect"
	"sync"
	"time"

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
	noop       bool // true when no global manager
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
	return &JobPayload{
		ID:         uuid.New().String(),
		JobName:    jobName(b.job),
		Queue:      b.queue,
		Payload:    data,
		Attempts:   0,
		MaxRetries: b.maxRetries,
		Delay:      b.delay,
		RunAt:      runAt,
		Meta:       map[string]interface{}{"priority": b.priority},
	}, nil
}

// Process pops a job from the adapter, deserializes, and runs it.
func (m *Manager) Process(ctx context.Context, queue string) error {
	payload, err := m.adapter.Pop(ctx, queue)
	if err != nil || payload == nil {
		return err
	}
	ack := func() {
		if ca, ok := m.adapter.(CompletableAdapter); ok {
			_ = ca.Complete(ctx, payload)
		}
	}
	job, err := m.deserialize(payload)
	if err != nil {
		ack()
		notifyProcessed(payload, 0, err)
		return err
	}
	start := time.Now()
	err = job.Handle(ctx)
	duration := time.Since(start)
	if err != nil {
		payload.Attempts++
		if payload.Attempts <= payload.MaxRetries {
			retryDelay := nextRetryDelay(payload.Attempts, payload.Delay)
			payload.Delay = retryDelay
			payload.RunAt = time.Now().Add(retryDelay)
			if pushErr := m.adapter.Push(ctx, payload); pushErr != nil {
				return fmt.Errorf("queue: retry requeue failed: %w", pushErr)
			}
			notifyRetried(payload, retryDelay)
			ack()
			return nil
		}
		if fj, ok := job.(FailedJob); ok {
			fj.Failed(ctx, err)
		}
		// Laravel Horizon: record in failed job store for dashboard (list/forget/retry)
		if store := GetFailedJobStore(); store != nil {
			_ = store.Push(ctx, payload, err.Error())
		}
		notifyProcessed(payload, duration, err)
		ack()
		return err
	}
	ack()
	notifyProcessed(payload, duration, nil)
	return nil
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
