# Nimbus

**AdonisJS-style web framework for Go.** Convention over configuration, clear structure, and a pleasant DX.

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

### Install CLI (from repo)

```bash
go install ./cmd/nimbus
```

### Create a new app

```bash
nimbus new myapp
cd myapp
go mod tidy
go run main.go
```

Server runs at `http://localhost:3333`.

### Use the framework in your own app

```go
package main

import (
	"net/http"
	"github.com/nimbus-framework/nimbus"
	"github.com/nimbus-framework/nimbus/context"
	"github.com/nimbus-framework/nimbus/middleware"
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
import "github.com/nimbus-framework/nimbus/database"

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
import "github.com/nimbus-framework/nimbus/validation"

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
| `nimbus make:model <Name>` | Scaffold a model (placeholder) |
| `nimbus make:migration <name>` | Scaffold a migration (placeholder) |

## License

MIT
