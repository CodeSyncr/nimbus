# Service Providers & Application Lifecycle

A provider is the unit that wires a capability into the application. Plugins are
providers with extra optional interfaces — see [plugin](plugin.md).

## The two phases

```go
type Provider interface {
    Register(app *App) error   // bind into the container — do not use other services yet
    Boot(app *App) error       // everything is registered; safe to resolve and use
}
```

The split exists for one reason: during `Register` you cannot assume any other
provider has run. Bind in `Register`, use in `Boot`. A provider that resolves a
dependency during `Register` works until someone reorders the list.

```go
type PaymentsProvider struct{}

func (p *PaymentsProvider) Register(app *nimbus.App) error {
    app.Container.Singleton("payments", func() *stripe.Client {
        return stripe.New(config.Get().Stripe.Key)
    })
    return nil
}

func (p *PaymentsProvider) Boot(app *nimbus.App) error {
    app.Health.Add("stripe", pingStripe)
    return nil
}
```

## Optional hook interfaces

Implemented by a provider **or** a plugin; the app type-asserts for each during
boot, so you implement only what you need.

| Interface | Method | Effect |
| --- | --- | --- |
| `HasStart` | `Start(app *App) error` | Runs right before the server begins serving. **Skipped in `ModeWarmup`** |
| `HasRoutes` | `RegisterRoutes(r *router.Router)` | Mounts routes during the capability pass |
| `HasMiddleware` | `Middleware() map[string]router.Middleware` | Merged into `app.NamedMiddleware()` |
| `HasBindings` | `Bindings(c *container.Container)` | Container bindings without writing `Register` |
| `HasConfig` | `DefaultConfig() map[string]any` | Default config, readable via `app.PluginConfig(name)` |
| `HasMigrations` | `Migrations() []database.Migration` | Ships migrations with the plugin |
| `HasViews` | `ViewsFS() fs.FS` | Supplies an embedded `.nimbus` view filesystem |
| `HasCommands` | `Commands() []cli.Command` | Adds CLI commands |
| `HasSchedule` | `Schedule(s *schedule.Scheduler)` | Registers cron entries |
| `HasEvents` | `Listeners() map[string][]events.Listener` | Registers event listeners |
| `HasHealthChecks` | `HealthChecks() map[string]health.Check` | Adds checks to `app.Health` |
| `HasShutdown` | `Shutdown() error` | Cleanup on graceful shutdown. **Skipped in `ModeWarmup`** |

## Lifecycle in order

1. **Configuration** — `.env` and `config/` are loaded; `locale.BootFromEnv()`.
2. **Register** — every provider's `Register`.
3. **Boot** — every provider's `Boot`; dispatches `events.ProviderBoot`, then
   plugin boot dispatches `events.PluginBoot`.
4. **Capability pass** — plugin routes mounted (`events.RouteRegistered`),
   named middleware merged (`events.MiddlewareRegistered`), CLI commands, cron
   entries, and health checks collected. Dispatches `events.AppBooted`.
5. **WarmUp** — `OnWarmup` hooks run; the app is fully assembled. Dispatches
   `events.AppWarmed`.
6. **Run** — the listener binds, the startup view prints, `OnStart` hooks run,
   schedulers start. Dispatches `events.AppStarted`, then `events.AppReady`.
7. **Shutdown** — on SIGINT/SIGTERM, dispatches `events.AppShutdown` and runs
   shutdown hooks.

## Hooks

For one-off work you do not want a whole provider for:

```go
app.OnBoot(func(a *nimbus.App)     { ... })
app.OnWarmup(func(a *nimbus.App)   { ... })
app.OnStart(func(a *nimbus.App)    { ... })
app.OnShutdown(func(a *nimbus.App) { ... })
```

## Application modes

`nimbus.AppMode` decides how much of the lifecycle runs:

| Mode | Behaviour |
| --- | --- |
| `ModeRun` | Default — HTTP server, queue consumers, schedulers |
| `ModeWarmup` | Assemble and inspect only. `app.Run()` returns an error; plugin shutdown hooks are skipped. Used by `gen:client`, codegen, and test suites |
| `ModeTest` | Integration and unit tests |
| `ModeCli` | Artisan-style command execution |

```go
app := bin.Boot()
app.SetMode(nimbus.ModeWarmup)
if err := app.WarmUp(); err != nil { log.Fatal(err) }

routes := app.Router.Routes()   // no listener was ever opened
```

`WarmUp()` is idempotent and calls `Boot()` first if needed, so it is safe to
call from a test helper.

## Boot-time environment switches

| Variable | Effect |
| --- | --- |
| `NIMBUS_SERVE=1` | Running under `nimbus serve`; the app emits its boot report as a marker for the CLI to render, and the console request log stays on |
| `NIMBUS_DUMP_ROUTES=1` | Write the route manifest and exit without serving |
| `NIMBUS_GOGC` | `debug.SetGCPercent` value; `off` disables GC entirely (not recommended) |

## Guidance

1. One provider per capability. A provider that registers five unrelated
   services is a provider nobody can remove.
2. Never start a goroutine in `Register` — nothing is wired yet. Use `Boot` or
   `HasStart`.
3. Register global middleware before routes; middleware added afterwards does
   not rewrap already-registered routes.
