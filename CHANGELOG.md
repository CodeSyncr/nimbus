# Changelog

All notable changes to Nimbus are documented in this file.

This project follows Semantic Versioning.

## [1.1.0] - 2026-05-21

### Added
- **Supabase Plugin:** Added first-class integration with Supabase services (`plugins/supabase`), including:
  - Auth client (`GoTrue`) for signing up, signing in, and managing user sessions.
  - Database client (`PostgREST`) for calling database RPC functions.
  - Realtime client for subscribing to channels and listening for database change events.
  - Verification middleware (`VerifySupabaseJWT`) to authenticate incoming API requests.
- **Template Engine:** Added support for Nested Components (dot notation subdirectory mapping, e.g. `@field.root(...)`).
- **Template Engine:** Finalized the Props and Provide/Inject Context APIs in the `.nimbus` rendering engine. Added lazy slot rendering to resolve parent-child rendering evaluation order.

### Fixed
- **Template Engine:** Changed the return type of `$props.toAttrs()` to `template.HTMLAttr`, bypassing Go's default context-aware auto-escaping and resolving `ZgotmplZ` errors.

## [1.0.1] - 2026-05-08

### Fixed
- **Security:** Resolved SQL injection vulnerability in Tenancy schema scoping.
- **Security:** Renamed `EncryptDeterministic` to `EncryptDeterministicUNSAFE` to highlight cryptographic risks.
- **Security:** Fixed WebSocket origin checker incorrectly rejecting all connections by default.
- **Concurrency:** Fixed data races and TOCTOU bugs in `logger` channels, `ai` provider registry, `presence` channels, and `cache` locks.
- **Middleware:** Implemented full logic for `RequireVerifiedEmail` and fixed HTTP spec violations in `ratelimit_redis` (correctly handles `Retry-After`).
- **Optimization:** Moved regex compilation in `shield` out of the hot path.

## [1.0.0] - 2026-03-23

First **stable** release (`v1.0.0`). The packages listed under **Versioning & stability** in `README.md` follow SemVer: breaking changes require a new major version after deprecation when possible.

### Added
- **CLI:** `nimbus plugin install` and `nimbus plugin list` as nested commands (same behavior as `plugin:install` / `plugin:list`).
- **Tests:** coverage for `router` (named URLs, groups, route metadata), `http` context helpers, `session` middleware, `database` migrator (`Fresh` on SQLite, `dropTableSQL`).

### Changed
- **`database.Migrator.Fresh`:** dialect-safe `DROP TABLE` (PostgreSQL uses `CASCADE`; SQLite/MySQL no longer use invalid `CASCADE` on SQLite).

### Previously unreleased (rolled into 1.0.0)

#### Added
- Queue reliability hardening:
  - retry backoff with jitter
  - Redis in-flight processing + visibility timeout reclaim
  - database queue lease reclaim and completion support
- Realtime security hardening:
  - websocket and presence origin allowlist support with safe same-origin default
- Queue telemetry counters:
  - retried and reclaimed signals in Horizon stats
  - Prometheus-style Horizon metrics endpoint
- Migration safety improvements:
  - transactional migration execution on supported dialects
  - per-migration `NonTransactional` override
- CI baseline workflow with:
  - `go test ./...`
  - `go test -race ./...`
  - `go vet ./...`

#### Changed
- Public docs expanded:
  - getting started path
  - production readiness checklist
  - versioning/release policy
  - release checklist (`V1_RELEASE.md`)

#### Fixed
- `/docs/getting-started` docs page registration and routing.
- `App.Run` / `RunTLS`: always cancel scheduler context on exit (including `Serve` errors) to satisfy `go vet` and avoid leaks.

### Known limitations (v1)

- **Telescope** plugin: many panels remain preview / “coming soon”; not treated as a v1-stable surface—see `README.md`.
- **First-party OAuth / API tokens** (Sanctum/Passport-class): not included in v1; document your own token strategy or wait for a future release.
- **HTML error pages** (404/500): applications should register `router.Fallback` and custom handlers; core focuses on structured JSON/API errors.
- **Locale:** v1 supports programmatic `locale.AddTranslations` / middleware; file-based translation loading is not the primary focus.

---

## Earlier history

Prior development was not consistently tagged in this changelog; see git history for detail.
