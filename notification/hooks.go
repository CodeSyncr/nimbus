package notification

import "sync"

var (
	afterSendMu  sync.RWMutex
	afterSendFns []func(Notification, error)
)

// AfterSend registers a callback invoked when Send completes. It receives the
// notification and the error from the mail step (if any); if mail succeeded,
// the notification was also broadcast (when applicable) before the hook runs.
func AfterSend(fn func(Notification, error)) {
	if fn == nil {
		return
	}
	afterSendMu.Lock()
	defer afterSendMu.Unlock()
	afterSendFns = append(afterSendFns, fn)
}

func runAfterSendHooks(n Notification, err error) {
	afterSendMu.RLock()
	fns := make([]func(Notification, error), len(afterSendFns))
	copy(fns, afterSendFns)
	afterSendMu.RUnlock()
	for _, fn := range fns {
		fn(n, err)
	}
}

var (
	fakeMu     sync.RWMutex
	fakeRecord func(Notification)
)

// Fake makes Send, SendMail and Broadcast hand notifications to record instead
// of delivering them, until restore is called. Used by
// nimbustesting.FakeNotifications.
func Fake(record func(Notification)) (restore func()) {
	fakeMu.Lock()
	prev := fakeRecord
	fakeRecord = record
	fakeMu.Unlock()
	return func() {
		fakeMu.Lock()
		fakeRecord = prev
		fakeMu.Unlock()
	}
}

// recordFake records n and reports true when notifications are faked.
func recordFake(n Notification) bool {
	fakeMu.RLock()
	record := fakeRecord
	fakeMu.RUnlock()
	if record == nil {
		return false
	}
	record(n)
	return true
}
