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
2.  **Provider Registration**: Register all Service Providers.
3.  **Plugin Initialization**: Register and boot plugins (AI, Inertia, etc.).
4.  **Routing**: Apply global and per-route middleware, then register routes.
5.  **Hooks**: Run `OnBoot` and `OnStart` callbacks.
6.  **HTTP Server**: Start listening and serving requests.

## Key Components

### Router
-   Wraps [go-chi/chi](https://github.com/go-chi/chi) for performance.
-   Supports route groups, named routes, and resources.
-   Dynamic parameter extraction (e.g., `:id`).

### Context
-   Unified access to `http.Request` and `http.ResponseWriter`.
-   Helpers: `c.JSON()`, `c.View()`, `c.Param()`, `c.Redirect()`.
-   Supports user context for authentication.

### Service Provider
-   `Register(app *App)`: Bind services to the container.
-   `Boot(app *App)`: Perform actions after all services are registered.

### Plugins
-   Modular extensions like `ai`, `mcp`, `drive` (storage), and `inertia`.
-   Can define their own routes, middleware, and CLI commands.
