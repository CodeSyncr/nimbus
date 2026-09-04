# Health Checks

`health.Checker` aggregates named checks into one result and exposes it as an
HTTP handler. `app.Health` is the application's checker, created during
`nimbus.New()`.

## Registering checks

```go
app.Health.DB(db)                  // database connectivity
app.Health.Redis(rdb)              // redis connectivity

app.Health.Add("stripe", func(ctx context.Context) error {
    return stripe.Ping(ctx)
})
```

A `Check` is `func(ctx context.Context) error` — returning `nil` is healthy,
returning an error is not, and the error message ends up in the result.

`DB(db)` and `Redis(rdb)` are shortcuts for the two checks nearly every app
needs.

## Exposing it

```go
app.Router.Mount("/health", app.Health.Handler())
```

`Handler()` returns a standard handler that runs every check and serialises the
`Result`. `Run(ctx) Result` does the same thing in-process, which is what you
want from a CLI command or a test.

Plugins register their own checks during boot, so mounting one handler covers
the whole application, plugins included.

## Designing checks

1. **Give every check a context deadline.** A check that blocks turns your
   health endpoint into a hang, and a load balancer reads a hang as unhealthy —
   or worse, holds the connection.
2. **Check dependencies you cannot serve without**, not everything you touch. A
   failing analytics API should not take an instance out of rotation.
3. **Distinguish liveness from readiness.** Liveness ("is this process wedged")
   should have almost no checks; readiness ("can this instance serve traffic")
   is where the database and cache belong. Mount two endpoints if your platform
   distinguishes them.
4. **Never expose secrets or version detail** on a public health endpoint — the
   result is a reconnaissance target.

Related: [metrics](metrics.md) for continuous numbers rather than a pass/fail
snapshot.
