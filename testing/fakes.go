package testing

import (
	"sync"

	"github.com/CodeSyncr/nimbus/mail"
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
