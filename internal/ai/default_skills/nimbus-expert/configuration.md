# Configuration - Nimbus

Nimbus uses a **layered configuration system** that combines environment variables (`.env`), type-safe config structs, and a central loader.

## Core Principles

-   **Layered Loading**: `.env` file → `config.LoadAuto()` → typed structs.
-   **Type Safety**: Configuration is accessed via typed Go structs (e.g., `config.App.Port`), avoiding "stringly-typed" access.
-   **Centralized**: All config initialization should happen in `config/config.go`.

## Loading Flow

1.  **`.env` file** is read and populates environment variables.
2.  **`config.LoadAuto()`** merges environment variables into a central key store using dot-notation (e.g., `app.port`).
3.  **Individual loaders** (e.g., `loadApp()`, `loadDatabase()`) read from the store and populate global config structs.
4.  **Application code** accesses these global structs.

## Config File Reference

### `config/app.go`
Controls core settings like `Name`, `Env` (development/production), `Port`, and `Key` (encryption).

### `config/database.go`
Supports PostgreSQL, MySQL, and SQLite. DSNs are automatically constructed based on driver-specific fields.

### `config/auth.go`
Configures authentication guards (session, token) and their respective settings (cookie names, lifetimes).

### `config/cache.go`
Sets the cache driver (`memory`, `redis`, `memcached`, `dynamodb`) and default TTL.

### `config/queue.go`
Configures background job drivers (`sync`, `redis`, `sqs`, `kafka`).

## Helper Functions

Nimbus provides helpers for reading environment variables with fallbacks:
-   `env(key, fallback)`: Read string.
-   `envInt(key, fallback)`: Read integer.
-   `envBool(key, fallback)`: Read boolean.
-   `cfg(key, fallback)`: Read from the central config store using dot-notation.

## Best Practices

1.  **Secrets in `.env`**: Never hardcode sensitive information; use environment variables.
2.  **Typed Structs**: Prefer accessing config via global structs rather than raw `os.Getenv` calls.
3.  **Sensible Defaults**: Ensure your loaders provide workable defaults for local development.
