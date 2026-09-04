# OpenAPI

Generates an OpenAPI 3.0 specification from the registered route table, and
serves it alongside a documentation UI.

## Generating

```go
gen  := openapi.NewGenerator(openapi.GeneratorConfig{ ... })
spec := gen.Generate(app.Router.Routes())
data, err := gen.JSON(app.Router.Routes())
```

`Generate` returns a `*Spec` you can post-process; `JSON` marshals it directly.

The full model is exposed as Go types, so anything the generator does not infer
can be filled in by hand: `Spec`, `Info`, `Contact`, `License`, `Server`, `Tag`,
`PathItem`, `Operation`, `Parameter`, `RequestBody`, `Response`, `MediaType`,
`Schema`, `Components`, `SecurityScheme`, `OAuthFlow`.

## From the app

```go
app.DumpOpenAPI("openapi.json")   // warms up first, then writes
```

Defaults to `openapi.json`, titled from `Config.App.Name`.

## As a plugin

```go
app.RegisterPlugin(openapi.NewPlugin(openapi.PluginConfig{ ... }))
```

The plugin mounts the spec and its UI via `RegisterRoutes`. `InvalidateCache()`
forces regeneration — call it if you add routes at runtime, otherwise the cached
spec goes stale.

## What is inferred, and what is not

The generator reads the route table: paths, methods, and route parameters. It
cannot see request or response *shapes* unless you describe them, so:

1. Fill in `RequestBody` and `Response` schemas for anything a consumer will
   generate a client from.
2. Declare `SecurityScheme` once in `Components` and reference it, rather than
   repeating auth on every operation.
3. Tag operations — an untagged spec renders as one flat list.

Related: [hive](hive.md) generates a typed TypeScript client from the same route
manifest, which is usually a better fit than an OpenAPI-generated client for a
first-party frontend.
