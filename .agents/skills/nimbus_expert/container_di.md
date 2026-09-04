# Service Container & Dependency Injection

`container.Container` is the service locator behind the application. Bindings
are keyed by string name.

```go
app.Container.Singleton("payments", func() *stripe.Client {
    return stripe.New(config.Get().Stripe.Key)
})

client := app.Container.MustMake("payments").(*stripe.Client)
```

## API

| Method | Behaviour |
| --- | --- |
| `New() *Container` | Fresh container |
| `Bind(name, constructor)` | **Transient** — the constructor runs on every `Make` |
| `Singleton(name, constructor)` | Constructor runs once; the value is cached and returned thereafter |
| `Instance(name, value)` | Store an already-built value |
| `Make(name) (any, error)` | Resolve; error if unbound or the constructor fails |
| `MustMake(name) any` | Resolve; panics if unbound |
| `Has(name) bool` | Binding exists |

`Constructor` is `any`, so a constructor may be:

- `func() T` — plain factory
- `func() (T, error)` — factory that can fail; the error surfaces from `Make`
- `func(dep *Dep) T` — parameters are auto-wired by type from existing bindings

Singleton resolution is concurrency-safe: simultaneous `Make` calls produce one
instance.

`Bind` after `Singleton` on the same name overrides it, which is the hook for
swapping a real service for a fake in tests.

## Auto-wiring

When a constructor takes parameters, the container resolves each by type from
what is already bound. Keep dependency graphs shallow; a missing type is a
runtime error, not a compile error.

## Router integration

`app.Router.Container` is set to the application container at construction, so
route handlers and resource controllers can resolve services without a package
global.

## Testing

```go
app.Container.Instance("payments", &fakePayments{})
```

`Instance` beats `Singleton` in tests: no constructor runs, and the exact value
you pass is what handlers receive.

## Guidance

1. Bind interfaces, resolve interfaces — it is what makes the swap above work.
2. Prefer `Singleton` for anything holding a connection pool.
3. Resolve at the edge (provider boot, handler entry), not deep inside domain
   code; a package that calls `MustMake` is a package you cannot unit-test
   without a container.
4. Service providers are the idiomatic place to register bindings — see
   [plugin](plugin.md) and [framework_overview](framework_overview.md).
