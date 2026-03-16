package logger

import (
	"go.uber.org/zap"

	"github.com/CodeSyncr/nimbus/http"
)

// contextKey is the key used to store the request-scoped logger in the context store.
const contextKey = "_nimbus_logger"

// ForRequest returns a *zap.SugaredLogger scoped to the current HTTP request.
// It carries the request_id (set by the RequestID middleware) and any other
// fields previously attached via WithContext.
//
// If no scoped logger exists yet, one is created from the global logger with
// the request_id field.
//
// Usage:
//
//	log := logger.ForRequest(c)
//	log.Info("processing payment", "amount", 42.50)
func ForRequest(c *http.Context) *zap.SugaredLogger {
	if v, ok := c.Get(contextKey); ok {
		if l, ok := v.(*zap.SugaredLogger); ok {
			return l
		}
	}

	// Build a scoped logger with request_id.
	l := Log
	if rid, ok := c.Get("request_id"); ok {
		l = l.With("request_id", rid)
	}

	c.Set(contextKey, l)
	return l
}

// WithContext attaches a request-scoped logger to the context. This is
// typically called by middleware to enrich the logger with additional fields.
//
//	logger.WithContext(c, logger.Log.With("user_id", userID))
func WithContext(c *http.Context, l *zap.SugaredLogger) {
	c.Set(contextKey, l)
}
