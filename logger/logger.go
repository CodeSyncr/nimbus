package logger

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger wraps zap.SugaredLogger for structured logging. Nimbus uses this
// logger in its own middleware and internals, and applications are free to
// replace it at startup (or in tests) via Set with their own zap.Logger.
var Log *zap.SugaredLogger

func init() {
	cfg := zap.NewProductionConfig()
	cfg.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
	cfg.Encoding = "console"
	cfg.EncoderConfig.TimeKey = "time"
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	l, _ := cfg.Build()
	Log = l.Sugar()
}

// Set replaces the global logger (e.g. for testing or custom config).
func Set(l *zap.Logger) {
	if Log != nil {
		_ = Log.Sync()
	}
	Log = l.Sugar()
}

// Debug logs at debug level.
func Debug(msg string, keysAndValues ...any) { Log.Debugw(msg, keysAndValues...) }

// Info logs at info level.
func Info(msg string, keysAndValues ...any) { Log.Infow(msg, keysAndValues...) }

// Warn logs at warn level.
func Warn(msg string, keysAndValues ...any) { Log.Warnw(msg, keysAndValues...) }

// Error logs at error level.
func Error(msg string, keysAndValues ...any) { Log.Errorw(msg, keysAndValues...) }

// Fatal logs and exits.
func Fatal(msg string, keysAndValues ...any) { Log.Fatalw(msg, keysAndValues...) }

// With returns a logger with additional fields.
func With(keysAndValues ...any) *zap.SugaredLogger { return Log.With(keysAndValues...) }
