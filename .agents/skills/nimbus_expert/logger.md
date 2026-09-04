# Logger

Structured logging built on [zap](https://github.com/uber-go/zap). The package
exposes a configured global plus per-channel and per-request loggers.

```go
logger.Info("order placed", "order_id", id, "total", total)
logger.Errorf("payment failed: %v", err)
```

## Levels

`Debug`, `Info`, `Warn`, `Error`, `Fatal` take a message plus alternating
key/value pairs. The `f` variants (`Debugf`, `Infof`, `Warnf`, `Errorf`,
`Fatalf`) take a format string instead.

`Fatal`/`Fatalf` call `os.Exit` — never use them in a request path.

## Structured context

```go
log := logger.With("tenant", tenantID)
log.Infow("cache miss", "key", key)

logger.WithFields(map[string]any{"job": "resize", "attempt": 2}).Warn("retrying")
```

## Configuration

```go
logger.Configure(logger.Config{ ... })
logger.SetLevel("debug")     // change level at runtime
logger.Set(customZapLogger)  // swap the global entirely
```

`Sync()` flushes buffered entries — call it from a shutdown hook.

## Channels

`Channel(name) *zap.SugaredLogger` returns a named logger configured by a
`ChannelConfig`, so audit, billing, and application logs can go to different
destinations with different levels.

## Per-request loggers

```go
// in middleware
logger.WithContext(c, logger.With("request_id", middleware.GetRequestID(c)))

// in a handler
logger.ForRequest(c).Infow("handled", "user", userID)
```

`ForRequest` returns the request's logger, falling back to the global when no
middleware attached one — so it is always safe to call.

## Rotation

```go
w, err := logger.NewRotatingWriter(logger.RotationConfig{ ... })
```

`RotatingWriter` implements `Write`, `Sync`, and `Close`. Close it from a
shutdown hook or the final entries are lost.

## Fan-out

`TeeCore(extra zapcore.Core) error` adds a second sink to the existing logger —
this is how log-shipping plugins attach without replacing your configuration.

## Guidance

1. Prefer key/value logging over formatted strings; it is what makes logs
   queryable.
2. Log the `error_id` from the error handler so a user report maps to a log line.
3. Set `LOG_FORMAT=json` in production — it also switches the request logger off
   the human-readable console line.
