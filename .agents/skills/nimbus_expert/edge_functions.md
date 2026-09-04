# Edge Functions

A lightweight request-interception runtime that runs **before** the main router:
geo routing, A/B tests, maintenance pages, edge caching. Handlers are
`func(req *Request) *Response`.

## Setting up

```go
rt := edge.New(edge.Config{ ... })

rt.Handle("/api/*", myEdgeHandler).
    Methods("GET", "HEAD").
    WithCache(60*time.Second)

app.Router.Use(rt.Middleware())
// or
app.RegisterPlugin(rt.Plugin())
```

## Request

| Member | Purpose |
| --- | --- |
| `Header(key) string` | Canonicalised header lookup |
| `QueryParam(key) string` | Query value |
| `ParseJSON(v any) error` | Decode the body |
| `Context() context.Context` | Request context |
| `Body []byte`, `Headers`, `Query` | Raw access |
| `GeoInfo` | Geographic data for the caller |

## Response

Constructors, all returning `*Response`:

| Function | Meaning |
| --- | --- |
| `Next()` | Pass through to the main application |
| `Rewrite(url)` | Pass through, but to a different path |
| `Respond(status, body)` | Terminate with a plain body |
| `JSON(status, data)` | Terminate with JSON |
| `HTML(status, html)` | Terminate with HTML |
| `Redirect(url, status)` | Terminate with a redirect |

`SetHeader(key, value) *Response` chains. `IsNext()` reports whether the
response passes through. `edge.Cached(resp, ttl)` marks a response cacheable.

**`Next()` is the default you want.** An edge handler that forgets to return
`Next()` silently swallows the request — the main router never sees it.

## Built-in handlers

| Handler | Purpose |
| --- | --- |
| `GeoRouter(routes map[string]string, fallback string)` | Route by country/region |
| `ABTest(variants []ABVariant)` | Split traffic across variants |
| `RateLimit(maxRequests, window)` | Reject before the app does any work |
| `SecurityHeaders()` | Security headers at the edge |
| `Maintenance(html string, allowedIPs ...string)` | Maintenance page with an IP allow-list |
| `BasicAuth(realm, credentials)` | HTTP basic auth gate |

```go
rt.Handle("/*", edge.Maintenance(maintenanceHTML, "203.0.113.10"))
```

## Caching

`edge.NewCache(maxSize)` is an in-process cache with `Get`, `Set(key, value, ttl)`,
and `Delete`. `WithCache(ttl, keyFn...)` on a route builder wires it up; supply
`keyFn` when the default key (the path) is not specific enough — for example
when a response varies by `Accept-Language`.

The cache is **per process**. With several instances, each warms its own; do not
treat it as a shared CDN layer.

## Observability

`rt.Metrics() map[string]any` returns hit/miss and handler counters.

## Guidance

1. Edge handlers run on every matching request before routing — keep them
   allocation-light and never touch the database from one.
2. `FallbackMode` in `Config` decides what happens when a handler errors; choose
   deliberately, since failing closed at the edge takes the whole site down.
3. Do not put business logic here. Anything that needs your models belongs in a
   controller.
