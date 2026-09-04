# HTTP Context

`http.Context` is the single object a handler receives. It wraps the standard
`*http.Request` / `http.ResponseWriter`, adds request parsing, a per-request
store, response builders, and streaming. Handlers have the signature
`func(*http.Context) error`.

```go
func Show(c *http.Context) error {
    id := c.Param("id")
    user, err := models.FindUser(id)
    if err != nil {
        return c.NotFound("user not found")
    }
    return c.JSON(200, user)
}
```

## Route and request identity

| Method | Returns |
| --- | --- |
| `Param(name) string` | Route parameter (`/users/:id` → `Param("id")`) |
| `Method() string` | HTTP verb |
| `Path() string` | Request path |
| `FullURL() string` | Scheme + host + path + query |
| `IP() string` | Client IP — see the warning below |
| `UserAgent() string` | `User-Agent` header |
| `Header(key) string` | Read a request header |
| `IsAjax() bool` | `X-Requested-With: XMLHttpRequest` |
| `IsJSON() bool` | Content type is JSON |

> **`IP()` trusts headers unconditionally.** It returns the first entry of
> `X-Forwarded-For`, then `X-Real-IP`, and only then `RemoteAddr`. Both headers
> are client-settable, so a caller can claim any address. Put
> `middleware.TrustedProxies(cidrs...)` in front of anything that makes a
> security decision on `IP()` — rate limiting, allow-lists, audit logs.

## Reading input

`Input*` reads form **and** query; `Query*` reads the query string only.

| Method | Notes |
| --- | --- |
| `Input(key, def ...string) string` | Form or query, optional default |
| `InputInt(key, def int) int` | |
| `InputBool(key) bool` | |
| `InputFloat(key, def float64) float64` | |
| `Query(key, def ...string) string` | Query only |
| `QueryInt(key, def int) int` | |
| `QueryBool(key) bool` | |
| `All() map[string]string` | Every input |
| `Only(keys ...string) map[string]string` | Whitelist |
| `Except(keys ...string) map[string]string` | Blacklist |
| `Has(key) bool` | Key present |
| `Filled(keys ...string) bool` | All keys present **and** non-empty |

### Binding into structs

| Method | Source |
| --- | --- |
| `Bind(v any) error` | Content-type aware — JSON, form, or query |
| `BindJSON(v any) error` | Request body as JSON |
| `BindForm(v any) error` | Form values |
| `BindQuery(v any) error` | Query string |

`Bind` is the one to reach for; the others are for when you must be explicit.

## File uploads

| Method | Purpose |
| --- | --- |
| `File(name) (multipart.File, *multipart.FileHeader, error)` | Single upload |
| `Files(name) []*multipart.FileHeader` | Multiple uploads |
| `HasFile(name) bool` | Presence check |
| `SaveUploadedFile(file *multipart.FileHeader, dst string) error` | Persist to disk |

## Cookies

```go
c.SetCookie("session", id, 3600, "/", "", true /*secure*/, true /*httpOnly*/)
v := c.Cookie("session", "fallback")
c.ClearCookie("session", "/", "")
```

## Per-request store

Middleware puts values here; handlers read them.

| Method | Behaviour |
| --- | --- |
| `Set(key, val)` | Store a value |
| `Get(key) (any, bool)` | Read with an ok flag |
| `MustGet(key) any` | Read, panics if absent |
| `Require(key) (any, error)` | Read, error if absent |

The router itself stores `route_method`, `route_path`, `route_handler`, and
`route_middleware` on every request — that is how Telescope and the request log
know which route matched.

## Responses

| Method | Emits |
| --- | --- |
| `JSON(code, body) error` | JSON |
| `String(code, s)` / `Text(code, s)` | `text/plain` |
| `HTML(code, html)` | `text/html` |
| `View(name, data) error` | Renders a `.nimbus` template |
| `Data(code, contentType, []byte)` | Raw bytes |
| `NoContent()` | 204 |
| `Redirect(code, url)` | Location redirect |

### Status shortcuts

`Created`, `Accepted`, `BadRequest`, `NotFound`, `Forbidden`, `Unauthorized`,
`ServerError`, `ValidationErrors`. Each returns `error` so it can be the
handler's return value directly.

```go
return c.Unauthorized("token expired")
```

### Fluent response builders

These return `*Context` so they chain before a terminal method:

```go
return c.Status(201).
    ContentType("application/json").
    CacheControl("public, max-age=300").
    JSON(201, payload)
```

`Status`, `ContentType`, `CacheControl`, `NoCache`, `Expires`, `LastModified`.
`StatusCode()` reads back what was set.

### Files

| Method | Behaviour |
| --- | --- |
| `Download(filePath, fileName) error` | `Content-Disposition: attachment` |
| `Inline(filePath, fileName) error` | `Content-Disposition: inline` |
| `SendFile(dir, file)` | Serve from a directory |
| `Attachment(filename)` | Set the disposition header only |

### Streaming and SSE

```go
// Chunked writes
return c.Stream(200, "text/plain", func(w io.Writer) error { ... })

// Streaming JSON encoder
return c.StreamJSON(200, func(enc *json.Encoder) error { ... })

// One server-sent event
return c.SSE("message", `{"ok":true}`)

// A full SSE stream
return c.SSEStream(func(w *SSEWriter) error { ... })
```

`Write`, `WriteString`, and `Flush` give raw access when you need it.

## Aborting

`Abort(code) error` and `AbortWithJSON(code, body) error` stop the chain and
write immediately.

## Context propagation

`Context` implements `context.Context` (`Deadline`, `Done`, `Err`), so it can be
passed to anything taking one. `Ctx()` returns the underlying
`context.Context`, and `WithContext(ctx)` returns a copy carrying a new one —
use it to attach deadlines or trace spans.

## Auth

`Auth() Authenticator` returns the authenticated principal for the request;
`SetAuth(a)` is what an auth middleware calls. See [auth](auth.md).

## Static file helpers

Package-level, not methods:

| Function | Purpose |
| --- | --- |
| `ServeStatic(dir) http.Handler` | Serve a directory |
| `ServeStaticFile(path) http.HandlerFunc` | Serve one file |
| `SPAHandler(dir) http.HandlerFunc` | Serve a SPA, falling back to `index.html` |
