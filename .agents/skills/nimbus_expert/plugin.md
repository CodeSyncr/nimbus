# Plugin System - Nimbus

Nimbus is designed to be highly extensible through its plugin system. Plugins allow for modular functionality that can be easily added to any Nimbus application.

## Plugin Interface

A Nimbus plugin must implement the `Plugin` interface:

```go
type Plugin interface {
    Name() string
    Register(app *App) error
    Boot(app *App) error
}
```

### Optional Interfaces

Plugins can also implement optional interfaces to provide additional capabilities:

-   **HasRoutes**: `RegisterRoutes(router *router.Router)` to add HTTP routes.
-   **HasMiddleware**: `Middleware() map[string]router.Middleware` to register named middleware.
-   **HasConfig**: `DefaultConfig() map[string]any` to provide default configuration values.
-   **HasCommands**: `Commands() []cli.Command` to add CLI commands (e.g., `nimbus myplugin:cmd`).
-   **HasSchedule**: `Schedule(s *schedule.Scheduler)` to register scheduled tasks.
-   **HasEvents**: `Listeners() map[string][]events.Listener` to listen for framework or application events.
-   **HasHealthChecks**: `HealthChecks() map[string]health.Check` to add health monitoring.
-   **HasShutdown**: `Shutdown() error` for graceful cleanup.

## Lifecycle

1.  **Register**: Called during `app.Boot()`. Use this to bind services to the container.
2.  **Boot**: Called after all plugins have been registered. Use this to perform initialization that requires other services.
3.  **Apply Capabilities**: After booting, the framework automatically extracts and applies routes, middleware, commands, etc., from the plugins.

## Usage

To use a plugin, register it in `bin/server.go`:

```go
app := nimbus.New()
app.Use(
    ai.New(),
    inertia.New(),
    mcp.New(),
)
```

## Creating a Plugin

1.  Define a struct that implements `Plugin`.
2.  Implement `Name()`, `Register()`, and `Boot()`.
3.  Add any optional capability interfaces.
4.  Export a `New()` function to instantiate the plugin.

## Best Practices

-   **Namespace Hooks**: Use unique names for routes and commands to avoid collisions.
-   **Graceful Shutdown**: Implement `HasShutdown` if your plugin uses external resources like database connections or background goroutines.
-   **Configuration**: Provide defaults via `HasConfig` but allow users to override them in their applications.
