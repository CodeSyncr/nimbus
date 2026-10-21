package testing

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/CodeSyncr/nimbus/queue"
)

// ── Fake Queue ──────────────────────────────────────────────────

// DispatchedJob records a job that was dispatched.
type DispatchedJob struct {
	Job   queue.Job // nil when the job type was not registered (see FakeJobs)
	Queue string
	Delay time.Duration
	// Payload is what a real adapter would have stored; nil for jobs
	// recorded with Push or PushToQueue.
	Payload *queue.JobPayload
}

// FakeQueue captures dispatched jobs instead of processing them.
type FakeQueue struct {
	mu   sync.Mutex
	Jobs []DispatchedJob

	manager  *queue.Manager // set by FakeJobs
	fallback *queue.Manager // the manager FakeJobs replaced
}

// NewFakeQueue returns a fresh fake queue. Use FakeJobs to also capture
// queue.Dispatch calls.
func NewFakeQueue() *FakeQueue {
	return &FakeQueue{}
}

// FakeJobs sends every queue.Dispatch to a fresh FakeQueue for the rest of
// the test and puts the previous global manager back when it ends. Jobs are
// serialized as a real adapter would store them, so a job that cannot be
// marshaled fails at Dispatch.
//
// Job types registered with queue.Register (after FakeJobs, or on the manager
// it replaced) come back as values in Dispatched and run with ProcessAll.
// JobsOf and AssertDispatched decode any job type without registering it.
//
//	jobs := nimbustesting.FakeJobs(t)
//	client.PostJSON("/signup", body)
//	nimbustesting.AssertDispatched(t, jobs, func(j *SendWelcome) bool { return j.Email == "a@b.co" })
func FakeJobs(t testing.TB) *FakeQueue {
	t.Helper()
	claim(t, "queue")
	f := NewFakeQueue()
	f.fallback = queue.GetGlobal()
	f.manager = queue.NewManager(fakeQueueAdapter{f})
	queue.SetGlobal(f.manager)
	t.Cleanup(func() { queue.SetGlobal(f.fallback) })
	return f
}

// Push captures the job with an empty queue name.
func (f *FakeQueue) Push(job queue.Job) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Jobs = append(f.Jobs, DispatchedJob{Job: job, Queue: ""})
}

// PushToQueue captures the job with the specified queue name.
func (f *FakeQueue) PushToQueue(queueName string, job queue.Job) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Jobs = append(f.Jobs, DispatchedJob{Job: job, Queue: queueName})
}

// Dispatched returns all captured jobs.
func (f *FakeQueue) Dispatched() []DispatchedJob {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]DispatchedJob, len(f.Jobs))
	copy(out, f.Jobs)
	return out
}

// DispatchedCount returns how many jobs were dispatched.
func (f *FakeQueue) DispatchedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.Jobs)
}

