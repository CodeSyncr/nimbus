package testing

import (
	"fmt"
	"sync"
	"testing"

	"github.com/CodeSyncr/nimbus/events"
	"github.com/CodeSyncr/nimbus/notification"
)

// ── Events ──────────────────────────────────────────────────────

// DispatchedEvent records one Dispatch or DispatchAsync call.
type DispatchedEvent struct {
	Name    string
	Payload any
}

// FakeEventBus records the events a faked dispatcher received.
type FakeEventBus struct {
	mu     sync.Mutex
	events []DispatchedEvent
}

// FakeEvents fakes events.Default for the rest of the test: events.Dispatch
// records events instead of running listeners, except for the event names in
// except, whose listeners still run (they are recorded too).
//
//	bus := nimbustesting.FakeEvents(t)
//	client.PostJSON("/signup", body)
//	bus.AssertDispatched(t, "user.created")
func FakeEvents(t testing.TB, except ...string) *FakeEventBus {
	t.Helper()
	return FakeEventsOn(t, events.Default, except...)
}

// FakeEventsOn fakes one dispatcher, such as app.Events, the same way
// FakeEvents fakes events.Default.
func FakeEventsOn(t testing.TB, d *events.Dispatcher, except ...string) *FakeEventBus {
	t.Helper()
	claim(t, fmt.Sprintf("events dispatcher %p", d))
	f := &FakeEventBus{}
	restore := d.Fake(func(name string, payload any) {
		f.mu.Lock()
		f.events = append(f.events, DispatchedEvent{Name: name, Payload: payload})
		f.mu.Unlock()
	}, except...)
	t.Cleanup(restore)
	return f
}

// Dispatched returns every recorded event, in order.
func (f *FakeEventBus) Dispatched() []DispatchedEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]DispatchedEvent, len(f.events))
	copy(out, f.events)
	return out
}

// Payloads returns the payloads recorded for event, in order.
func (f *FakeEventBus) Payloads(event string) []any {
	var out []any
	for _, e := range f.Dispatched() {
		if e.Name == event {
			out = append(out, e.Payload)
		}
	}
	return out
}

// AssertDispatched fails the test unless event was dispatched (and, when match
// is given, one of its payloads matches it).
func (f *FakeEventBus) AssertDispatched(t testing.TB, event string, match ...func(payload any) bool) {
	t.Helper()
	payloads := f.Payloads(event)
	if len(payloads) == 0 {
		t.Errorf("expected event %q to be dispatched, got none", event)
		return
	}
	if len(match) > 0 && !anyMatch(payloads, match[0]) {
		t.Errorf("event %q dispatched %d time(s), no payload matched", event, len(payloads))
	}
}

// AssertNotDispatched fails the test if event was dispatched.
func (f *FakeEventBus) AssertNotDispatched(t testing.TB, event string) {
	t.Helper()
	if n := len(f.Payloads(event)); n > 0 {
		t.Errorf("expected event %q not to be dispatched, got %d", event, n)
	}
}

// AssertDispatchedTimes fails the test unless event was dispatched exactly n times.
func (f *FakeEventBus) AssertDispatchedTimes(t testing.TB, event string, n int) {
	t.Helper()
	if got := len(f.Payloads(event)); got != n {
		t.Errorf("expected event %q dispatched %d time(s), got %d", event, n, got)
	}
}

// AssertNothingDispatched fails the test if any event was dispatched.
func (f *FakeEventBus) AssertNothingDispatched(t testing.TB) {
	t.Helper()
	if got := f.Dispatched(); len(got) > 0 {
		t.Errorf("expected no dispatched events, got %d (first %q)", len(got), got[0].Name)
	}
}

// ── Notifications ───────────────────────────────────────────────

// FakeNotifier records the notifications sent while faked.
type FakeNotifier struct {
	mu   sync.Mutex
	sent []notification.Notification
}

// FakeNotifications makes notification.Send (and SendMail, Broadcast) record
// notifications instead of mailing or broadcasting them for the rest of the
// test. AfterSend hooks still run for Send.
//
//	notes := nimbustesting.FakeNotifications(t)
//	client.PostJSON("/orders", body)
//	nimbustesting.AssertNotificationSent(t, notes, func(n *OrderShipped) bool { return n.OrderID == 7 })
func FakeNotifications(t testing.TB) *FakeNotifier {
	t.Helper()
	claim(t, "notifications")
	f := &FakeNotifier{}
	t.Cleanup(notification.Fake(func(n notification.Notification) {
		f.mu.Lock()
		f.sent = append(f.sent, n)
		f.mu.Unlock()
	}))
	return f
}

// Sent returns every recorded notification, in order.
func (f *FakeNotifier) Sent() []notification.Notification {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]notification.Notification, len(f.sent))
	copy(out, f.sent)
	return out
}

// AssertNothingSent fails the test if any notification was sent.
func (f *FakeNotifier) AssertNothingSent(t testing.TB) {
	t.Helper()
	if n := len(f.Sent()); n > 0 {
		t.Errorf("expected no sent notifications, got %d", n)
	}
}

// AssertSentCount fails the test unless exactly n notifications were sent.
func (f *FakeNotifier) AssertSentCount(t testing.TB, n int) {
	t.Helper()
	if got := len(f.Sent()); got != n {
		t.Errorf("expected %d sent notifications, got %d", n, got)
	}
}

// NotificationsOf returns the sent notifications of type T.
func NotificationsOf[T notification.Notification](f *FakeNotifier) []T {
	var out []T
	for _, n := range f.Sent() {
		if v, ok := n.(T); ok {
			out = append(out, v)
		}
	}
	return out
}

// AssertNotificationSent fails the test unless a notification of type T was
// sent (and, when match is given, one of them matches it).
func AssertNotificationSent[T notification.Notification](t testing.TB, f *FakeNotifier, match ...func(T) bool) {
	t.Helper()
	sent := NotificationsOf[T](f)
	if len(sent) == 0 {
		t.Errorf("expected a sent %T notification, got none", *new(T))
		return
	}
	if len(match) > 0 && !anyMatch(sent, match[0]) {
		t.Errorf("%d %T notification(s) sent, none matched", len(sent), *new(T))
	}
}

// AssertNotificationNotSent fails the test if a notification of type T was sent.
func AssertNotificationNotSent[T notification.Notification](t testing.TB, f *FakeNotifier) {
	t.Helper()
	if n := len(NotificationsOf[T](f)); n > 0 {
		t.Errorf("expected no sent %T notification, got %d", *new(T), n)
	}
}
