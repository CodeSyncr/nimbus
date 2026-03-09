# Type-Safe Config

Nimbus provides a type-safe configuration system that loads from `.env`.

## Type-Safe Get

```go
config.LoadAuto()  // loads .env and populates store

name := config.Get[string]("app.name")
port := config.Get[int]("server.port")
debug := config.Get[bool]("app.debug")

// With default
name := config.GetOrDefault[string]("app.name", "nimbus")

// Panic if missing
port := config.Must[int]("server.port")
```

## Schema-Based (Struct)

```go
type AppConfig struct {
    Port int    `config:"app.port" env:"PORT" default:"3333"`
    Env  string `config:"app.env" env:"APP_ENV" default:"development"`
    Name string `config:"app.name" env:"APP_NAME" default:"nimbus"`
}

config.LoadAuto()
var cfg AppConfig
config.LoadInto(&cfg)
```

Struct tags:
- `config:"key"` — dot-notation key (e.g. `app.name`)
- `env:"VAR"` — env var (e.g. `APP_NAME`)
- `default:"value"` — fallback when missing

## Load

```go
config.LoadAuto()           // .env from current dir
config.LoadFromEnv(".env")  // explicit path
```

## Custom Env Mapping

```go
config.AddEnvMapping("CUSTOM_VAR", "custom.key")
config.LoadFromEnv()
```

## Migrating Existing Apps

1. Replace `godotenv.Load()` with `nimbusconfig.LoadAuto()` in your `config.Load()`.
2. Use `cfg(key, fallback)` or `config.GetOrDefault[T](key, default)` instead of `env()` for app/database keys.
3. Add env mappings if needed: `nimbusconfig.AddEnvMapping("VAR", "section.key")` before `LoadAuto()`.
