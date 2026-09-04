# Middleware

A middleware is `func(router.HandlerFunc) router.HandlerFunc`. Register it
globally on the router, on a group, or per route.

```go
app.Router.Use(middleware.Logger(), middleware.Recover())

api := app.Router.Group("/api", middleware.RateLimit(60, time.Minute, nil))
api.Get("/me", handlers.Me)
```

Global middleware is applied in registration order and wraps every route,
including the fallback. Adding global middleware after routes are registered
re-mounts the fallback (`router.remountFallback`), but does **not** rewrap
already-registered routes — register global middleware first.

## Built-in middleware

| Constructor | Purpose |
| --- | --- |
| `Logger()` | Request logging; prints a console line when a human is watching |
| `Recover()` | Converts a panic into a 500 instead of killing the process |
| `RequestID()` | Assigns an id per request; read it with `GetRequestID(c)` |
| `CORS(origins ...string)` | Cross-origin headers; supports wildcard |
| `CSRF(store CSRFStore)` | Token check on unsafe verbs |
| `SecureHeaders(cfg)` | HSTS, frame options, content-type options, referrer policy |
| `RateLimit(limit, window, keyFn)` | In-process token bucket |
| `RateLimitRedis(rdb, limit, window, keyFn, failOpen...)` | Distributed limiting |
| `Timeout(d)` | Cancels the request context after `d` |
| `Gzip()` | Response compression |
| `BodyLimit(maxBytes)` | Rejects oversized request bodies |
| `Metrics()` | Feeds the `metrics` package |
| `TrustedProxies(cidrs ...string)` | Makes `c.IP()` honour `X-Forwarded-For` only from listed proxies |

### Secure headers

```go
cfg := middleware.DefaultSecureHeadersConfig()
cfg.HSTSMaxAge = 63072000
app.Router.Use(middleware.SecureHeaders(cfg))
```

### CSRF

`CSRFStore` is an interface with `Create() string` and
`Valid(ctx, token) bool`. `NewMemoryCSRFStore()` is the built-in
implementation; `GenerateCSRFToken()` produces a token directly.

Safe verbs (GET/HEAD/OPTIONS) pass through untouched.

### Rate limiting

`keyFn func(*stdhttp.Request) string` decides the bucket. Pass `nil` to use
`DefaultKeyFn`, which keys on client IP.

`RateLimitRedis`'s trailing `failOpen` argument decides what happens when Redis
is unreachable: fail open (allow) or fail closed (reject). Choose deliberately —
failing open under a Redis outage removes your limiter entirely.

## The console request line

`ConsoleRequestLine(method, path, status, duration) string` renders the
`[HTTP] GET /cloud 200 OK — 0.8ms` line in the same visual language as the
startup view. It is printed only when a human is watching: not in production,
not when `NO_COLOR` or `LOG_FORMAT=json` is set, and only when stdout is a
terminal — or when `NIMBUS_SERVE=1`, because then the `nimbus serve` CLI is
piping the output straight to one.

## Named middleware

Plugins ship middleware under a name by implementing `HasMiddleware`:

```go
func (p *Plugin) Middleware() map[string]router.Middleware {
    return map[string]router.Middleware{"auth": p.requireAuth()}
}
```

During boot the app merges every plugin's map, and `app.NamedMiddleware()`
returns the merged result — use it from `start/kernel.go` or `start/routes.go`:

```go
mw := app.NamedMiddleware()
app.Router.Group("/admin", mw["auth"])
```

This is what lets a plugin supply middleware your routes reference without
importing the plugin package. There is no `App` method for registering a named
middleware directly; go through a plugin, or pass the middleware value itself.

## Writing your own

```go
func Timing(header string) router.Middleware {
    return func(next router.HandlerFunc) router.HandlerFunc {
        return func(c *http.Context) error {
            start := time.Now()
            err := next(c)
            c.SetHeader(header, time.Since(start).String())
            return err
        }
    }
}
```

Rules that matter:

1. Always call `next(c)` unless you are deliberately short-circuiting, and
   return its error.
2. Set response headers **before** anything writes a body.
3. If you wrap `ResponseWriter`, implement `Unwrap()` — the framework's own
   wrappers do, so that `Flush`, `Hijack`, and websocket upgrades keep working.

`nimbus make:middleware Auth` scaffolds `app/middleware/auth.go`.
