# Migrations, Schema, Seeders & Factories

## Migrations

`nimbus make:migration create_users` writes
`database/migrations/<timestamp>_create_users.go`.

```go
package migrations

import (
    "github.com/CodeSyncr/nimbus"
    "github.com/CodeSyncr/nimbus/database/schema"
)

type CreateUsers struct {
    schema.BaseSchema
}

// TableName is the name this migration is tracked under.
func (m *CreateUsers) TableName() string { return "users" }

func (m *CreateUsers) Up(db *nimbus.DB) error {
    return schema.New(db).CreateTable("users", func(table *schema.Table) {
        table.Increments("id")
        table.String("name", 191)
        table.String("email", 191).Unique()
        table.String("password", 255)
        table.Boolean("active").Default("true")
        table.Timestamps()
        table.SoftDeletes()
        table.Index("email")
    })
}

func (m *CreateUsers) Down(db *nimbus.DB) error {
    return schema.New(db).DropTable("users")
}
```

Note the shape: `Up`/`Down` take a `*nimbus.DB`, and you build a
`schema.New(db)` inside. Embedding `schema.BaseSchema` supplies the rest of the
interface.

**Register it.** Generators print a reminder but do not edit the file: add
`&CreateUsers{}` to `database/migrations/registry.go` or the migration never
runs.

### Running

| Command | Effect |
| --- | --- |
| `nimbus db:migrate` | Run pending migrations |
| `nimbus migrate:status` | Show each migration and whether it has run |
| `nimbus migrate:fresh` | Drop every table and re-run everything |

See [cli](cli.md) for the app-delegation caveat — these shell out to
`go run . <arg>` and need your `main.go` to dispatch them.

### The migrator

`database.NewMigrator(db, migrations)` gives you `Up()`, `Down()`, `Fresh()`,
`Status() ([]MigrationStatus, error)`, and `PrintStatus()`.

- State lives in a `schema_migrations` table with a **batch** number, so
  `Down()` rolls back the last batch, not the last file.
- On dialects that support transactional DDL the whole migration runs in a
  transaction and fully rolls back on failure. On those that do not (MySQL),
  a failure leaves partial DDL behind — write migrations that are safe to
  re-run.
- `RunMigrationsFromDir(db, dir)` runs migrations from a directory instead of a
  registry.

## Schema builder

`schema.New(db)` → `CreateTable(name, fn)`, `AlterTable(name, fn)`,
`DropTable(name)`.

### Column types

| Group | Methods |
| --- | --- |
| Keys | `ID()`, `Increments`, `BigIncrements`, `UUIDPrimary()`, `UUID` |
| Text | `String(name, size)`, `Text`, `LongText`, `Enum(name, values)` |
| Numbers | `Integer`, `SmallInteger`, `BigInteger`, `Float(name, p, s)`, `Decimal(name, p, s)` |
| Boolean | `Boolean` |
| Time | `Date`, `Time`, `DateTime`, `Timestamp`, `LegacyTimestamp`, `Timestamps()`, `SoftDeletes()` |
| Structured | `JSON`, `JSONB`, `Binary` |
| Relations | `ForeignId(column, references)` |

`Timestamps()` adds `created_at`/`updated_at`; `SoftDeletes()` adds
`deleted_at`.

> **`Timestamp` vs `LegacyTimestamp`.** `Timestamp` maps to a type that is not
> subject to the 2038 overflow (on SQLite it emits `datetime`).
> `LegacyTimestamp` emits the old `TIMESTAMP` keyword and exists for
> compatibility with existing schemas. Use `Timestamp` for anything new.
> `nimbus make:migration fix_y2038 --auto-detect` scans MySQL `TIMESTAMP`
> columns and generates the fix.

### Modifiers

Chain after a column: `Nullable()`, `NotNull()`, `Default(val)`, `Unique()`,
`Unsigned()`, `Primary()`, `Comment(text)`, `After(col)`, `First()`.

### Indexes

`Index(column)`, `CompositeIndex(cols)`, `UniqueComposite(cols)`.

## Seeders

`nimbus make:seeder UserSeeder` → `database/seeders/user_seeder.go`. Run with
`nimbus db:seed` (delegates to `go run . seed`).

A seeder implements one method:

```go
type Seeder interface {
    Run(db *lucid.DB) error
}
```

`database.SeedFunc` adapts a plain `func(*lucid.DB) error` to the interface.
`database.NewSeedRunner(db, seeders).Run()` executes them in order.

Seeders should be **idempotent** — `db:seed` gets run repeatedly in development,
and a seeder that blindly inserts will duplicate rows. Use a find-or-create.

## Factories

`nimbus make:factory User` → `database/factories/user_factory.go`. A factory is
defined against a **table name** and returns a map of column values:

```go
var UserFactory = database.Define("users", func(f *database.Faker) map[string]any {
    return map[string]any{
        "name":       f.Sentence(),
        "email":      f.Email(),
        "active":     true,
        "login_count": f.Int(0, 50),
    }
})

// insert one row
err := UserFactory.Create(db)

// override specific fields
err := UserFactory.Create(db, map[string]any{"email": "known@example.com"})
```

`Create(db *lucid.DB, attrs ...map[string]any) error` merges each `attrs` map
over the generated values, so a test states only the fields it cares about.

`Faker` is intentionally minimal: `Sentence()`, `Paragraph()`, `Word()`,
`Email()`, `Int(min, max)`. It is not a general fake-data library — for
anything richer, generate the value yourself in the define function.

Related: [orm](orm.md) for models and queries, [testing](testing.md) for using
factories in tests.
