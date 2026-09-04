# Lucid — the database handle

`lucid` is Nimbus's database layer. It is a thin, deliberate alias over GORM
rather than a wrapper:

```go
type DB         = gorm.DB
type Config     = gorm.Config
type Dialector  = gorm.Dialector
type Option     = gorm.Option
type DeletedAt  = gorm.DeletedAt
type Session    = gorm.Session
type Statement  = gorm.Statement

func Open(dialector Dialector, opts ...Option) (*DB, error)
```

## What that means in practice

1. **`*lucid.DB` *is* `*gorm.DB`.** Every GORM method, plugin, and piece of
   documentation applies unchanged. Anything typed `*gorm.DB` can be passed
   where `*lucid.DB` is expected and vice versa — they are the same type, not
   convertible types.
2. **Import `lucid`, not `gorm`, in application code.** The alias is the seam:
   framework code, plugin signatures, and generated scaffolding all speak
   `*lucid.DB`, and going through it keeps your code aligned with the
   framework's version of GORM.
3. **No behaviour is added or removed.** If a query behaves oddly, the answer is
   in GORM's documentation.

## Opening a connection

Usually you do not — `bootDatabase(app)` in `bin/server.go` does it from
`config`. Open directly only for a second connection:

```go
db, err := lucid.Open(postgres.Open(dsn), &lucid.Config{ ... })
```

## Where `*lucid.DB` shows up

| Package | Use |
| --- | --- |
| `database` | `NewMigrator(db, migrations)`, seeding, factories |
| `database/schema` | `schema.New(db)` |
| `session` | `NewDatabaseStore(db, table)` |
| `tenancy` | `NewGormStore(db)`, `ScopeDB`, `tenancy.DB(c)` |
| `validation` | `SetDB(fn func() *lucid.DB)` — powers the `unique` and `exists` rules |

## Soft deletes

`lucid.DeletedAt` is the field type; `schema.Table.SoftDeletes()` adds the
column. A model with a `DeletedAt` field is excluded from queries automatically
— use `Unscoped()` to see deleted rows.

## Guidance

1. Keep raw SQL behind a repository function; a handler holding a `*lucid.DB`
   and building queries inline is a handler you cannot test without a database.
2. Use `Session` for scoped behaviour changes (dry run, prepared statements)
   rather than mutating the shared handle.
3. Under multi-tenancy always take the handle from `tenancy.DB(c)` — see
   [multi_tenancy](multi_tenancy.md).

Related: [orm](orm.md) for models and relationships,
[database_migrations](database_migrations.md) for schema.
