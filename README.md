# Nimbus
https://agentrouter.org/register?aff=iHLj
**Laravel-inspired web framework for Go.** Convention over configuration, clear structure, and a pleasant DX.

**Repository:** [github.com/CodeSyncr/nimbus](https://github.com/CodeSyncr/nimbus)

## Versioning & stability (v1.0.0)

Nimbus **v1.0.0** is the first release with a **stable core** under [Semantic Versioning](https://semver.org/): we avoid breaking changes to the packages listed below; when unavoidable, we prefer deprecation for one **minor** release before a **major** bump.

### Stable in v1 (SemVer)

These packages are intended as stable building blocks for applications:

`router`, `http`, `middleware`, `validation`, `config`, `session`, `auth`, `database`, `lucid`, `queue`, `mail`, `errors`, `schedule`, `locale`, `health`, `metrics`, `logger`, `cache`, `encryption`, `resource`, `notification`, `hash`, `container`, `view`, and the **Nimbus CLI** (`cmd/nimbus`).

### Evolving / preview

- **`plugins/telescope`** — Debugging dashboard; many panels are still placeholders. Treat as **preview**; UI and internal APIs may evolve in minor releases until called out as stable in release notes.
- **Integration-style plugins** (`plugins/ai`, Scout, Socialite, etc.) — Pin versions in production; follow release notes for breaking changes until each plugin is explicitly marked stable.
- **`studio`** — Optional tooling; not part of the core stability promise.

### Not in v1

- **OAuth / first-party API tokens** (Laravel Sanctum/Passport-class) — Not shipped in v1; plan your own bearer tokens or wait for a future release (see `CHANGELOG.md`).
- **Default HTML 404/500 pages** — Applications should use `router.Fallback` and custom handlers; the core error stack focuses on structured JSON/API responses.

### Requirements

- **Go 1.26+** (see [`go.mod`](./go.mod)). CI uses `go-version-file: go.mod`.

Release checklist: **[V1_RELEASE.md](./V1_RELEASE.md)** · History: **[CHANGELOG.md](./CHANGELOG.md)**

## Features

- **Router** – Express-style routes with `:param` placeholders, route groups, and middleware
- **Context** – Request/response helpers: `JSON()`, `Param()`, `Redirect()`
- **Config** – Environment-based config (`.env` + `config/`)
- **Middleware** – Global and per-route middleware (Logger, Recover, CORS)
- **Validation** – Struct validation with [go-playground/validator](https://github.com/go-playground/validator)
- **Database** – GORM-based models with `database.Model` (ID, timestamps), migrations support
- **CLI** – `nimbus new`, `make:model`, `make:migration` (Ace-style)

## Project structure (Laravel-inspired)

```
├── app/
│   ├── controllers/
│   ├── models/
│   └── middleware/
├── bin/            # Server boot (bin/server.go)
├── config/
├── database/
│   └── migrations/
├── start/          # Routes, kernel (optional)
├── public/
├── main.go
├── go.mod
└── .env
```

## Quick start

### Install CLI

From the **nimbus** repo directory:

```bash
cd /path/to/nimbus
go install ./cmd/nimbus
```

**If you get `zsh: command not found: nimbus`**, add Go’s bin directory to your PATH. For zsh, run once:

```bash
echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.zshrc && source ~/.zshrc
```

Then run `nimbus` again. You can also run your app without the CLI: `go run main.go` (no hot reload) or `go run github.com/air-verse/air@v1.52.3` (hot reload).

**Hot reload:** `nimbus serve` runs [air](https://github.com/air-verse/air) via `go run`, so you don’t install anything extra. The first run may download air once; after that, edits to `.go` and `.nimbus` files restart the app automatically. No need to add air to your app’s `go.mod` or run `go mod tidy` for it. Press **Ctrl+C** to stop the server; it shuts down gracefully and releases the port.

### Create a new app

```bash
nimbus new myapp
cd myapp
go mod tidy
nimbus serve
```

Server runs at `http://localhost:3333`. You can also run `go run main.go` directly.

**If you see** `reading ../go.mod: no such file or directory` when running `nimbus serve`: your app’s `go.mod` has `replace github.com/CodeSyncr/nimbus => ../`, which points at the parent directory. If the app lives **outside** the nimbus repo (e.g. as a sibling), change it to:

```go
replace github.com/CodeSyncr/nimbus => ../nimbus
```

So the path after `=>` is the directory that contains the nimbus `go.mod`.

**If you see** `missing go.sum entry for module providing package ... (imported by github.com/CodeSyncr/nimbus/...)`: your app’s `go.sum` is missing transitive dependencies from the local nimbus module. From your **app** directory run:

```bash
go mod tidy
```

Then run `nimbus serve` again.

### Use the framework in your own app

```go
package main

import (
	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/middleware"
)

func main() {
	app := nimbus.New()
	app.Router.Use(middleware.Logger(), middleware.Recover())

	app.Router.Get("/", func(c *http.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"hello": "nimbus"})
	})
	app.Router.Get("/users/:id", func(c *http.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"id": c.Param("id")})
	})

	// Route groups
	api := app.Router.Group("/api")
	api.Get("/posts", listPosts)
	api.Post("/posts", createPost)

	_ = app.Run()
}
```

### Config & env

Set `PORT`, `APP_ENV`, `APP_NAME`, `DB_DRIVER`, `DB_DSN` in `.env`. Config is loaded via `config.Load()` in `nimbus.New()`.

### Database & models

```go
import "github.com/CodeSyncr/nimbus/database"

// Connect (e.g. in bin/server.go)
db, _ := database.Connect(config.Database.Driver, config.Database.DSN)

// Model (embed database.Model)
type User struct {
	database.Model
	Name  string
	Email string
}
db.AutoMigrate(&User{})
```

### Migrations

Run migrations from your app root:

```bash
nimbus db:migrate
```

Or directly: `go run . migrate`. Migrations use a Laravel-inspired schema builder. Create one with:

```bash
nimbus make:migration create_users
```

Then add the new migration to `database/migrations/registry.go`. Each migration runs once; already-run migrations are tracked in `schema_migrations` and shown as *skipped* on subsequent runs.

### Validation

```go
import "github.com/CodeSyncr/nimbus/validation"

type CreateUserRequest struct {
	Name  string `validate:"required,min=2"`
	Email string `validate:"required,email"`
}

func createUser(c *http.Context) error {
	var req CreateUserRequest
	if err := validation.ValidateRequestJSON(c.Request.Body, &req); err != nil {
		return c.JSON(http.StatusUnprocessableEntity, map[string]any{"errors": err})
	}
	// ...
}
```

### Views (.nimbus, Edge-style)

Put templates in a `views/` folder with the **`.nimbus`** extension. Use `c.View("name", data)` to render in a Laravel-inspired workflow.

**Syntax (Edge-aligned):**

| Nimbus | Description |
|--------|-------------|
| `{{ variable }}` | Output, HTML-escaped |
| `{{{ variable }}}` | Output, unescaped (for rich content) |
| `{{-- comment --}}` | Comment (stripped from output) |
| `@if(cond)` … `@elseif(cond)` … `@else` … `@endif` | Conditionals |
| `@each(items)` … `@endeach` | Loop; use `{{ . }}` for current item |
| `@each(post in posts)` … `@endeach` | Loop with named var; use `{{ $post }}` |
| `@layout('layout')` | Wrap with layout; layout uses `{{ .embed }}` or `{{ .content }}` |
| `{{ .csrfField }}` | CSRF hidden input (auto-injected when Shield enabled) |
| `@dump(posts)` | Debug: pretty-print variable (or `@dump(state)` for all) |
| `@card()` … `@end` | Component: `views/components/card.nimbus` becomes `@card()` |

**Components:** Create `views/components/card.nimbus`; use `@card()` … `@end` in templates. Render the main slot with `{{{ .slots.main }}}` (Edge-style).

**Example:** `views/home.nimbus`

```
@layout('layout')
<h2>Hello, {{ name }}!</h2>
@if(.items)
  @each(items)
  <li>{{ . }}</li>
  @endeach
@else
  <p>No items.</p>
@endif
```

**In your handler:**

```go
return c.View("home", map[string]any{"name": "Guest", "title": "Home", "items": []string{"A", "B"}})
```

Views are loaded from the `views/` directory by default. Change with `view.SetRoot("custom/views")` in `main.go`.

## Plugins & packages

### Default plugins (auto-registered with `nimbus new`)

| Plugin | Description | Docs |
|--------|-------------|------|
| [Drive](plugins/drive/README.md) | File storage (fs, S3, GCS, R2, Spaces, Supabase) | [README](plugins/drive/README.md) |
| [Transmit](plugins/transmit/README.md) | SSE for real-time server-to-client push | [README](plugins/transmit/README.md) |

### Core packages

| Package | Description | Docs |
|---------|-------------|------|
| [Queue](queue/README.md) | Background jobs (sync, Redis, database, SQS, Kafka) | [README](queue/README.md) |

### Additional plugins (`nimbus plugin install <name>`)

| Plugin | Description | Docs |
|--------|-------------|------|
| [Horizon](plugins/horizon/README.md) | Queue dashboard, metrics, failed jobs (Redis) | [README](plugins/horizon/README.md) |
| Pulse | Lightweight request / app metrics middleware | `plugins/pulse` |
| [Reverb](plugins/reverb/README.md) | WebSocket channel broadcasting (optional Redis fan-out) | [README](plugins/reverb/README.md) |
| [AI](plugins/ai/README.md) | AI integration (OpenAI, Ollama, Anthropic, etc.) | [README](plugins/ai/README.md) |
| [Inertia](plugins/inertia/README.md) | Inertia.js for Vue/React/Svelte SPAs | [README](plugins/inertia/README.md) |
| [Telescope](plugins/telescope/README.md) | Debugging and introspection dashboard | [README](plugins/telescope/README.md) |
| [MCP](plugins/mcp/README.md) | Model Context Protocol for AI clients | [README](plugins/mcp/README.md) |
| Unpoly | Progressive enhancement and partial page updates | `nimbus plugin install unpoly` |

### Redis

Redis is used by **Queue** (`QUEUE_DRIVER=redis`), **Transmit** (`TRANSMIT_TRANSPORT=redis`), **Horizon** (failed jobs + live queue depths), and **Reverb** (multi-instance WebSocket fan-out). Set `REDIS_URL=redis://localhost:6379` in `.env` when using these features.

## Commands

| Command | Description |
|--------|-------------|
| `nimbus new <name>` | Create a new Nimbus app |
| `nimbus serve` | Run the app from the app root |
| `nimbus db:migrate` | Run database migrations |
| `nimbus db:rollback` | Rollback the last migration |
| `nimbus make:model <Name>` | Scaffold a model |
| `nimbus make:migration <name>` | Scaffold a migration |
| `nimbus queue:work` | Run the queue worker (processes background jobs) |
| `nimbus plugin install <name>` / `nimbus plugin:install <name>` | Install a plugin (horizon, reverb, telescope, inertia, ai, mcp, …) |

## Publishing (for maintainers)

1. **Push to GitHub** (repo must be public for `go get`):
   ```bash
   git remote add origin https://github.com/CodeSyncr/nimbus.git   # if not already set
   git push -u origin main
   ```

2. **Tag a version** (so users can pin versions):
   ```bash
   git tag v1.0.0
   git push origin v1.0.0
   ```

3. **Install CLI** (others can install from the repo):
   ```bash
   go install github.com/CodeSyncr/nimbus/cmd/nimbus@latest
   ```

4. **Use in another project**:
   ```bash
   go get github.com/CodeSyncr/nimbus@v1.0.0
   ```
   After the first fetch, the module appears on [pkg.go.dev](https://pkg.go.dev) automatically.

## License

MIT
