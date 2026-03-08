# Nimbus

**AdonisJS-style web framework for Go.** Convention over configuration, clear structure, and a pleasant DX.

**Repository:** [github.com/CodeSyncr/nimbus](https://github.com/CodeSyncr/nimbus)

## Features

- **Router** – Express-style routes with `:param` placeholders, route groups, and middleware
- **Context** – Request/response helpers: `JSON()`, `Param()`, `Redirect()`
- **Config** – Environment-based config (`.env` + `config/`)
- **Middleware** – Global and per-route middleware (Logger, Recover, CORS)
- **Validation** – Struct validation with [go-playground/validator](https://github.com/go-playground/validator)
- **Database** – GORM-based models with `database.Model` (ID, timestamps), migrations support
- **CLI** – `nimbus new`, `make:model`, `make:migration` (Ace-style)

## Project structure (AdonisJS-inspired)

```
├── app/
│   ├── controllers/
│   ├── models/
│   └── middleware/
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

Ensure `$HOME/go/bin` is in your PATH (add to `~/.zshrc` if needed):

```bash
export PATH="$HOME/go/bin:$PATH"
```

Then run `nimbus serve` from your app directory. If you get "command not found", either add the export above and restart the terminal, or run the app with `go run main.go` instead.

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

### Use the framework in your own app

```go
package main

import (
	"net/http"
	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/context"
	"github.com/CodeSyncr/nimbus/middleware"
)

func main() {
	app := nimbus.New()
	app.Router.Use(middleware.Logger(), middleware.Recover())

	app.Router.Get("/", func(c *context.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"hello": "nimbus"})
	})
	app.Router.Get("/users/:id", func(c *context.Context) error {
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

// Connect (e.g. in main)
db, _ := database.Connect(app.Config.Database.Driver, app.Config.Database.DSN)

// Model (embed database.Model)
type User struct {
	database.Model
	Name  string
	Email string
}
db.AutoMigrate(&User{})
```

### Validation

```go
import "github.com/CodeSyncr/nimbus/validation"

type CreateUserRequest struct {
	Name  string `json:"name" validate:"required,min=2"`
	Email string `json:"email" validate:"required,email"`
}

func createUser(c *context.Context) error {
	var req CreateUserRequest
	if err := validation.ValidateRequestJSON(c.Request.Body, &req); err != nil {
		return c.JSON(http.StatusUnprocessableEntity, map[string]any{"errors": err})
	}
	// ...
}
```

## Commands

| Command | Description |
|--------|-------------|
| `nimbus new <name>` | Create a new Nimbus app |
| `nimbus serve` | Run the app (from app root; like AdonisJS `ace serve`) |
| `nimbus make:model <Name>` | Scaffold a model |
| `nimbus make:migration <name>` | Scaffold a migration (placeholder) |

## Publishing (for maintainers)

1. **Push to GitHub** (repo must be public for `go get`):
   ```bash
   git remote add origin https://github.com/CodeSyncr/nimbus.git   # if not already set
   git push -u origin main
   ```

2. **Tag a version** (so users can pin versions):
   ```bash
   git tag v0.1.0
   git push origin v0.1.0
   ```

3. **Install CLI** (others can install from the repo):
   ```bash
   go install github.com/CodeSyncr/nimbus/cmd/nimbus@latest
   ```

4. **Use in another project**:
   ```bash
   go get github.com/CodeSyncr/nimbus@v0.1.0
   ```
   After the first fetch, the module appears on [pkg.go.dev](https://pkg.go.dev) automatically.

## License

MIT
