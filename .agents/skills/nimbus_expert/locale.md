# Localisation

Translation catalogues keyed by locale, with a request-scoped current locale.

## Loading catalogues

```go
locale.LoadDirectory("resources/lang")        // every JSON file in the dir
locale.LoadJSONFile("fr", "resources/lang/fr.json")
locale.LoadJSONBytes("de", embeddedDE)
locale.AddTranslations("es", map[string]string{"welcome": "Bienvenido"})
```

Nested JSON is flattened to dotted keys, so:

```json
{ "auth": { "failed": "These credentials do not match." } }
```

is reachable as `auth.failed`.

`locale.SetDefault("en")` sets the fallback. `locale.BootFromEnv()` is called
during app construction and reads the configured locale from the environment.

## Translating

```go
locale.T("welcome")                       // current default locale
locale.T("greeting", user.Name)           // printf-style arguments
locale.TLocale("fr", "welcome")           // explicit locale
locale.TCtx(ctx, "welcome")               // locale from the request context
```

`TCtx` is the one to use inside handlers — it respects the request's locale.

A missing key returns the key itself rather than an error, so a missing
translation degrades to something readable instead of a blank page.

## Per-request locale

```go
app.Router.Use(locale.Middleware())
```

The middleware resolves the request locale (`Accept-Language`, and whatever
else your app sets) and stores it on the context.

| Function | Purpose |
| --- | --- |
| `WithLocale(ctx, loc) context.Context` | Attach a locale to a context |
| `FromContext(ctx) string` | Read the current locale |

## In templates

The view engine's default function map exposes translation to `.nimbus`
templates — see [views_templates](views_templates.md).

## Guidance

1. Key by meaning (`auth.failed`), not by English text.
2. Load catalogues once at boot, not per request.
3. Always use `TCtx` in request paths; `T` silently uses the default locale and
   will look correct in development and wrong in production.
