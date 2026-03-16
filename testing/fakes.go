package testing

import (
	"context"
	"sync"

	"github.com/CodeSyncr/nimbus/mail"
	"github.com/CodeSyncr/nimbus/queue"
)

// ── Fake Mail Driver ────────────────────────────────────────────

// FakeMailer captures sent emails for testing assertions.
// Implements mail.Driver.
type FakeMailer struct {
	mu    sync.Mutex
	Mails []*mail.Message
}

// NewFakeMailer returns a fresh fake mailer.
func NewFakeMailer() *FakeMailer {
	return &FakeMailer{}
}

// Send captures the message instead of sending it.
func (f *FakeMailer) Send(m *mail.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Mails = append(f.Mails, m)
	return nil
}

// Sent returns all captured messages.
func (f *FakeMailer) Sent() []*mail.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*mail.Message, len(f.Mails))
	copy(out, f.Mails)
	return out
}

// SentCount returns how many emails were sent.
func (f *FakeMailer) SentCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.Mails)
}

// SentTo returns all messages sent to the given email address.
func (f *FakeMailer) SentTo(addr string) []*mail.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	var result []*mail.Message
	for _, m := range f.Mails {
		for _, to := range m.To {
			if to == addr {
				result = append(result, m)
				break
			}
		}
	}
	return result
}

// HasSentTo returns true if any mail was sent to the given address.
func (f *FakeMailer) HasSentTo(addr string) bool {
	return len(f.SentTo(addr)) > 0
}

// Reset clears all captured messages.
func (f *FakeMailer) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Mails = nil
}

// ── Fake Queue ──────────────────────────────────────────────────

// DispatchedJob records a job that was dispatched.
type DispatchedJob struct {
	Job   queue.Job
	Queue string
}

// FakeQueue captures dispatched jobs instead of processing them.
type FakeQueue struct {
	mu   sync.Mutex
	Jobs []DispatchedJob
}

// NewFakeQueue returns a fresh fake queue.
func NewFakeQueue() *FakeQueue {
	return &FakeQueue{}
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
	f.mu.Lock()
	jobs := make([]DispatchedJob, len(f.Jobs))
	copy(jobs, f.Jobs)
	f.mu.Unlock()

	var errs []error
	for _, d := range jobs {
		if err := d.Job.Handle(ctx); err != nil {
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
