# Changelog

All notable changes to Nimbus are documented in this file.

This project follows Semantic Versioning.

## [1.2.0] - 2026-07-15

### Added
- **Cashier Plugin (`plugins/cashier`):** Multi-gateway billing, modeled on Laravel Cashier but gateway-agnostic.
  - Gateways: **Stripe**, **Razorpay**, and **PayU**, each with real webhook signature verification (Stripe/Razorpay HMAC-SHA256, PayU SHA-512).
  - `GatewayManager` for registering several gateways in one app, selecting a default (`Config.Default` → `PAYMENTS_DEFAULT_GATEWAY` → first registered), and routing a charge per request.
  - Paywall with `RequirePlan` middleware (HTTP 402), pluggable `EntitlementStore`, and canonical cross-gateway events via `events.Normalize`.
  - `FromEnv` auto-registers any gateway whose credentials are present. Ships `cashier_transactions` / `cashier_subscriptions` migrations.
- **Passport Plugin (`plugins/passport`):** OAuth2 authorization server, modeled on Laravel Passport.
  - Grants: `authorization_code` (with **PKCE**, S256/plain), `client_credentials`, and `refresh_token` (with rotation — the old token pair is revoked).
  - RFC 7662 token introspection and RFC 7009 revocation. Opaque tokens stored SHA-256 hashed, so they are fully revocable.
  - Confidential and public clients (PKCE enforced for public clients by default, per OAuth 2.1), per-client redirect/scope allowlists, first-party consent skip, and a bundled consent screen.
  - Resource-server middleware: `RequireAccessToken` (401 + `WWW-Authenticate`) and `RequireScope` (403).
- **Admin Plugin (`plugins/admin`):** Resource-based CRUD admin panel, modeled on Nova/Filament.
  - Reflection-driven over any `database.Model` — declares no migrations of its own.
  - Field constructors (`Text`, `Textarea`, `Number`, `Boolean`, `Email`, `Password`, `Date`, `Select`) with chainable modifiers (`WithLabel`, `AsSortable`, `AsReadonly`, `HideFromIndex`, `HideFromForm`).
  - Zero-config field inference from struct types when `Fields` is omitted; paginated list, create/edit/delete screens; gated by `Config.Middleware`.
- **Browser Testing (`testing/browser`):** Dusk-style end-to-end harness driving the app in-process through its `http.Handler`.
  - Cookie jar, redirect following, link clicking, and form submission that automatically carries hidden inputs (e.g. CSRF).
  - Fluent assertions: `AssertSee`, `AssertDontSee`, `AssertSeeIn`, `AssertPathIs`, `AssertQueryStringHas`, `AssertTitle`, `AssertInputValue`, `AssertStatus`/`AssertOk`, `AssertHeader`.
  - Pure stdlib — no browser binary or new dependencies required.
- **Auth:** Token ability (scope) checks matching Laravel Sanctum's `ability:`/`abilities:` semantics — `HasAnyAbility` / `HasAllAbilities` on `PersonalAccessToken`, plus `RequireAnyAbility` (OR) and `RequireAllAbilities` (AND) middleware.
- **CLI:** `nimbus key:generate` for generating `APP_KEY`, wired into `nimbus new` scaffolding.
- **AI Plugin:** Implemented the **Anthropic**, **Mistral**, and **Cohere** providers.
- **Config:** Startup configuration validation (`Config.Validate`) with actionable errors, and configurable HTTP server timeouts.
- **Router:** `router.Manifest` / `router.WriteManifest` expose registered routes for code generation, plus `router.PathParams` and `router.DeriveName`.

### Fixed
- **`nimbus gen:client` produced an empty registry for every project.** Three compounding bugs:
  - `WriteRouteManifest` was never called by the framework, so the documented `NIMBUS_DUMP_ROUTES=1` flow wrote nothing and generation always fell back to an empty skeleton. The app now writes the manifest at startup when `NIMBUS_DUMP_ROUTES=1` (honoring `NIMBUS_CLIENT_OUT`) and exits without serving.
  - Routes registered without an explicit `.As(...)` name were silently skipped. Unnamed routes now get a stable, REST-conventional derived name (`GET /api/posts/{id}` → `api.posts.show`), guaranteed unique; explicit `.As(...)` still wins.
  - Only `:id` path params were recognized. Both `:id` and `{id}` syntaxes are now supported.
  - Generation no longer writes an empty file silently — it fails with instructions when no manifest exists.
- **Errors:** Default HTML error pages, and zero-config 404s now render correctly.
- **Middleware:** Structured request logging.

### Hive TypeScript client (`@codesyncr/hive`)
- **Added:** Automatic retries — configurable `limit`, `methods` (idempotent-only by default), `statusCodes`, exponential backoff with jitter capped at `backoffLimit`, `Retry-After` header support, and an `onRetry` hook. Previously `retry` existed in the config type but was **never implemented**.
- **Added:** Per-request `timeout`, `signal`, and `retry` overrides.
- **Fixed:** A caller-supplied `AbortSignal` was overwritten by the internal timeout signal; the two are now combined.
- **Fixed:** Array query values serialized as `?tag=a%2Cb` instead of repeated `?tag=a&tag=b` params.
- **Fixed:** Path params now support both `{id}` and `:id`, are URL-encoded, and `:id` no longer matches inside `:idx`.

### Chore
- Added a `.gitignore` and removed accidentally committed artifacts from tracking, including a 28 MB compiled test binary (`livewire.test`) that had shipped in every release since v0.1.8, and stray `.DS_Store` files.

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
