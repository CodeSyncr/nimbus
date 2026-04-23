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