// ProcessAll runs all captured jobs synchronously (useful for testing side effects).
func (f *FakeQueue) ProcessAll(ctx context.Context) []error {
	jobs := f.Dispatched()
	var errs []error
	for _, d := range jobs {
		job := d.Job
		if job == nil {
			job = f.decode(d.Payload)
		}
		if job == nil {
			errs = append(errs, fmt.Errorf("nimbustesting: job %q is not registered; call queue.Register after FakeJobs", d.Payload.JobName))
			continue
		}
		if err := job.Handle(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// Reset clears all captured jobs.
func (f *FakeQueue) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Jobs = nil
}

// AssertNothingDispatched fails the test if any job was dispatched.
func (f *FakeQueue) AssertNothingDispatched(t testing.TB) {
	t.Helper()
	if n := f.DispatchedCount(); n > 0 {
		t.Errorf("expected no dispatched jobs, got %d", n)
	}
}

// AssertDispatchedCount fails the test unless exactly n jobs were dispatched.
func (f *FakeQueue) AssertDispatchedCount(t testing.TB, n int) {
	t.Helper()
	if got := f.DispatchedCount(); got != n {
		t.Errorf("expected %d dispatched jobs, got %d", n, got)
	}
}

// JobsOf returns the dispatched jobs of type T (for example *SendWelcome),
// decoded from what was queued.
func JobsOf[T queue.Job](f *FakeQueue) []T {
	var zero T
	if reflect.TypeOf(zero) == nil {
		return nil // T is an interface; there is no job name to match
	}
	name := queue.JobName(zero)
	var out []T
	for _, d := range f.Dispatched() {
		if j, ok := d.Job.(T); ok {
			out = append(out, j)
			continue
		}
		if d.Payload == nil || d.Payload.JobName != name {
			continue
		}
		if j, err := decodeAs[T](d.Payload.Payload); err == nil {
			out = append(out, j)
		}
	}
	return out
}

// AssertDispatched fails the test unless a job of type T was dispatched (and,
// when match is given, one of them matches it).
func AssertDispatched[T queue.Job](t testing.TB, f *FakeQueue, match ...func(T) bool) {
	t.Helper()
	jobs := JobsOf[T](f)
	if len(jobs) == 0 {
		t.Errorf("expected a dispatched %T job, got none", *new(T))
		return
	}
	if len(match) == 0 || anyMatch(jobs, match[0]) {
		return
	}
	t.Errorf("%d %T job(s) dispatched, none matched", len(jobs), *new(T))
}

// AssertNotDispatched fails the test if a job of type T was dispatched (that
// matches match, when given).
func AssertNotDispatched[T queue.Job](t testing.TB, f *FakeQueue, match ...func(T) bool) {
	t.Helper()
	jobs := JobsOf[T](f)
	if len(match) > 0 {
		var hit []T
		for _, j := range jobs {
			if match[0](j) {
				hit = append(hit, j)
			}
		}
		jobs = hit
	}
	if len(jobs) > 0 {
		t.Errorf("expected no dispatched %T job, got %d", *new(T), len(jobs))
	}
}

func (f *FakeQueue) decode(p *queue.JobPayload) queue.Job {
	if p == nil {
		return nil
	}
	for _, m := range []*queue.Manager{f.manager, f.fallback} {
		if m == nil {
			continue
		}
		if job, err := m.Decode(p); err == nil {
			return job
		}
	}
	return nil
}

// fakeQueueAdapter records pushes on the FakeQueue. FakeQueue cannot be the
// adapter itself: its Push(job) predates queue.Adapter's Push(ctx, payload).
type fakeQueueAdapter struct{ f *FakeQueue }

func (a fakeQueueAdapter) Push(_ context.Context, p *queue.JobPayload) error {
	d := DispatchedJob{Job: a.f.decode(p), Queue: p.Queue, Delay: p.Delay, Payload: p}
	a.f.mu.Lock()
	a.f.Jobs = append(a.f.Jobs, d)
	a.f.mu.Unlock()
	return nil
}

// Pop never hands out jobs: faked jobs run only through ProcessAll.
func (a fakeQueueAdapter) Pop(ctx context.Context, _ string) (*queue.JobPayload, error) {
	<-ctx.Done()
	return nil, nil
}

func (a fakeQueueAdapter) Len(_ context.Context, name string) (int, error) {
	n := 0
	for _, d := range a.f.Dispatched() {
		if d.Queue == name {
			n++
		}
	}
	return n, nil
}

func decodeAs[T any](data []byte) (T, error) {
	var v T
	if rt := reflect.TypeOf(v); rt != nil && rt.Kind() == reflect.Ptr {
		v = reflect.New(rt.Elem()).Interface().(T)
		return v, json.Unmarshal(data, v)
	}
	return v, json.Unmarshal(data, &v)
}

func anyMatch[T any](items []T, match func(T) bool) bool {
	for _, it := range items {
		if match(it) {
			return true
		}
	}
	return false
}
