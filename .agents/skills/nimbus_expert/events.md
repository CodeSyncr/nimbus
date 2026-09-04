# Events

A small synchronous/asynchronous dispatcher. Events are plain strings; a
listener is `func(payload any) error`.

```go
app.Events.Listen("user:registered", func(payload any) error {
    u := payload.(*models.User)
    return mail.Send(u.Email, welcome)
})

app.Events.Dispatch("user:registered", user)
```

## Dispatcher API

| Method | Behaviour |
| --- | --- |
| `New() *Dispatcher` | Fresh dispatcher |
| `Listen(event, fn)` | Register a listener |
| `Dispatch(event, payload) error` | Run listeners **synchronously**; returns the first error |
| `DispatchAsync(event, payload)` | Run listeners in goroutines; errors are dropped |
| `Has(event) bool` | Any listener registered |
| `ListenerCount(event) int` | How many |
| `Clear(events ...string)` | Remove listeners; no arguments clears everything |

Package-level `Listen`, `Dispatch`, and `DispatchAsync` proxy to a `Default`
dispatcher. `app.Events` is the one wired into the application lifecycle — use
it rather than the global in application code.

`AfterDispatch(fn func(event string, payload any))` installs a global observer
that sees every dispatch. Telescope uses this to record the event timeline;
avoid stacking your own on top in production, since it runs for every event.

## Framework lifecycle events

The app dispatches these itself (`events` constants):

### Application lifecycle

| Constant | Name | Payload | When |
| --- | --- | --- | --- |
| `AppBooted` | `app:booted` | nil | Boot complete, capabilities applied |
| `AppWarmed` | `app:warmed` | `*App` | Warm-up complete, app assembled for run or inspection |
| `AppStarted` | `app:started` | `string` (port) | Server listening |
| `AppReady` | `app:ready` | `string` (port) | HTTP server actively accepting traffic |
| `AppShutdown` | `app:shutdown` | `os.Signal` | Graceful shutdown started |

### Boot phases

| Constant | Name | Payload | When |
| --- | --- | --- | --- |
| `ProviderBoot` | `provider:boot` | nil | All service providers booted |
| `PluginBoot` | `plugin:boot` | nil | All plugins booted |
| `RouteRegistered` | `route:registered` | nil | Plugin routes mounted |
| `MiddlewareRegistered` | `middleware:registered` | nil | Plugin middleware merged |

### Database

`DatabaseQuery` (`db:query`), `DatabaseInsert` (`db:insert`),
`DatabaseUpdate` (`db:update`), `DatabaseDelete` (`db:delete`) — these are what
Telescope's query panel listens to.

Listen for these to run work at a precise point in the lifecycle without
writing a provider.

## Error handling

`Dispatch` stops at the first listener that returns an error and returns it, so
a failing listener can abort the caller. `DispatchAsync` cannot — if a listener
must not fail silently, dispatch synchronously or report inside the listener.

## Scaffolding

```bash
nimbus make:event UserRegistered     # app/events/user_registered.go
nimbus make:listener SendWelcome     # app/listeners/send_welcome.go
```

Related: [queue_jobs](queue_jobs.md) for work that should survive a restart —
events are in-process only.
