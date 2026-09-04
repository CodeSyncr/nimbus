# Feature Flags

Runtime toggles with per-user targeting and variants.

## Defining flags

```go
flags.Define("new-checkout", flags.Config{ ... })

if flags.Active("new-checkout") {
    // new path
}
```

`flags.Default() *Manager` is the global manager; `flags.New(store)` builds
your own.

## Per-user evaluation

```go
if flags.For(user.ID, "beta", "staff").Active("new-checkout") {
    // enabled for this user
}

variant := flags.Variant("checkout-copy", user.ID)  // "control" | "b" | …
```

`For(userID, groups...)` returns a `UserEvaluator` carrying the user's identity
and group membership; `UserContext` is the value the store sees when deciding.

Evaluation is deterministic for a given user id, so a percentage rollout keeps
the same users inside the flag between requests rather than flickering.

## Stores

| Store | Constructor | Use |
| --- | --- | --- |
| Memory | `MemoryStore` | Tests and local development |
| File | `NewFileStore(path)` | Flags in version control, reloaded from disk |

`Store` is an interface, so a database- or Redis-backed store is a small
implementation away.

## Route protection

```go
api.Get("/checkout/v2", handler, flags.FlagGate(mgr, "new-checkout"))
app.Router.Use(flags.RequireFlag(mgr))
```

| Middleware | Behaviour |
| --- | --- |
| `FlagGate(m, name)` | Blocks the route unless that flag is active |
| `RequireFlag(m)` | Generic gate reading the flag from the route context |

## As a plugin

`flags.NewPlugin(store) *FlagPlugin` registers the manager into the app so
handlers and templates can reach it without a package global. See
[plugin](plugin.md).

## Guidance

1. Flags are temporary. Every flag needs a removal date, or you accumulate dead
   branches that no test covers.
2. Never flag-gate a database migration — schema changes must be safe for both
   sides of the flag.
3. Default to **off**; a flag that fails open removes the safety it was added
   for.
