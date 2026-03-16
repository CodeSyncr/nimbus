package errors

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"sync"
	"time"
)

// ---------- Error IDs -------------------------------------------------------

// AppError is an application error with a unique ID for tracking.
// When returned from a handler, the error handler middleware logs the full
// error + ID and returns only the ID to the client so they can reference it
// in support requests.
//
//	return errors.New(500, "database timeout")
//	// → logs "error_id=abc123 database timeout"
//	// → responds {"error": "Internal server error", "error_id": "abc123"}
type AppError struct {
	ID        string    `json:"error_id"`
	Status    int       `json:"status"`
	Message   string    `json:"message"`
	Internal  error     `json:"-"` // original error, not exposed to client
	Timestamp time.Time `json:"timestamp"`
}

// New creates an AppError with a unique ID.
func New(status int, message string) *AppError {
	return &AppError{
		ID:        generateErrorID(),
		Status:    status,
		Message:   message,
		Timestamp: time.Now(),
	}
}

// Wrap creates an AppError wrapping an existing error. The original error
// is kept for logging but not exposed to the client.
func Wrap(status int, err error) *AppError {
	return &AppError{
		ID:        generateErrorID(),
		Status:    status,
		Message:   err.Error(),
		Internal:  err,
		Timestamp: time.Now(),
	}
}

// Error implements the error interface.
func (e *AppError) Error() string {
	return fmt.Sprintf("[%s] %s", e.ID, e.Message)
}

// Unwrap returns the wrapped error.
func (e *AppError) Unwrap() error {
	return e.Internal
}

func generateErrorID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ---------- Error Reporter --------------------------------------------------

// Reporter is an interface for external error reporting services
// (e.g. Sentry, Bugsnag, Rollbar).
type Reporter interface {
	Report(err error, context map[string]any) error
}

// reporters holds registered error reporters.
var (
	reportersMu sync.RWMutex
	reporters   []Reporter
)

// RegisterReporter adds an error reporter. When errors are handled by the
// error handler middleware, they are also sent to all registered reporters.
//
//	errors.RegisterReporter(&SentryReporter{DSN: "https://..."})
func RegisterReporter(r Reporter) {
	reportersMu.Lock()
	reporters = append(reporters, r)
	reportersMu.Unlock()
}

// ReportError sends an error to all registered reporters.
func ReportError(err error, context map[string]any) {
	reportersMu.RLock()
	reps := make([]Reporter, len(reporters))
	copy(reps, reporters)
	reportersMu.RUnlock()

	for _, r := range reps {
		if reportErr := r.Report(err, context); reportErr != nil {
			log.Printf("[errors] reporter failed: %v", reportErr)
		}
	}
}

// ClearReporters removes all registered reporters (useful in tests).
func ClearReporters() {
	reportersMu.Lock()
	reporters = nil
	reportersMu.Unlock()
}

// ---------- Built-in Reporters ----------------------------------------------

// LogReporter is a simple reporter that logs errors to the standard logger.
type LogReporter struct{}

// Report logs the error.
func (r *LogReporter) Report(err error, context map[string]any) error {
	log.Printf("[error-report] %v context=%v", err, context)
	return nil
}
