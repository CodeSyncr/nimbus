# Unpoly Plugin for Nimbus

Unpoly integrates server-driven partial page updates with Nimbus.
The plugin adds middleware helpers for Unpoly request/response headers.

## Install

```go
import "github.com/CodeSyncr/nimbus/plugins/unpoly"

app.Use(unpoly.New())
```

## What it does

- Adds protocol headers for Unpoly requests
- Exposes helpers to inspect request context and set response behaviors
- Keeps server-rendered templates as the primary rendering model

## Default config

From `DefaultConfig()`:

- `enabled`: `true`
- `cdn`: `https://unpoly.com`
- `version`: `3.12.0`

## Typical usage

```go
app.Router.Get("/posts/:id", func(c *http.Context) error {
    if unpoly.IsFragmentRequest(c) {
        return c.View("posts/_show", data)
    }
    return c.View("posts/show", data)
})
```
