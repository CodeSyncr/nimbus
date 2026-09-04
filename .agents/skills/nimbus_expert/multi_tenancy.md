# Multi-Tenancy

Resolve a tenant per request and scope the database to it.

## Configuration

```go
mgr := tenancy.New(tenancy.Config{ ... })
mgr.SetStore(tenancy.NewGormStore(db))
app.Router.Use(mgr.Middleware())
```

Two axes control behaviour:

- **`ResolveMethod`** — how the tenant is identified from the request
  (subdomain, domain, header, path, and so on). `mgr.Resolve(r) (string, error)`
  is the underlying call.
- **`Strategy`** — how data is isolated: separate row scope, separate schema, or
  separate database.

## In a handler

```go
func Index(c *http.Context) error {
    t  := tenancy.Current(c)   // *Tenant
    id := tenancy.ID(c)        // tenant id
    db := tenancy.DB(c)        // *lucid.DB already scoped to the tenant

    var projects []Project
    db.Find(&projects)         // only this tenant's rows
    return c.JSON(200, projects)
}
```

**Always use `tenancy.DB(c)`, never the global database handle.** The global
handle is unscoped; one query through it is a cross-tenant data leak. The
package has a dedicated test for schema-strategy isolation (`TestScopeSchema_Security`)
because this is the failure mode that matters.

## Tenant registry

| Method | Purpose |
| --- | --- |
| `Register(t *Tenant)` | Add a tenant in memory |
| `Get(id) (*Tenant, bool)` | Look one up |
| `SetStore(TenantStore)` | Back the registry with persistence |
| `ScopeDB(tenant) (*lucid.DB, error)` | Build a scoped handle directly |

`TenantStore` is an interface — `FindByID`, `FindByDomain`, `All`, `Save`,
`Delete`. `NewGormStore(db)` is the built-in implementation.

## As a plugin

```go
app.RegisterPlugin(tenancy.NewPlugin(cfg))
```

`TenantPlugin` registers the middleware under a name (via `Middleware()`) and
mounts tenant management routes.

## Choosing a strategy

| Strategy | Isolation | Cost |
| --- | --- | --- |
| Row scope | Weakest — one missed `WHERE` leaks | Cheapest; one schema, one connection pool |
| Schema | Strong within a database | Migrations must run per schema |
| Database | Strongest | Connection pool per tenant; heaviest to operate |

Pick on your compliance requirement, not on convenience — moving from row scope
to schema isolation later is a data migration, not a config change.

## Guidance

1. Scope in one place (the middleware), then never think about it again.
2. Background jobs do not have a request — pass the tenant id in the job payload
   and re-scope explicitly inside the handler.
3. Migrations under schema or database strategies must iterate tenants; a single
   `db:migrate` will not reach them.
