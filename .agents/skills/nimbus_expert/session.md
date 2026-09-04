# Sessions

Cookie-backed sessions with pluggable stores.

```go
app.Router.Use(session.Middleware(session.Config{
    Store:      session.NewRedisStore(rdb),
    CookieName: "nimbus_session",
    MaxAge:     24 * time.Hour,
    Secure:     true,
    HttpOnly:   true,
    SameSite:   session.SameSiteLax,
}))
```

## Using a session in a handler

```go
func Login(c *http.Context) error {
    s := session.FromContext(c.Ctx())
    s.Set("user_id", user.ID)
    s.Regenerate()                       // new id — do this on privilege change
    s.SetFlash("status", "Welcome back")
    return c.Redirect(302, "/dashboard")
}
```

| Method | Behaviour |
| --- | --- |
| `Get(key) any` | Read a value |
| `Set(key, val)` | Write a value |
| `Delete(key)` | Remove a value |
| `Regenerate()` | Issue a new session id, keeping the data |
| `SetFlash(key, val)` | Value that survives exactly one request |
| `GetFlash(key) any` | Read and consume a flash value |

`session.FromContext(ctx) *Session` is the only way to reach the session; the
middleware puts it on the request context.

**Always call `Regenerate()` on login.** Keeping the pre-authentication id is
session fixation.

## Stores

`Store` is an interface — `Get`, `Set`, `Destroy` — so any backend works.

| Store | Constructor | Notes |
| --- | --- | --- |
| Memory | `NewMemoryStore()` | Process-local; lost on restart, wrong for multi-instance |
| Cookie | `NewCookieStore(key []byte)` | Encrypted client-side; no server state, but size-limited |
| Redis | `NewRedisStore(client)` / `NewRedisStoreWithPrefix(client, prefix)` | The usual production choice |
| Database | `NewDatabaseStore(db, table)` | Call `EnsureTable()` once; rows are `SessionRecord` |

`session.KeyFromString(s) []byte` derives a cookie-store key from a string —
feed it `APP_KEY`, not a literal.

### Choosing

- **Cookie store**: no infrastructure, but every byte rides on every request and
  you cannot invalidate a session server-side.
- **Redis**: fast, shared across instances, supports immediate invalidation.
- **Database**: same benefits as Redis without another service, at the cost of a
  query per request.

## Config

```go
type Config struct {
    Store      Store
    CookieName string
    MaxAge     time.Duration
    HttpOnly   bool
    Secure     bool
    SameSite   http.SameSite
}
```

Note the spelling: `CookieName` and `HttpOnly`. The package re-exports
`SameSiteLax`, `SameSiteStrict`, and `SameSiteNone` so you do not need to import
`net/http` for them. Set `Secure: true` anywhere you terminate TLS.

The middleware writes the session cookie lazily through a wrapped
`ResponseWriter`, so the cookie is set before the first body byte no matter
where in the handler you touch the session.

Related: [auth](auth.md) for the session guard built on top of this.
