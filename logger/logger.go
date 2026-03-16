package logger

import (
	"fmt"
	"os"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger wraps zap.SugaredLogger for structured logging. Nimbus uses this
// logger in its own middleware and internals, and applications are free to
// replace it at startup (or in tests) via Set with their own zap.Logger.
var Log *zap.SugaredLogger

// channelLoggers holds named channel loggers.
var channelLoggers = map[string]*zap.SugaredLogger{}

func init() {
	cfg := zap.NewProductionConfig()
	cfg.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
	cfg.Encoding = "console"
	cfg.EncoderConfig.TimeKey = "time"
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	l, _ := cfg.Build()
	Log = l.Sugar()
}

// ── Configuration ───────────────────────────────────────────────

// Config configures the logger.
type Config struct {
	// Level: debug, info, warn, error, fatal (default: info)
	Level string

	// Format: "console" or "json" (default: console)
	Format string

	// Channels: named output channels.
	// Each channel can write to stdout, stderr, or a file path.
	Channels map[string]ChannelConfig
}

// ChannelConfig configures a single logging channel.
type ChannelConfig struct {
	// Driver: "stdout", "stderr", "file" (default: stdout)
	Driver string

	// Path is required for "file" driver.
	Path string

	// Level overrides the global level for this channel.
	Level string

	// Format overrides the global format for this channel.
	Format string
}

// Configure sets up the global logger from Config.
func Configure(cfg Config) error {
	level := parseLevel(cfg.Level)
	encoding := cfg.Format
	if encoding == "" {
		encoding = "console"
	}

	zapCfg := zap.Config{
		Level:            zap.NewAtomicLevelAt(level),
		Encoding:         encoding,
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
		EncoderConfig:    encoderConfig(encoding),
	}

	l, err := zapCfg.Build()
	if err != nil {
		return fmt.Errorf("logger: %w", err)
	}
	Set(l)

	// Set up channels
	for name, ch := range cfg.Channels {
		cl, err := buildChannel(name, ch, cfg)
		if err != nil {
			return fmt.Errorf("logger: channel %q: %w", name, err)
		}
		channelLoggers[name] = cl
	}

	return nil
}

// buildChannel creates a named channel logger.
func buildChannel(name string, ch ChannelConfig, global Config) (*zap.SugaredLogger, error) {
	lvl := parseLevel(ch.Level)
	if ch.Level == "" {
		lvl = parseLevel(global.Level)
	}

	encoding := ch.Format
	if encoding == "" {
		encoding = global.Format
	}
	if encoding == "" {
		encoding = "console"
	}

	var outputPaths []string
	switch strings.ToLower(ch.Driver) {
	case "stderr":
		outputPaths = []string{"stderr"}
	case "file":
		if ch.Path == "" {
			return nil, fmt.Errorf("file channel requires path")
		}
		// Ensure parent directory exists
		dir := ch.Path[:strings.LastIndex(ch.Path, "/")]
		if dir != "" {
			_ = os.MkdirAll(dir, 0755)
		}
		outputPaths = []string{ch.Path}
	default:
		outputPaths = []string{"stdout"}
	}

	zapCfg := zap.Config{
		Level:            zap.NewAtomicLevelAt(lvl),
		Encoding:         encoding,
		OutputPaths:      outputPaths,
		ErrorOutputPaths: []string{"stderr"},
		EncoderConfig:    encoderConfig(encoding),
	}

	l, err := zapCfg.Build()
	if err != nil {
		return nil, err
	}
	return l.Sugar().Named(name), nil
}

func encoderConfig(encoding string) zapcore.EncoderConfig {
	if encoding == "json" {
		return zapcore.EncoderConfig{
			TimeKey:        "time",
			LevelKey:       "level",
			NameKey:        "channel",
			CallerKey:      "caller",
			MessageKey:     "msg",
			StacktraceKey:  "stacktrace",
			LineEnding:     zapcore.DefaultLineEnding,
			EncodeLevel:    zapcore.LowercaseLevelEncoder,
			EncodeTime:     zapcore.ISO8601TimeEncoder,
			EncodeDuration: zapcore.MillisDurationEncoder,
			EncodeCaller:   zapcore.ShortCallerEncoder,
		}
	}
	return zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "channel",
		CallerKey:      "",
		MessageKey:     "msg",
		StacktraceKey:  "",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalColorLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}
}

func parseLevel(s string) zapcore.Level {
	switch strings.ToLower(s) {
	case "debug":
		return zapcore.DebugLevel
	case "warn", "warning":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	case "fatal":
		return zapcore.FatalLevel
	default:
		return zapcore.InfoLevel
	}
}

// ── Global Logger ───────────────────────────────────────────────

// Set replaces the global logger (e.g. for testing or custom config).
func Set(l *zap.Logger) {
	if Log != nil {
		_ = Log.Sync()
	}
	Log = l.Sugar()
}

// SetLevel dynamically changes the log level.
func SetLevel(level string) {
	lvl := parseLevel(level)
	cfg := zap.NewProductionConfig()
	cfg.Level = zap.NewAtomicLevelAt(lvl)
	cfg.Encoding = "console"
	cfg.EncoderConfig.TimeKey = "time"
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	l, _ := cfg.Build()
	Set(l)
}

// ── Channel Logging ─────────────────────────────────────────────

// Channel returns a named channel logger. Falls back to the global logger.
func Channel(name string) *zap.SugaredLogger {
	if cl, ok := channelLoggers[name]; ok {
		return cl
	}
	return Log.Named(name)
}

// ── Convenience Functions ───────────────────────────────────────

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

// Debugf logs a formatted debug message.
func Debugf(template string, args ...any) { Log.Debugf(template, args...) }

// Infof logs a formatted info message.
func Infof(template string, args ...any) { Log.Infof(template, args...) }

// Warnf logs a formatted warning message.
func Warnf(template string, args ...any) { Log.Warnf(template, args...) }

// Errorf logs a formatted error message.
func Errorf(template string, args ...any) { Log.Errorf(template, args...) }

// Fatalf logs a formatted fatal message and exits.
func Fatalf(template string, args ...any) { Log.Fatalf(template, args...) }

// With returns a logger with additional fields.
func With(keysAndValues ...any) *zap.SugaredLogger { return Log.With(keysAndValues...) }

// WithFields returns a logger with structured fields.
func WithFields(fields map[string]any) *zap.SugaredLogger {
	kv := make([]any, 0, len(fields)*2)
	for k, v := range fields {
		kv = append(kv, k, v)
	}
	return Log.With(kv...)
}

// Sync flushes any buffered log entries.
func Sync() {
	if Log != nil {
		_ = Log.Sync()
	}
	for _, cl := range channelLoggers {
		_ = cl.Sync()
	}
}
