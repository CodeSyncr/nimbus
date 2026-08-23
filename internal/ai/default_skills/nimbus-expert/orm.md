# ORM (SQL) - Nimbus

Nimbus uses [GORM](https://gorm.io/) as its default ORM, but provides a higher-level abstraction for multiple database connections and query building.

## Core Features

-   **Multi-DB Connection Manager**: Easily manage multiple named database connections (e.g., "default", "analytics", "logs").
-   **GORM Integration**: Every connection is a `*gorm.DB` instance, allowing full access to GORM's features.
-   **Query Builder**: A custom wrapper around GORM for common operations like `Where`, `Order`, `Limit`, `Offset`, `Get`, `First`, etc.
-   **Model Embedding**: Standard `database.Model` struct (incorporating ID, CreatedAt, UpdatedAt) to ensure consistent data structures.
-   **Migrations**: AdonisJS Lucid-style schema builder for database migrations.

## Usage

### Connecting

Define connections in `config/database.go` and use `database.ConnectAll(configs)` or `database.AddConnection(name, db)`.

### Models

```go
type User struct {
    database.Model
    Name  string
    Email string `gorm:"uniqueIndex"`
}
```

### Querying

#### Default Connection
```go
users, _ := User{}.Where("is_active = ?", true).Get()
```

#### Named Connection
```go
events, _ := database.On("analytics").Where("event_type = ?", "click").Get(&events)
```

#### Raw GORM access
```go
db := database.Connection("default")
db.Raw("SELECT * FROM users").Scan(&users)
```

### Migrations

Create a migration:
```bash
nimbus make:migration create_users
```

Update `database/migrations/registry.go` and run:
```bash
nimbus db:migrate
```

## Advanced Patterns

-   **Relations**: Support for `HasOne`, `HasMany`, `BelongsTo`, `ManyMany` using GORM tags.
-   **Soft Deletes**: Use `gorm.DeletedAt` in your model to enable soft deletes.
-   **Pagination**: Built-in helper for paginated results (e.g., `Paginate(page, limit)`).
-   **Events/Hooks**: Global and per-model hooks (e.g., `BeforeSave`, `AfterCreate`).
