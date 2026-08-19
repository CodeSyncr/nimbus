# Framework Overview - Nimbus

Nimbus is an AdonisJS-style web framework for Go, prioritizing "convention over configuration," clear project structure, and high developer satisfaction (DX).

## Core Philosophy

-   **AdonisJS Inspiration**: Rooted in the principles of AdonisJS and Laravel, featuring a structured directory layout and powerful first-party packages.
-   **Provider/Plugin Architecture**: Highly extensible via Service Providers and Plugins.
-   **Go-Native**: Leverages the performance and safety of Go while providing a high-level API.

## Directory Structure

```text
├── app/
│   ├── controllers/
│   ├── models/
│   └── middleware/
├── bin/            # Server boot (bin/server.go)
├── config/         # Multi-file configuration
├── database/       # Migrations and seeds
├── start/          # Routes, kernel
├── public/
├── main.go
├── go.mod
└── .env
```

## Lifecycle

1.  **Configuration**: Load `.env` and `config/`.
2.  **Provider Registration**: Register all Service Providers (`Register(app)`).
3.  **Plugin Initialization**: Register and boot plugins (`Register` + `Boot`).
4.  **Routing & Capabilities**: Mount routes, merge middleware, register CLI commands, crons, and health checks.
5.  **WarmUp Phase**: Run `OnWarmup` hooks, assemble the app completely, and dispatch `events.AppWarmed`.
6.  **Run / HTTP Server**: Run `OnStart` hooks, start schedulers, dispatch `events.AppReady`, and serve HTTP traffic.

## Application Modes & Warmup

Nimbus supports 4 application modes (`nimbus.AppMode`):
- `nimbus.ModeRun` (default): Normal execution with HTTP server, queue consumers, and schedulers.
- `nimbus.ModeWarmup`: Inspection/assembly mode. Used by codegen tools (`nimbus gen:client`), CLI commands, and test suites. `app.Run()` returns an error, and plugin shutdown hooks are skipped.
- `nimbus.ModeTest`: Integration and unit test mode.
- `nimbus.ModeCli`: Artisan-style CLI command execution.

### Using app.WarmUp()

```go
app := bin.Boot()
app.SetMode(nimbus.ModeWarmup)

if err := app.WarmUp(); err != nil {
    log.Fatal(err)
}

// Safely inspect routes, container bindings, and plugins without starting network listeners
routes := app.Router.Routes()
```

## Key Components

### Router
-   High-performance Nimbus router with full RESTful and resourceful routing support.
-   Supports route groups, named routes, prefixes, and resources.
-   Dynamic parameter extraction (e.g., `:id`).

### HTTP Context (`*http.Context`)
-   Unified, developer-friendly interface for HTTP request lifecycle and response building.
-   First-party helpers: `c.JSON()`, `c.View()`, `c.Param()`, `c.Redirect()`, `c.Validate()`.
-   Built-in user context and authentication guards.

### Service Provider
-   `Register(app *App)`: Bind services to the container.
-   `Boot(app *App)`: Perform actions after all services are registered.

### Plugins
-   Modular extensions like `ai`, `mcp`, `drive` (storage), and `inertia`.
-   Can define their own routes, middleware, and CLI commands.
