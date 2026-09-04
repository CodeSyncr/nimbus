# Errors & Exception Handling

Handlers return `error`. What the client sees is decided by the error's type
and by content negotiation.

## Error types

### `AppError`

The general-purpose HTTP error.

```go
return errors.New(http.StatusPaymentRequired, "subscription expired")
return errors.Wrap(http.StatusBadGateway, err)   // keeps the cause
```

| Method | Behaviour |
| --- | --- |
| `Error() string` | Message |
| `Unwrap() error` | The wrapped cause, so `errors.Is` / `errors.As` work |
| `HTTPStatus() int` | Status to send |

### `HTTPError`

A value type carrying a status; `WriteHTTPError(c, he)` renders it. Anything
implementing `HTTPStatus() int` is treated as an HTTP error by the handler
middleware, so your own domain errors can opt in by adding that one method.

## Content negotiation

`WantsHTML(c) bool` decides the shape of the response from the `Accept` header:
a browser gets a styled HTML page, an API client gets structured JSON. Both
carry a tracking `error_id`. `RenderDefaultHTML(c, HTMLPageData{...})` renders
the built-in page.

## Middleware

| Function | Use |
| --- | --- |
| `Handler() router.Middleware` | Production error handler |
| `SmartErrorHandler(cfg ...DevPageConfig) router.Middleware` | Development page with stack frames and source context |
| `NotFoundHandler() func(*http.Context) error` | Default 404; the router installs it as the fallback |

`SmartErrorHandler` builds a `DevError` containing `StackFrame`s, each with
`SourceLine`s around the failure, plus `RequestInfo`. `RelPath(path)` shortens
absolute paths for display. **Only enable it outside production** — it exposes
source code and request detail.

Override the 404 with `app.Router.Fallback(myHandler)`.

## Reporting to an external service

```go
type Reporter interface {
    Report(err error, context map[string]any) error
}

errors.RegisterReporter(&SentryReporter{})
errors.ReportError(err, map[string]any{"user_id": id})
```

`LogReporter` writes to the logger and is the default. `ClearReporters()` resets
the list — useful in tests. Reporters are additive: every registered reporter
sees every reported error.

`RegisterExceptionRecorder(fn func(class, message, file string, line int, trace string))`
is the lower-level hook Telescope uses to capture exceptions for its dashboard.

## Patterns

```go
func Show(c *http.Context) error {
    user, err := repo.Find(c.Param("id"))
    if err == sql.ErrNoRows {
        return c.NotFound("user not found")     // shortcut, returns error
    }
    if err != nil {
        return errors.Wrap(500, err)            // preserves the cause
    }
    return c.JSON(200, user)
}
```

1. Return errors, do not panic — `Recover()` exists for genuine bugs, not
   control flow.
2. Wrap rather than replace, so `errors.Is` still works upstream.
3. Never put a database or driver message in a client-facing string; log the
   cause and return a generic message with the `error_id`.
