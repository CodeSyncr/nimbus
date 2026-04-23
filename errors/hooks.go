package errors

import "sync"

var (
	hookMu            sync.RWMutex
	exceptionRecorder func(class, message, file string, line int, trace string)
)

// RegisterExceptionRecorder registers a callback invoked when the global error
// handler records an exception (AppError, unhandled handler error, or panic).
// Used by the Telescope plugin; safe to call with nil to clear.
func RegisterExceptionRecorder(fn func(class, message, file string, line int, trace string)) {
	hookMu.Lock()
	defer hookMu.Unlock()
	exceptionRecorder = fn
}

func notifyExceptionRecorded(class, message, file string, line int, trace string) {
	hookMu.RLock()
	fn := exceptionRecorder
	hookMu.RUnlock()
	if fn != nil {
		fn(class, message, file, line, trace)
	}
}

