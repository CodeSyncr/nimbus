# Nimbus Framework — Comprehensive Audit Report

> **Module:** `github.com/CodeSyncr/nimbus`  
> **Language:** Go  
> **Philosophy:** AdonisJS / Laravel-inspired full-stack Go web framework  
> **Foundation:** Chi router · GORM ORM · Zap logger · Cobra CLI · gorilla/websocket

---

## Table of Contents

1. [Root Package (`nimbus`)](#1-root-package-nimbus)
2. [Auth (`auth/`)](#2-auth-auth)
3. [Auth Socialite (`auth/socialite/`)](#3-auth-socialite-authsocialite)
4. [Cache (`cache/`)](#4-cache-cache)
5. [CLI (`cli/`)](#5-cli-cli)
6. [Config (`config/`)](#6-config-config)
7. [Container (`container/`)](#7-container-container)
8. [Database (`database/`)](#8-database-database)
9. [Edge (`edge/`)](#9-edge-edge)
10. [Errors (`errors/`)](#10-errors-errors)
11. [Events (`events/`)](#11-events-events)
12. [Flags (`flags/`)](#12-flags-flags)
13. [Hash (`hash/`)](#13-hash-hash)
14. [Health (`health/`)](#14-health-health)
15. [HTTP (`http/`)](#15-http-http)
16. [Locale (`locale/`)](#16-locale-locale)
17. [Logger (`logger/`)](#17-logger-logger)
18. [Mail (`mail/`)](#18-mail-mail)
19. [Metrics (`metrics/`)](#19-metrics-metrics)
20. [Middleware (`middleware/`)](#20-middleware-middleware)
21. [Notification (`notification/`)](#21-notification-notification)
22. [OpenAPI (`openapi/`)](#22-openapi-openapi)
23. [Presence (`presence/`)](#23-presence-presence)
24. [Queue (`queue/`)](#24-queue-queue)
25. [Resource (`resource/`)](#25-resource-resource)
26. [Router (`router/`)](#26-router-router)
27. [Schedule (`schedule/`)](#27-schedule-schedule)
28. [Scheduler (`scheduler/`)](#28-scheduler-scheduler)
29. [Search (`search/`)](#29-search-search)
30. [Session (`session/`)](#30-session-session)
31. [Shield (`shield/`)](#31-shield-shield)
32. [Storage (`storage/`)](#32-storage-storage)
33. [Studio (`studio/`)](#33-studio-studio)
34. [Tenancy (`tenancy/`)](#34-tenancy-tenancy)
35. [Testing (`testing/`)](#35-testing-testing)
36. [Validation (`validation/`)](#36-validation-validation)
37. [View (`view/`)](#37-view-view)
38. [WebSocket (`websocket/`)](#38-websocket-websocket)
39. [Workflow (`workflow/`)](#39-workflow-workflow)
40. [Plugins (`plugins/`)](#40-plugins-plugins)
41. [CLI Commands](#41-cli-commands)

---

## 1. Root Package (`nimbus`)

**Purpose:** Application kernel — bootstraps the framework, manages plugin/provider lifecycle, runs the HTTP server.

### Exported Types

| Type | Description |
|------|-------------|
| `App` | Core application struct. Holds Config, Router, Server, Container, Events, Scheduler, Health, plugins, middleware, and lifecycle hooks. |
| `Plugin` | Interface — `Name()`, `Version()`, `Register(app)`, `Boot(app)` |
| `BasePlugin` | Embeddable base implementation of Plugin |
| `Provider` | Interface — `Register(app)`, `Boot(app)` |
| `DB` | Type alias for `*gorm.DB` — framework-level database handle |

### Capability Interfaces (for Plugins)

| Interface | Method |
|-----------|--------|
| `HasRoutes` | `RegisterRoutes(r *router.Router)` |
| `HasMiddleware` | `Middleware() []func(http.Handler) http.Handler` |
| `HasConfig` | `DefaultConfig() map[string]any` |
| `HasMigrations` | `Migrations() []database.Migration` |
| `HasViews` | `ViewsFS() fs.FS` |
| `HasShutdown` | `Shutdown() error` |
| `HasBindings` | `Bindings(c *container.Container)` |
| `HasCommands` | `Commands() []*cobra.Command` |
| `HasSchedule` | `Schedule(s *schedule.Scheduler)` |
| `HasEvents` | `Events() map[string][]events.Listener` |
| `HasHealthChecks` | `HealthChecks() []health.Check` |

### Exported Functions/Methods

| Function | Signature |
|----------|-----------|
| `New` | `New(cfgs ...config.AppConfig) *App` |
| `App.Register` | `Register(providers ...Provider)` |
| `App.Use` | `Use(plugins ...Plugin)` |
| `App.Plugin` | `Plugin(name string) Plugin` |
| `App.Boot` | `Boot() error` |
| `App.Run` | `Run() error` |
| `App.RunTLS` | `RunTLS(certFile, keyFile string) error` |
| `App.Shutdown` | `Shutdown() error` |
| `App.OnBoot` | `OnBoot(fn func(*App))` |
| `App.OnStart` | `OnStart(fn func(*App))` |
| `App.OnShutdown` | `OnShutdown(fn func(*App))` |
| `App.NamedMiddleware` | `NamedMiddleware() map[string]router.Middleware` |
| `App.PluginConfig` | `PluginConfig(name string) map[string]any` |
| `SetDB` | `SetDB(conn *DB)` |
| `GetDB` | `GetDB() *DB` |
| `Transaction` | `Transaction(fn func(tx *DB) error) error` |
| `Begin` | `Begin() *DB` |

---

## 2. Auth (`auth/`)

**Purpose:** Authentication guards (session, token, basic auth), authorization policies, and middleware.

### Exported Types

| Type | Description |
|------|-------------|
| `User` | Interface — `GetID() uint` |
| `UserLoader` | Interface — `LoadUser(id uint) (User, error)` |
| `Guard` | Interface — `User() User`, `Login(user User)`, `Logout()` |
| `SessionGuard` | Session-based authentication guard |
| `TokenGuard` | API token authentication guard (personal access tokens) |
| `BasicAuthGuard` | HTTP Basic Auth guard |
| `PersonalAccessToken` | GORM model for API tokens (with abilities/scopes) |
| `NewAccessToken` | Struct returned after creating a token (contains plaintext) |
| `Policy` | Interface — `Before(user, action)` for authorization |
| `ResourcePolicy` | Interface — CRUD policy for a resource |
| `BasePolicy` | Embeddable base policy |
| `Gate` | Authorization gate — define/check abilities |
| `UserGate` | Per-user gate for `Can()`/`Cannot()` checks |

### Exported Functions

| Function | Signature |
|----------|-----------|
| `NewSessionGuard` | `NewSessionGuard(session) *SessionGuard` |
| `NewSessionGuardWithLoader` | `NewSessionGuardWithLoader(session, loader) *SessionGuard` |
| `NewTokenGuard` | `NewTokenGuard(db) *TokenGuard` |
| `NewBasicAuthGuard` | `NewBasicAuthGuard(validate) *BasicAuthGuard` |
| `WithUser` | `WithUser(ctx, user) context.Context` |
| `UserFromContext` | `UserFromContext(ctx) User` |
| `RequireAuth` | `RequireAuth(guard, redirectTo) Middleware` |
| `RequireToken` | `RequireToken(guard) Middleware` |
| `RequireAbility` | `RequireAbility(guard, abilities...) Middleware` |
| `OptionalToken` | `OptionalToken(guard) Middleware` |
| `RequireBasicAuth` | `RequireBasicAuth(validate) Middleware` |
| `TokenGuard.CreateToken` | `CreateToken(user, name, abilities) (*NewAccessToken, error)` |
| `TokenGuard.RevokeToken` | `RevokeToken(tokenID) error` |
| `TokenGuard.ListTokens` | `ListTokens(user) ([]PersonalAccessToken, error)` |
| `Gate.Define` | `Define(ability, callback)` |
| `Gate.RegisterPolicy` | `RegisterPolicy(model, policy)` |
| `Gate.Allows` | `Allows(user, ability, args...)` |
| `Gate.Authorize` | `Authorize(user, ability, args...) error` |
| `Gate.ForUser` | `ForUser(user) *UserGate` |

---

## 3. Auth Socialite (`auth/socialite/`)

**Purpose:** OAuth2 social authentication (like Laravel Socialite). Built-in providers: Google, GitHub, Discord, Apple. Custom providers supported.

### Exported Types

| Type | Description |
|------|-------------|
| `Socialite` | Manager for OAuth providers |
| `SocialUser` | Authenticated user from OAuth provider (ID, Name, Email, Avatar, AccessToken, etc.) |
| `ProviderConfig` | OAuth config (ClientID, ClientSecret, RedirectURL, Scopes) |
| `Config` | Map of provider name → ProviderConfig |
| `CallbackFunc` | `func(c *Context, user *SocialUser) error` |
| `SocialitePlugin` | Nimbus plugin wrapping Socialite |

### Exported Functions

| Function | Description |
|----------|-------------|
| `New(cfg)` | Create Socialite manager |
| `NewPlugin(cfg, callback)` | Create as a Nimbus plugin |
| `RedirectHandler()` | Returns handler that redirects to OAuth provider |
| `CallbackHandler(fn)` | Returns handler that processes OAuth callback |
| `Provider(name)` | Get a specific provider |

---

## 4. Cache (`cache/`)

**Purpose:** Multi-backend caching with `Remember` pattern. Backends: Memory, Redis, Memcached, DynamoDB, Cloudflare KV.

### Exported Types

| Type | Description |
|------|-------------|
| `Store` | Interface — `Set()`, `Get()`, `Delete()`, `Remember()` |
| `MemoryStore` | In-memory cache with TTL |
| `RedisStore` | Redis-backed cache |
| `MemcachedStore` | Memcached-backed cache |
| `DynamoDBStore` | AWS DynamoDB-backed cache |
| `CloudflareStore` | Cloudflare Workers KV-backed cache |

### Exported Functions

| Function | Description |
|----------|-------------|
| `Set(key, value, ttl)` | Store a value |
| `Get(key)` | Retrieve a value |
| `Delete(key)` | Remove a value |
| `Has(key)` | Check existence |
| `Missing(key)` | Check non-existence |
| `Pull(key)` | Get and delete |
| `SetForever(key, value)` | Store without expiry |
| `Remember(key, ttl, fn)` | Get-or-compute pattern |
| `RememberT[T](key, ttl, fn)` | Generic typed remember |
| `Boot(cfg)` | Initialize cache from config/env |
| `Default` | Global default store variable |
| `NewLock(store, key, ttl)` | Create a cache lock |
| `AtomicLock(store, key, ttl, fn)` | Acquire + run + release in one call |

### Exported Types (Locks)

| Type | Description |
|------|-------------|
| `Lock` | Cache-backed mutual exclusion lock |

### Lock Methods

| Method | Description |
|--------|-------------|
| `Acquire()` | Try to acquire lock (returns bool) |
| `Release()` | Release lock (owner-only) |
| `Block(timeout, interval)` | Retry-acquire until timeout |

### Exported Errors

| Error | Description |
|-------|-------------|
| `ErrLockNotAcquired` | Lock could not be acquired |

---

## 5. CLI (`cli/`)

**Purpose:** CLI framework built on Cobra. Registers commands, generators, and provides UI/prompt utilities.

### Exported Types

| Type | Description |
|------|-------------|
| `Command` | Interface — `Name()`, `Description()`, `Run(ctx)` |
| `CommandWithFlags` | Adds `Flags(*pflag.FlagSet)` |
| `CommandWithAliases` | Adds `Aliases() []string` |
| `CommandWithArgs` | Adds `Args() cobra.PositionalArgs` |
| `Root` | CLI root wrapping `*cobra.Command` |
| `Context` | CLI execution context (Cmd, Args, AppRoot, UI) |

### Exported Functions

| Function | Description |
|----------|-------------|
| `NewRoot(name, version)` | Create CLI root |
| `RegisterCommand(cmd)` | Register a command globally |
| `Root.Execute()` | Run the CLI |
| `NewContext(cmd, args)` | Create execution context |
| `ToSnake(s)` | Convert to snake_case |
| `ToPascal(s)` | Convert to PascalCase |
| `Timestamp()` | Generate timestamp string |

### Sub-packages

- **`cli/ui/`**: Terminal prompts (`Input()`, `Confirm()`, `Select()`, `MultiSelect()`) and styling (`Bold()`, `Green()`, `Red()`, `Table()`, `Spinner()`)
- **`cli/generators/`**: Code generation engine with Go templates
- **`cli/commands/`**: All built-in CLI commands (see [§41](#41-cli-commands))

---

## 6. Config (`config/`)

**Purpose:** Typed configuration with dot-notation access, env var loading, struct tag binding.

### Exported Types

| Type | Description |
|------|-------------|
| `Config` | Central config store (map-based) |
| `AppConfig` | Top-level app config (Name, Port, Env, Debug, etc.) |
| `DatabaseConfig` | Database connection config |

### Exported Functions

| Function | Signature |
|----------|-----------|
| `Load(file)` | Load config from YAML/JSON file |
| `Current()` | Get current config instance |
| `Get[T](key)` | Generic dot-notation get: `config.Get[string]("app.name")` |
| `Must[T](key)` | Get or panic |
| `GetOrDefault[T](key, def)` | Get with fallback |
| `LoadFromEnv()` | Load config from environment variables |
| `AddEnvMapping(envKey, configKey)` | Add custom env→config mapping |
| `LoadAuto()` | Auto-detect and load from env |
| `LoadInto(dest)` | Load into struct via `config:`, `env:`, `default:` tags |
| `ValidateEnv(rules...)` | Validate env vars at boot (Required, OneOf, Default) |
| `Required(keys...)` | Shorthand for requiring multiple env vars |

### Exported Types (Env Validation)

| Type | Description |
|------|-------------|
| `EnvRule` | Env validation rule (Key, Required, OneOf, Default) |

---

## 7. Container (`container/`)

**Purpose:** IoC (Inversion of Control) dependency injection container with auto-wiring.

### Exported Types

| Type | Description |
|------|-------------|
| `Constructor` | `func() interface{}` |
| `Container` | DI container with singleton/transient bindings and auto-wiring |

### Exported Functions

| Function | Description |
|----------|-------------|
| `New()` | Create a new container |
| `Bind(name, constructor)` | Register transient binding |
| `Singleton(name, constructor)` | Register singleton binding |
| `Make(name)` | Resolve a binding (auto-wires constructor params by type) |
| `MustMake(name)` | Resolve or panic |
| `Instance(name, value)` | Bind a concrete instance |
| `Has(name)` | Check if binding exists |

**Auto-wiring:** Constructor parameters are automatically resolved by matching their types against registered bindings (exact type match first, then interface satisfaction).

---

## 8. Database (`database/`)

**Purpose:** Full ORM layer built on GORM — models, migrations, queries, relations, pagination, hooks, factories, seeders, serialization, scopes, transactions, caching, events.

### Exported Types

| Type | Description |
|------|-------------|
| `Model` | Base model struct (ID, CreatedAt, UpdatedAt, DeletedAt) |
| `BaseModel` | Alias for `Model` |
| `DB` | Global `*gorm.DB` |
| `ConnectConfig` | Connection configuration |
| `Migration` | Migration definition (Name, Up, Down functions) |
| `Migrator` | Migration runner |
| `Query` | Fluent query builder |
| `Paginator` | Pagination result (Data, Meta, Links) |
| `Hooks` | Model lifecycle hooks (Before/AfterCreate, Update, Save, Delete) |
| `Factory` | Test data factory with `Faker` |
| `Faker` | Fake data generator (Sentence, Email, Word, Int, etc.) |
| `Seeder` | Interface — `Seed(db) error` |
| `SeedFunc` | Function adapter for Seeder |
| `SeedRunner` | Runs multiple seeders |
| `SerializeOptions` | Serialization options (Omit, Pick fields) |
| `Scope` | Query scope function type |
| `DatabaseNotification` | Notification model for DB channel |
| `NotificationStore` | CRUD for database notifications |
| `RelationKind` | Enum: belongsTo, hasMany, hasOne, manyToMany |
| `Relation` | Parsed relation metadata |
| `QueryPayload` | Event payload for DB query events |

### Exported Functions

| Function | Description |
|----------|-------------|
| `Connect(driver, dsn)` | Connect to database (postgres/mysql/sqlite) |
| `ConnectWithConfig(cfg)` | Connect with full config |
| `Get()` / `Debug()` | Get global DB / debug mode |
| `PrettyPrintQueries()` | Enable query logging |
| `NewMigrator(db, migrations)` | Create migration runner |
| `Migrator.RunAll()` / `Rollback()` | Run/rollback migrations |
| `From(model)` / `QueryFor[T]()` | Create query builder |
| `Query.Where/Select/OrderBy/Limit/Get/First/DB` | Fluent query chain |
| `Preload(db, relations...)` | Eager-load relations |
| `Paginate(db, dest, page, perPage)` | Paginated results |
| `RegisterHooks(db, model, hooks)` | Register model hooks |
| `Define(fn)` | Define a factory |
| `Factory.Create() / CreateMany(n) / Merge(overrides)` | Generate records |
| `NewSeedRunner(db)` | Create seeder runner |
| `Serialize(model, opts)` | Serialize model to map |
| `IsDirty(model)` | Check if model has unsaved changes |
| `WhereScope/OrderScope/LatestScope/OldestScope/LimitScope/WhenScope` | Reusable scopes |
| `WithTrashed/OnlyTrashed/Restore/ForceDelete/IsTrashed` | Soft delete helpers |
| `Chunk(db, size, fn)` | Process records in chunks |
| `Transaction(fn)` / `TransactionWithDB(db, fn)` | Database transactions |
| `CachedFind(key, model, query, ttl)` | Query with cache |
| `CreateDatabaseIfNotExists(driver, dsn, name)` | Create database |
| `AutoPreload(db, model)` | Auto-eager-load from struct tags |
| `TableNameFor(model)` | Get table name |
| `ParseRelations(model)` | Parse relation tags |
| `NotificationStore.Send/All/Unread/Read/MarkAsRead/MarkAllAsRead/Delete` | Notification CRUD |

---

## 9. Edge (`edge/`)

**Purpose:** Edge function runtime — handle requests at the edge with geo-aware routing, A/B testing, and middleware.

### Exported Types

| Type | Description |
|------|-------------|
| `Config` | Edge runtime configuration |
| `FallbackMode` | Fallback behavior enum |
| `Request` | Edge request wrapper |
| `GeoInfo` | Geolocation data (Country, City, Region, Lat, Lon, Timezone) |
| `Response` | Edge response |
| `Runtime` | Edge function runtime engine |

### Exported Functions

| Function | Description |
|----------|-------------|
| `Next()` | Pass to origin |
| `Respond(status, body)` | Direct response |
| `JSON(status, data)` | JSON response |
| `HTML(status, html)` | HTML response |
| `Redirect(url, status)` | Redirect response |
| `Runtime.Handle(fn)` | Register edge handler |
| `Runtime.Plugin()` | Get as Nimbus plugin |

---

## 10. Errors (`errors/`)

**Purpose:** Error handling middleware, error IDs for tracking, and external error reporting.

### Exported Types

| Type | Description |
|------|-------------|
| `HTTPError` | Structured HTTP error (Status, Message, Payload) |
| `AppError` | Unique-ID error (ID, Status, Message, Internal, Timestamp) |
| `Reporter` | Interface — `Report(err, context) error` |
| `LogReporter` | Built-in reporter that logs errors |
| `DevPageConfig` | Configuration for smart error pages |
| `StackFrame` | Parsed stack frame |
| `SourceLine` | Source code line with number |
| `DevError` | Enriched error with source context |
| `RequestInfo` | Request details for error page |

### Exported Functions

| Function | Description |
|----------|-------------|
| `Handler()` | Error-catching middleware (validation + HTTP + AppError + reporting) |
| `New(status, message)` | Create AppError with unique ID |
| `Wrap(status, err)` | Wrap error as AppError with unique ID |
| `RegisterReporter(r)` | Register an external error reporter |
| `ReportError(err, ctx)` | Send error to all registered reporters |
| `ClearReporters()` | Remove all registered reporters |
| `WriteHTTPError(w, status, msg)` | Write structured error response |
| `SmartErrorHandler(cfg)` | Rich HTML error pages with source code, stack traces, diagnostic hints |

---

## 11. Events (`events/`)

**Purpose:** Pub/sub event dispatcher with sync and async dispatch.

### Built-in Event Names

`ProviderRegister`, `PluginRegister`, `ProviderBoot`, `PluginBoot`, `AppBooted`, `AppStarted`, `AppShutdown`, `RouteRegistered`, `MiddlewareRegistered`, `DatabaseQuery`, `DatabaseInsert`, `DatabaseUpdate`, `DatabaseDelete`

### Exported Types

| Type | Description |
|------|-------------|
| `Event` | Event struct (Name, Data, Timestamp) |
| `Listener` | `func(event Event)` |
| `Dispatcher` | Event dispatcher |

### Exported Functions

| Function | Description |
|----------|-------------|
| `New()` | Create dispatcher |
| `Listen(event, listener)` | Register listener |
| `Dispatch(event, data)` | Synchronous dispatch |
| `DispatchAsync(event, data)` | Asynchronous dispatch |
| `Has(event)` | Check if event has listeners |
| `Clear(event)` | Remove all listeners |
| `ListenerCount(event)` | Count listeners |
| `Default` | Global default dispatcher |

---

## 12. Flags (`flags/`)

**Purpose:** Feature flag system with rollout percentages, user targeting, and environment-based config.

### Exported Types

| Type | Description |
|------|-------------|
| `Config` | Feature flag configuration |
| `Flag` | Feature flag definition (Name, Enabled, Rollout%, Variants) |
| `UserContext` | User context for targeting |
| `Store` | Interface for flag persistence |
| `Manager` | Feature flag manager |
| `FlagPlugin` | Nimbus plugin with admin routes at `/_flags` |

### Exported Functions

| Function | Description |
|----------|-------------|
| `New(cfg)` | Create flag manager |
| `Define(name, flag)` | Define a feature flag |
| `Group(name, flags)` | Group flags |
| `Active(name, user)` | Check if flag is active for user |
| `Variant(name, user)` | Get active variant |
| `Enable/Disable(name)` | Toggle flag |
| `SetRollout(name, pct)` | Set rollout percentage |
| `All()` | List all flags |
| `LoadFromEnv()` | Load flags from environment |
| `FlagGate(name)` | Middleware — gate route by flag |
| `RequireFlag(name)` | Middleware — require flag active |

---

## 13. Hash (`hash/`)

**Purpose:** Password hashing with bcrypt.

### Exported Functions

| Function | Description |
|----------|-------------|
| `Make(password)` | Hash password (default cost 10) |
| `MakeWithCost(password, cost)` | Hash with custom cost |
| `Check(plaintext, hash)` | Verify password |

---

## 14. Health (`health/`)

**Purpose:** Health check endpoint with pluggable checks.

### Exported Types

| Type | Description |
|------|-------------|
| `Check` | Health check function |
| `Result` | Check result (Name, Healthy, Message, Duration) |
| `Checker` | Health checker with registered checks |

### Exported Functions

| Function | Description |
|----------|-------------|
| `New()` | Create checker |
| `Add(name, fn)` | Add custom check |
| `DB(db)` | Add database check |
| `Redis(rdb)` | Add Redis check |
| `Run(ctx)` | Run all checks |
| `Handler()` | HTTP handler for health endpoint |

---

## 15. HTTP (`http/`)

**Purpose:** HTTP context with request/response helpers, plus re-exports of `net/http` types.

### Exported Types

| Type | Description |
|------|-------------|
| `Context` | Request context (Request, Response, Params, key-value store) |
| Re-exports | `Handler`, `StdHandlerFunc`, `Request`, `ResponseWriter`, `Cookie`, `Server`, `Client` |

### Context Methods

`New()`, `Set(key, val)`, `Get(key)`, `MustGet(key)`, `QueryInt(key, default)`, `Param(name)`, `Status(code)`, `JSON(status, data)`, `String(status, text)`, `Redirect(url, status)`, `View(template, data)`

### Constants

All standard HTTP methods (`MethodGet`, `MethodPost`, etc.) and status codes (`StatusOK`, `StatusNotFound`, etc.)

---

## 16. Locale (`locale/`)

**Purpose:** i18n / internationalization system.

### Exported Functions

| Function | Description |
|----------|-------------|
| `SetDefault(lang)` | Set default locale |
| `AddTranslations(lang, translations)` | Register translations |
| `T(key, args...)` | Translate using default locale |
| `TLocale(lang, key, args...)` | Translate in specific locale |
| `WithLocale(ctx, lang)` | Attach locale to context |
| `FromContext(ctx)` | Get locale from context |
| `Middleware()` | Auto-detect locale from Accept-Language header |

---

## 17. Logger (`logger/`)

**Purpose:** Structured logging via Zap with request-scoped loggers and log file rotation.

### Exported Variables/Functions

| Export | Description |
|--------|-------------|
| `Log` | Global `*zap.SugaredLogger` |
| `Set(logger)` | Replace global logger |
| `Debug/Info/Warn/Error/Fatal(msg, args...)` | Log at level |
| `With(key, val...)` | Create child logger with fields |
| `ForRequest(c)` | Request-scoped logger with request_id |
| `WithContext(c, l)` | Attach scoped logger to context |

### Exported Types

| Type | Description |
|------|-------------|
| `RotatingWriter` | `io.Writer` with auto-rotation by file size |
| `RotationConfig` | Config: Path, MaxSizeMB (100), MaxBackups (5) |

### Exported Functions (Rotation)

| Function | Description |
|----------|-------------|
| `NewRotatingWriter(cfg)` | Create rotating file writer |

---

## 18. Mail (`mail/`)

**Purpose:** Multi-driver email sending (SMTP and native HTTP API drivers).

### Exported Types

| Type | Description |
|------|-------------|
| `Message` | Email message (From, To, CC, BCC, Subject, Body, HTML, Attachments) |
| `Driver` | Interface — `Send(msg) error` |
| `SMTPDriver` | Generic SMTP email driver |
| `SESDriver` | AWS SES SMTP driver |
| `MailgunSMTPDriver` | Mailgun SMTP driver |
| `SendGridSMTPDriver` | SendGrid SMTP driver |
| `PostmarkDriver` | Postmark SMTP driver |
| `SendGridDriver` | SendGrid v3 native HTTP API driver |
| `MailgunAPIDriver` | Mailgun native HTTP API driver |
| `SESAPIDriver` | AWS SES native HTTP API driver |
| `ResendDriver` | Resend native HTTP API driver |

### Exported Functions

| Function | Description |
|----------|-------------|
| `NewSMTPDriver(addr, auth, from)` | Create generic SMTP driver |
| `NewSESDriver(addr, auth, from)` | Create SES SMTP driver |
| `NewMailgunSMTPDriver(addr, auth, from)` | Create Mailgun SMTP driver |
| `NewSendGridSMTPDriver(addr, auth, from)` | Create SendGrid SMTP driver |
| `NewPostmarkDriver(addr, auth, from)` | Create Postmark SMTP driver |
| `NewSendGridDriver(apiKey, from)` | Create SendGrid v3 API driver |
| `NewMailgunAPIDriver(domain, apiKey, from)` | Create Mailgun API driver |
| `NewSESAPIDriver(region, accessKey, secretKey, from)` | Create SES API driver |
| `NewResendDriver(apiKey, from)` | Create Resend API driver |
| `Send(msg)` | Send via default driver |
| `Default` | Global default driver |

---

## 19. Metrics (`metrics/`)

**Purpose:** Prometheus-compatible metrics system with runtime stats.

### Exported Types

| Type | Description |
|------|-------------|
| `RuntimeStats` | Goroutines, GC stats, Heap stats |
| `Labels` | `map[string]string` for metric labels |
| `Counter` | Monotonically increasing counter with labels |
| `Gauge` | Value that can go up or down with labels |
| `Histogram` | Distribution tracker with configurable buckets |
| `Registry` | Metric registry with Prometheus text export |

### Exported Functions

| Function | Description |
|----------|-------------|
| `ReadRuntimeStats()` | Collect current runtime metrics |
| `NewCounter(name, help)` | Create a counter metric |
| `NewGauge(name, help)` | Create a gauge metric |
| `NewHistogram(name, help, buckets)` | Create a histogram metric |
| `Handler()` | HTTP handler serving /metrics in Prometheus format |
| `RegistryHandler(r)` | HTTP handler for a custom registry |

### Exported Variables

| Variable | Description |
|----------|-------------|
| `DefaultRegistry` | Global metric registry |
| `DefaultBuckets` | Default histogram buckets |

---

## 20. Middleware (`middleware/`)

**Purpose:** Common HTTP middleware (logging, recovery, CORS, CSRF, rate limiting, security, metrics).

### Exported Functions

| Function | Description |
|----------|-------------|
| `Logger()` | Request logging middleware |
| `Recover()` | Panic recovery middleware |
| `CORS(origin)` | CORS middleware |
| `CSRF(store)` | CSRF protection middleware |
| `RateLimit(limit, window, keyFn)` | In-memory rate limiter |
| `RateLimitRedis(rdb, limit, window, keyFn)` | Redis-backed rate limiter |
| `DefaultKeyFn()` | Default rate limit key (by IP) |
| `GenerateCSRFToken()` | Generate CSRF token |
| `RequestID()` | Unique request ID (X-Request-Id header + context) |
| `Timeout(d)` | Request context deadline |
| `BodyLimit(maxBytes)` | Request body size limit (413 on exceed) |
| `Gzip()` | Response gzip compression |
| `SecureHeaders(cfg)` | HSTS, X-Frame-Options, XSS protection |
| `TrustedProxies(cidrs...)` | Strip forwarding headers from untrusted IPs |
| `Metrics()` | Prometheus HTTP metrics (count, duration, in-flight, size) |

### Exported Types

| Type | Description |
|------|-------------|
| `CSRFStore` | Interface for CSRF token storage |
| `MemoryCSRFStore` | In-memory CSRF store |
| `SecureHeadersConfig` | Configuration for SecureHeaders middleware |

---

## 21. Notification (`notification/`)

**Purpose:** Multi-channel notification dispatch (mail + broadcast).

### Exported Types

| Type | Description |
|------|-------------|
| `Notification` | Interface — `ToMail() *mail.Message`, `ToBroadcast() map[string]any` |

### Exported Functions

| Function | Description |
|----------|-------------|
| `Send(user, notification)` | Send via all channels |
| `SendMail(notification)` | Send via mail only |
| `Broadcast(notification)` | Send via broadcast only |

---

## 22. OpenAPI (`openapi/`)

**Purpose:** Full OpenAPI 3.0 spec generation from router routes with Swagger UI, Redoc, and Scalar UIs.

### Exported Types

| Type | Description |
|------|-------------|
| `Spec` | Root OpenAPI 3.0 document |
| `Info`, `Contact`, `License` | API metadata |
| `Server`, `Tag` | Server/tag definitions |
| `PathItem`, `Operation` | Path and operation specs |
| `Parameter`, `RequestBody`, `Response` | Operation details |
| `Schema` | Full JSON Schema representation |
| `Components`, `SecurityScheme` | Reusable components |
| `OAuthFlow`, `FlowConfig` | OAuth2 flow configuration |
| `GeneratorConfig` | Generator configuration |
| `PluginConfig` | Plugin configuration (Path, Enabled, CustomCSS, etc.) |
| `Plugin` | Nimbus plugin serving docs |

### Exported Functions

| Function | Description |
|----------|-------------|
| `NewGenerator(cfg)` | Create spec generator |
| `Generator.Generate(routes)` | Generate `*Spec` from routes |
| `Generator.JSON(routes)` | Generate JSON bytes |
| `NewPlugin(cfg)` | Create docs plugin |
| `Plugin.InvalidateCache()` | Clear cached spec |

### Plugin Routes

- `/_docs` — Swagger UI
- `/_docs/openapi.json` — Raw OpenAPI spec
- `/_docs/redoc` — Redoc alternative
- `/_docs/scalar` — Scalar alternative
- `/_docs/spec` — Pretty-printed JSON

---

## 23. Presence (`presence/`)

**Purpose:** Realtime presence channels — know who's online.

### Exported Types

| Type | Description |
|------|-------------|
| `User` | Presence user (ID, Name, Meta) |
| `Event` | Presence event (join, leave, update) |
| `Client` | WebSocket client in a channel |
| `Config` | Presence configuration |
| `Hub` | Presence hub managing channels |
| `Channel` | Single presence channel |

### Exported Functions

| Function | Description |
|----------|-------------|
| `NewHub(cfg)` | Create presence hub |
| `GetChannel(name)` | Get/create channel |
| `Channels()` | List all channels |
| `Broadcast(channel, event)` | Broadcast to channel |
| `BroadcastExcept(channel, event, excludeClient)` | Broadcast excluding sender |
| `UsersIn(channel)` | Get users in channel |
| `UserCount(channel)` | Count users in channel |
| `Plugin()` | Get as Nimbus plugin |

---

## 24. Queue (`queue/`)

**Purpose:** Job queue system with multiple backends. Inspired by Laravel queues.

### Exported Types

| Type | Description |
|------|-------------|
| `Job` | Interface — `Handle(ctx) error` |
| `FailedJob` | Interface — `Failed(ctx, err)` (optional cleanup) |
| `Tagger` | Interface — `Tags() []string` (for Horizon) |
| `Silenced` | Interface — `Silenced() bool` (hide from Horizon) |
| `JobFunc` | Function adapter for Job |
| `JobPayload` | Serialized job for storage (ID, JobName, Queue, Payload, Attempts, MaxRetries, Delay, RunAt, Meta) |
| `Adapter` | Interface — `Push()`, `Pop()`, `Len()` |
| `CompletableAdapter` | Adds `Complete()` (for SQS ack) |
| `Manager` | Queue manager (adapter + job registry) |
| `DispatchBuilder` | Fluent dispatch options (OnQueue, Delay, Retries, Priority) |
| `Observer` | Interface for queue lifecycle events (Horizon) |
| `Queue` | Legacy in-memory worker pool |
| `FailedJobRecord` | Failed job entry for dashboard |
| `FailedJobStore` | Interface for failed job persistence |
| `Scheduler` | Cron-based job scheduling with `cron` expressions |
| `BootConfig` | Boot configuration |

### Adapters

| Adapter | Description |
|---------|-------------|
| `SyncAdapter` | Runs jobs immediately (dev/test) |
| `RedisAdapter` | Redis lists + sorted sets for delayed jobs |
| `DatabaseAdapter` | SQL database (Postgres/MySQL/SQLite) |
| `SQSAdapter` | AWS SQS |
| `KafkaAdapter` | Apache Kafka |
| `RateLimitAdapter` | Wraps any adapter with rate limiting |

### Exported Functions

| Function | Description |
|----------|-------------|
| `Dispatch(job)` | Global dispatch — returns `DispatchBuilder` |
| `Register(job)` | Register job type globally |
| `NewManager(adapter)` | Create queue manager |
| `Manager.Dispatch(job)` | Dispatch via manager |
| `Manager.Process(ctx, queue)` | Pop and run next job |
| `Manager.Register(job)` | Register job type |
| `Boot(cfg)` | Initialize from config/env |
| `SetGlobal(m)` / `GetGlobal()` | Set/get global manager |
| `SetObserver(o)` | Set queue observer (Horizon) |
| `NewRedisAdapter(client)` | Create Redis adapter |
| `NewDatabaseAdapter(db)` | Create database adapter |
| `NewSQSAdapter(ctx, url)` | Create SQS adapter |
| `NewKafkaAdapter(cfg)` | Create Kafka adapter |
| `NewRateLimitAdapter(inner, rate, burst)` | Create rate-limited adapter |
| `NewScheduler(manager)` | Create job scheduler |
| `Scheduler.Cron(expr, job)` | Schedule via cron expression |
| `Scheduler.Every(duration, job)` | Schedule at interval |
| `NewRedisFailedStore(client)` | Create Redis failed job store |

---

## 25. Resource (`resource/`)

**Purpose:** API resource transformation (JSON serialization).

### Exported Types

| Type | Description |
|------|-------------|
| `Resource` | Interface — `ToJSON() map[string]any` |
| `ResourceFunc` | Function adapter for Resource |

### Exported Functions

| Function | Description |
|----------|-------------|
| `Collection(items, fn)` | Transform a slice of items |

---

## 26. Router (`router/`)

**Purpose:** HTTP router built on Chi with route metadata for OpenAPI generation.

### Exported Types

| Type | Description |
|------|-------------|
| `HandlerFunc` | `func(*http.Context) error` |
| `Middleware` | `func(HandlerFunc) HandlerFunc` |
| `Router` | Main router |
| `Route` | Route definition with metadata |
| `RouteMeta` | Metadata (Summary, Description, Tags, Deprecated, RequestBody, Responses, Params, Headers, Security) |
| `Group` | Route group with shared prefix/middleware |
| `ResourceController` | Interface — Index, Create, Store, Show, Edit, Update, Destroy |
| `ParamMeta` | Route parameter metadata |
| `Bindable` | Interface — `RouteKey()`, `FindForRoute(value)` for route model binding |
| `ModelBinding` | Route model binding registration (Param, Model, ContextKey) |

### Router Methods

| Method | Description |
|--------|-------------|
| `New()` | Create router |
| `Use(middleware...)` | Add middleware |
| `Group(prefix, fn)` | Create route group |
| `Get/Post/Put/Patch/Delete(path, handler)` | Register route |
| `Any(path, handler)` | Register for all methods |
| `Route(method, path, handler)` | Generic route registration |
| `Resource(path, controller)` | RESTful resource routes |
| `Mount(path, handler)` | Mount sub-router |
| `URL(name, params...)` | Generate URL by route name |
| `Routes()` | Get all registered routes |
| `ServeHTTP(w, r)` | Implement http.Handler |
| `Fallback(handler)` | Set custom 404 handler |

### Route Model Binding

| Function | Description |
|----------|-------------|
| `BindModel(bindings...)` | Middleware: auto-resolve route params to models |
| `BindModelParam(model)` | Create binding from Bindable model |
| `ParamInt(c, name)` | Parse route param as int |
| `ParamInt64(c, name)` | Parse route param as int64 |

### Route Chaining

| Method | Description |
|--------|-------------|
| `As(name)` | Name the route |
| `Describe(summary, description)` | Add description (OpenAPI) |
| `Tag(tags...)` | Add tags (OpenAPI) |
| `Body(example)` | Describe request body |
| `Returns(status, description, example)` | Describe response |
| `Secure(scheme)` | Mark as requiring auth |
| `DeprecatedRoute()` | Mark as deprecated |

### Resource Options

`ApiOnly()` — Exclude Create/Edit (HTML form routes)  
`Only(actions...)` — Whitelist actions  
`Except(actions...)` — Blacklist actions

---

## 27. Schedule (`schedule/`)

**Purpose:** Cron-style task scheduler with named tasks, panic recovery, and daily-at scheduling.

### Exported Types

| Type | Description |
|------|-------------|
| `Task` | Scheduled task definition (Name, Interval, Fn) |
| `Scheduler` | Task scheduler with non-blocking Start/Stop |

### Exported Functions

| Function | Description |
|----------|-------------|
| `New()` | Create scheduler |
| `Every(duration, name, fn)` | Run at custom interval |
| `EveryMinute(name, fn)` | Run every minute |
| `EveryFiveMinutes(name, fn)` | Run every 5 minutes |
| `Hourly(name, fn)` | Run every hour |
| `Daily(at, name, fn)` | Run daily at "HH:MM" (local timezone) |
| `Start(ctx)` | Start scheduler (non-blocking) |
| `Stop()` | Stop scheduler gracefully |
| `Count()` | Number of registered tasks |

---

## 28. Scheduler (`scheduler/`) — DEPRECATED

**Purpose:** Backward-compatible wrapper that delegates to `schedule/`. Use `schedule/` directly.

### Exported Types

| Type | Description |
|------|-------------|
| `Scheduler` | Type alias for `schedule.Scheduler` |

### Exported Functions

| Function | Description |
|----------|-------------|
| `New()` | Returns `schedule.New()` |

---

## 29. Search (`search/`)

**Purpose:** Full-text search abstraction (like Laravel Scout). Multiple backends.

### Exported Types

| Type | Description |
|------|-------------|
| `Searchable` | Interface — `SearchIndex()`, `SearchID()`, `SearchData()` |
| `Result` | Single search result (ID, Score, Data) |
| `Results` | Search results with pagination metadata |
| `Options` | Query options (Page, PerPage, Filters, Sort) |
| `Engine` | Interface — `Index()`, `Delete()`, `Search()`, `Flush()` |
| `PostgresEngine` | PostgreSQL tsvector full-text search |
| `MeilisearchEngine` | Meilisearch backend |
| `TypesenseEngine` | Typesense backend |
| `Plugin` | Nimbus plugin wrapping search engine |

### Exported Functions

| Function | Description |
|----------|-------------|
| `Register(name, engine)` | Register named engine |
| `Use(name)` | Get engine by name |
| `Default()` | Get default engine |
| `IndexRecord(ctx, record)` | Index a Searchable model |
| `DeleteRecord(ctx, record)` | Delete from index |
| `Query(ctx, index, query, opts)` | Search with default engine |
| `DefaultOptions()` | Sensible defaults |
| `NewPostgresEngine(db)` | Create Postgres engine |
| `NewMeilisearchEngine(cfg)` | Create Meilisearch engine |
| `NewTypesenseEngine(cfg)` | Create Typesense engine |
| `NewPlugin(engine)` | Create search plugin |

---

## 30. Session (`session/`)

**Purpose:** HTTP session management with multiple backends.

### Exported Types

| Type | Description |
|------|-------------|
| `Store` | Interface — `Get(id)`, `Set(id, data, ttl)`, `Destroy(id)` |
| `Session` | Session instance with Get/Set/Delete/Regenerate |
| `Config` | Session middleware configuration |
| `MemoryStore` | In-memory session store |
| `RedisStore` | Redis session store |
| `DatabaseStore` | SQL database session store |
| `CookieStoreImpl` | AES-256 encrypted cookie sessions |
| `SessionRecord` | Database model for sessions |

### Exported Functions

| Function | Description |
|----------|-------------|
| `Middleware(cfg)` | Session middleware |
| `FromContext(ctx)` | Get session from request context |
| `NewMemoryStore()` | Create memory store |
| `NewRedisStore(client)` | Create Redis store |
| `NewRedisStoreWithPrefix(client, prefix)` | Create Redis store with prefix |
| `NewDatabaseStore(db)` | Create database store |
| `NewCookieStore(key)` | Create encrypted cookie store |
| `KeyFromString(hex)` | Parse AES key from hex string |
| `EnsureTable(db)` | Auto-create sessions table |

---

## 31. Shield (`shield/`)

**Purpose:** AI-powered request protection — detects SQL injection, XSS, path traversal, command injection, prompt injection, bot abuse. Pattern-based scoring with configurable thresholds.

### Exported Types

| Type | Description |
|------|-------------|
| `Config` | Shield configuration (Level, enabled modules, thresholds, callbacks) |
| `Rule` | Custom detection rule (Name, Pattern, Targets, Score, Category) |
| `BlockEvent` | Details of a blocked/flagged request |

### Exported Functions

| Function | Description |
|----------|-------------|
| `Guard(cfg...)` | Main security middleware — inspects all requests |
| `AIContentGuard(cfg...)` | Specialized middleware for AI/LLM endpoints — deep prompt injection analysis |

### Detection Categories

- **SQL Injection** (`sqli`): Union, tautology, comments, stacked queries
- **XSS** (`xss`): Script tags, event handlers, encoding attacks
- **Path Traversal** (`traversal`): `../`, encoded variants, sensitive paths
- **Command Injection** (`cmdi`): Shell commands, backticks, `$()`
- **Prompt Injection** (`prompt_injection`): Direct injection, indirect (special tokens), jailbreak attempts, prompt extraction
- **Bot Detection** (`bot`): Known attack tools (sqlmap, nikto, Burp, etc.)
- **Header Anomalies** (`anomaly`): JNDI/Log4j, null bytes, CRLF injection
- **Rate Burst**: Per-IP burst rate limiting
- **Payload Size**: Body size limits

### Levels

- `permissive` (threshold 70)
- `balanced` (threshold 50, default)
- `strict` (threshold 30)

---

## 32. Storage (`storage/`)

**Purpose:** File storage abstraction with upload handling.

### Exported Types

| Type | Description |
|------|-------------|
| `Driver` | Interface — `Put()`, `Get()`, `Delete()`, `Exists()` |
| `LocalDriver` | Local filesystem driver |
| `UploadedFile` | Uploaded file wrapper (Name, Size, Extension, MimeType) |
| `SignedURLGenerator` | Interface for temporary signed URLs |

### Exported Functions

| Function | Description |
|----------|-------------|
| `NewLocalDriver(root)` | Create local storage |
| `PutFromRequest(r, field, path)` | Store uploaded file |
| `PutFromRequestAs(r, field, path, name)` | Store with custom name |
| `UploadedFile.Store(path)` | Store file |
| `UploadedFile.StoreAs(path, name)` | Store with name |
| `UploadedFile.StoreRandomName(path)` | Store with random name |
| `UploadedFile.IsValid()` | Validate file |
| `ServeSignedFiles(r, generator)` | Serve files via signed URLs |

---

## 33. Studio (`studio/`)

**Purpose:** Auto-generated admin panel. Introspects GORM models to build CRUD UI with filtering, sorting, pagination, forms, and relationship management.

### Exported Types

| Type | Description |
|------|-------------|
| `Config` | Admin panel config (Path, Models, DB, Title, BrandColor, Auth, ReadOnly, PerPage, CustomActions, Widgets) |
| `ModelAction` | Custom action on a model (Name, Label, Handler, Bulk, Destructive) |
| `Widget` | Dashboard widget (count, chart, list, custom) |
| `ModelMeta` | Introspected model metadata |
| `FieldMeta` | Field metadata (type, primary, required, unique, sortable, filterable, searchable, input type, relation) |
| `Plugin` | Nimbus Studio plugin |

### Exported Functions

| Function | Description |
|----------|-------------|
| `NewPlugin(cfg)` | Create admin panel plugin |

### Plugin Routes (default `/_studio`)

- `GET /_studio` — Dashboard
- `GET /_studio/api/models` — List models
- `GET /_studio/api/models/:model` — Model metadata
- `GET /_studio/api/models/:model/records` — List records (paginated, filterable)
- `GET /_studio/api/models/:model/records/:id` — Get record
- `POST /_studio/api/models/:model/records` — Create record
- `PUT /_studio/api/models/:model/records/:id` — Update record
- `DELETE /_studio/api/models/:model/records/:id` — Delete record
- `POST /_studio/api/models/:model/actions/:action` — Custom action
- `GET /_studio/api/stats` — Dashboard stats
- Model page routes for UI

---

## 34. Tenancy (`tenancy/`)

**Purpose:** Multi-tenant support with automatic tenant resolution and database isolation.

### Resolution Methods

- **Subdomain**: `tenant.example.com`
- **Header**: `X-Tenant-ID`
- **Path**: `/tenant/...`
- **Custom**: User-defined resolver function

### Isolation Strategies

| Strategy | Description |
|----------|-------------|
| `StrategyRow` | Shared DB, `tenant_id` column (auto `WHERE`) |
| `StrategySchema` | Shared DB, per-tenant PostgreSQL schema |
| `StrategyDatabase` | Separate database per tenant |

### Exported Types

| Type | Description |
|------|-------------|
| `Strategy` | Isolation strategy enum |
| `ResolveMethod` | Resolution method enum |
| `Tenant` | Tenant model (ID, Name, Domain, Schema, DBName, Metadata, Active) |
| `Config` | Tenancy configuration |
| `Manager` | Tenant resolution + DB scoping |
| `TenantStore` | Interface — FindByID, FindByDomain, All, Save, Delete |

### Exported Functions

| Function | Description |
|----------|-------------|
| `New(cfg)` | Create tenancy manager |
| `Manager.SetStore(store)` | Configure tenant persistence |
| `Manager.Register(tenant)` | Add tenant in-memory |
| `Manager.Get(id)` | Get tenant by ID |
| `Manager.Resolve(r)` | Resolve tenant from HTTP request |
| `Manager.Plugin()` | Get as Nimbus plugin |
| `Current(c)` | Get current tenant from context |
| `DB(c)` | Get tenant-scoped database from context |

---

## 35. Testing (`testing/`)

**Purpose:** Test utilities — HTTP test client, assertion helpers, fake mail/queue, database helpers.

### Exported Types

| Type | Description |
|------|-------------|
| `TestClient` | HTTP test client for router |
| `TestResponse` | Response wrapper with assertion methods |
| `FakeMailer` | Captures sent emails for assertions |
| `FakeQueue` | Captures dispatched jobs for assertions |
| `DispatchedJob` | Recorded dispatched job |

### TestClient Methods

`NewTestClient(router)`, `WithHeader(k,v)`, `WithCookie(c)`, `WithBearerToken(t)`, `Get(path)`, `Post(path, body)`, `PostJSON(path, v)`, `PostForm(path, data)`, `Put(path, body)`, `PutJSON(path, v)`, `Patch(path, body)`, `Delete(path)`, `Do(req)`

### TestResponse Assertions

`AssertStatus(t, code)`, `AssertOK(t)`, `AssertCreated(t)`, `AssertNoContent(t)`, `AssertNotFound(t)`, `AssertUnauthorized(t)`, `AssertForbidden(t)`, `AssertRedirect(t, location)`, `AssertHeader(t, key, val)`, `AssertContains(t, substring)`, `AssertJSON(t, expected)`, `JSON(t, dest)`

### Fake Helpers

| Type | Methods |
|------|---------|
| `FakeMailer` | `Send()`, `Sent()`, `SentCount()`, `SentTo(addr)`, `HasSentTo(addr)`, `Reset()` |
| `FakeQueue` | `Push()`, `PushToQueue()`, `Dispatched()`, `DispatchedCount()`, `ProcessAll(ctx)`, `Reset()` |

### Database Helpers

| Function | Description |
|----------|-------------|
| `RefreshDatabase(t, db)` | Wrap test in rolled-back transaction |
| `TruncateTables(db, tables...)` | Truncate tables for test setup |
| `SeedDatabase(db, seeder)` | Run seeder in test scope |

---

## 36. Validation (`validation/`)

**Purpose:** Request validation — struct-based and VineJS-style chainable schema validation.

### Exported Types

| Type | Description |
|------|-------------|
| `ValidationErrors` | `map[string][]string` with `Error()` and `ToMap()` |
| `FormRequest[T]` | Generic interface for typed form requests |
| `BaseFormRequest[T]` | Embeddable base implementation |
| `Schema` | `map[string]Rule` — VineJS-style schema |
| `Rule` | Interface for chainable validation rules |
| `StringRule` | String validation chain |
| `NumberRule` | Number validation chain |
| `BoolRule` | Boolean validation chain |
| `WhenRule` | Conditional rule (`When`/`WhenFn`/`Otherwise`) |
| `SchemaProvider` | Interface — `Schema() map[string]Rule` |
| `UniqueOpts` | Options for database unique rule |
| `ExistsOpts` | Options for database exists rule |

### StringRule Chain

`Required()`, `Min(n)`, `Max(n)`, `Email()`, `URL()`, `Alpha()`, `AlphaNum()`, `Trim()`, `Regex(pattern)`, `In(values...)`, `Confirmed()`, `Unique(opts)`, `Exists(opts)`

### Conditional Validation

| Function | Description |
|----------|-------------|
| `When(field, value, rule)` | Apply rule when field equals value |
| `WhenFn(predicate, rule)` | Apply rule when predicate returns true |
| `.Otherwise(rule)` | Fallback rule when condition not met |

### Exported Functions

| Function | Description |
|----------|-------------|
| `ValidateStruct(s)` | Validate tagged struct |
| `ValidateRequestJSON(r, dest)` | Parse + validate JSON request |
| `BindAndValidate[T](r)` | Generic bind + validate |
| `BindAndValidateSchema(r, schema)` | Bind + validate with schema |
| `SetDB(db)` | Set DB for unique/exists rules |

---

## 37. View (`view/`)

**Purpose:** Edge-style template engine with `.nimbus` templates.

### Template Directives

| Directive | Description |
|-----------|-------------|
| `{{ var }}` | Output (auto HTML-escaped, dot-notated) |
| `{{{ var }}}` | Raw/unescaped output |
| `{{-- comment --}}` | Template comment |
| `@layout('name')` | Extend a layout |
| `@if / @elseif / @else / @endif` | Conditionals |
| `@each(item in collection) / @endeach` | Loops |
| `@include('partial')` | Include partial |
| `@component('name') / @endcomponent` | Component system |
| `@dump(var)` | Debug dump |
| `@range(n)` | Range loop |

### Exported Types

| Type | Description |
|------|-------------|
| `Engine` | Template engine |

### Exported Functions

| Function | Description |
|----------|-------------|
| `New(root, funcs)` | Create engine with template root dir + custom funcs |
| `Engine.Render(name, data)` | Render template to string |
| `Engine.RenderWriter(w, name, data)` | Render to writer |

### Built-in Template Functions

`raw`, `dump`, `dict`, `len`, `slot`, `include`

---

## 38. WebSocket (`websocket/`)

**Purpose:** WebSocket hub for real-time messaging.

### Exported Types

| Type | Description |
|------|-------------|
| `Hub` | WebSocket hub managing connections |
| `Conn` | WebSocket connection |

### Exported Functions

| Function | Description |
|----------|-------------|
| `NewHub()` | Create hub |
| `Hub.Run()` | Start hub (goroutine) |
| `Hub.Broadcast(msg)` | Send to all connections |
| `Hub.Upgrade(w, r)` | Upgrade HTTP to WebSocket |

---

## 39. Workflow (`workflow/`)

**Purpose:** Durable, multi-step workflow orchestration with retries, timeouts, parallel branches, and human approvals (like Temporal/Inngest).

### Exported Types

| Type | Description |
|------|-------------|
| `Payload` | `map[string]any` data bag |
| `StepFunc` | `func(ctx, payload) (Payload, error)` |
| `StepStatus` | Step lifecycle enum (Pending, Running, Completed, Failed, Skipped, Waiting, Cancelled) |
| `RunStatus` | Run lifecycle enum (Pending, Running, Completed, Failed, Cancelled, Paused) |
| `StepDef` | Step definition (Name, Fn, DependsOn, IsParallel, MaxRetries, RetryDelay, Timeout, WaitEvent, Condition, OnFailure) |
| `StepBuilder` | Fluent step configuration |
| `Definition` | Workflow template (Name + Steps) |
| `Run` | Builder context for defining steps |
| `StepInstance` | Runtime step state |
| `RunInstance` | Runtime workflow state |
| `Store` | Interface — Save, Load, List, Delete |
| `Engine` | Workflow orchestration engine |
| `EngineHooks` | Observability hooks (OnStepStart/Complete/Fail, OnRunComplete/Fail) |
| `WorkflowPlugin` | Nimbus plugin with management routes |

### StepBuilder Chain

`After(deps...)`, `Parallel()`, `Retry(max, delay)`, `WithTimeout(d)`, `WaitForEvent(event, timeout)`, `When(fn)`, `ContinueOnFailure()`

### Exported Functions

| Function | Description |
|----------|-------------|
| `Define(name, builder)` | Create workflow definition |
| `Run.Step(name, fn)` | Register a step |
| `NewEngine(store)` | Create engine (nil = memory store) |
| `Engine.Register(def)` | Register workflow |
| `Engine.Dispatch(name, payload)` | Start async workflow run |
| `Engine.DispatchSync(ctx, name, payload)` | Start blocking workflow run |
| `Engine.Signal(runID, event, data)` | Send external event to waiting step |
| `Engine.Cancel(ctx, runID)` | Cancel running workflow |
| `Engine.Status(ctx, runID)` | Get run status |
| `Engine.List(ctx, workflow, limit)` | List runs |
| `Engine.Workflows()` | List registered workflow names |
| `Engine.SetHooks(hooks)` | Set lifecycle hooks |
| `NewPlugin(store)` | Create workflow plugin |

### Plugin Routes (`/_workflow`)

- `GET /_workflow/` — List workflows
- `GET /_workflow/:name/runs` — List runs
- `GET /_workflow/runs/:id` — Run status
- `POST /_workflow/:name/dispatch` — Start workflow
- `POST /_workflow/runs/:id/signal` — Send signal
- `POST /_workflow/runs/:id/cancel` — Cancel run

---

## 40. Plugins (`plugins/`)

### 40.1 AI (`plugins/ai/`)

Unified AI SDK supporting multiple providers and capabilities.

**Providers:** OpenAI, Anthropic, Gemini, Mistral, Ollama, Cohere, xAI

**Capabilities:**
- Text generation (streaming + non-streaming)
- Structured output (JSON schema enforcement)
- Tool calling / function calling
- Agents (autonomous multi-step)
- RAG (Retrieval-Augmented Generation)
- Embeddings
- Vector stores (pgvector, Pinecone, Qdrant)
- Image generation
- Video generation
- Guardrails
- Tracing
- Memory
- Workflow orchestration
- Cost tracking
- Evaluation
- Document processing

### 40.2 Drive (`plugins/drive/`)

File storage abstraction (like Laravel Filesystem).

**Drivers:** Local FS, S3, Google Cloud Storage  
**Exports:** `Disk` interface with `Put()`, `Get()`, `Delete()`, `GetUrl()`, file serving

### 40.3 Horizon (`plugins/horizon/`)

Queue dashboard (Laravel Horizon style).

**Features:** Dashboard UI, real-time metrics, failed job management, worker configuration, Redis-backed state

### 40.4 Inertia (`plugins/inertia/`)

Inertia.js server-side adapter — build SPAs (Vue/React/Svelte) without writing an API.

**Exports:** `Render(component, props)` for page rendering

### 40.5 MCP (`plugins/mcp/`)

Model Context Protocol server — expose tools, resources, and prompts to AI clients (Claude, etc.).

**Exports:** Tool/Resource/Prompt registration, web server transport

### 40.6 Pulse (`plugins/pulse/`)

Application monitoring dashboard.

**Features:** Request recording middleware, dashboard UI at `/pulse`

### 40.7 Telescope (`plugins/telescope/`)

Debugging/introspection dashboard (like Laravel Telescope).

**Watchers:** Requests, exceptions, logs, database queries  
**Dashboard:** at `/telescope`  
**Implements:** HasMiddleware, HasRoutes, HasConfig, HasViews

### 40.8 Transmit (`plugins/transmit/`)

Server-Sent Events (SSE) for real-time broadcasting.

**Exports:** `Broadcast()`, `BroadcastExcept()`, `Authorize()`  
**Multi-instance:** Redis transport for horizontal scaling  
**Routes:** `__transmit/events`, `__transmit/subscribe`

### 40.9 Unpoly (`plugins/unpoly/`)

Unpoly server protocol integration.

**Features:** Middleware for X-Up headers, context helpers, partial page updates

---

## 41. CLI Commands

### Application Commands

| Command | Description |
|---------|-------------|
| `nimbus new` | Create a new Nimbus application |
| `nimbus serve` | Start the development server |
| `nimbus build` | Build application assets and binary |
| `nimbus repl` | Start a REPL session |
| `nimbus ai` | AI copilot — generate code from natural language |
| `nimbus release` | Release a new version |

### Database Commands

| Command | Description |
|---------|-------------|
| `nimbus db:create` | Create the database |
| `nimbus db:migrate` | Run pending migrations |
| `nimbus db:rollback` | Rollback last migration |
| `nimbus db:seed` | Seed the database |

### Generator Commands

| Command | Description |
|---------|-------------|
| `nimbus make:model` | Create a new GORM model |
| `nimbus make:controller` | Create a new controller |
| `nimbus make:migration` | Create a new migration |
| `nimbus make:middleware` | Create a new middleware |
| `nimbus make:job` | Create a new queue job |
| `nimbus make:seeder` | Create a new seeder |
| `nimbus make:validator` | Create a new validation schema |
| `nimbus make:command` | Create a new CLI command |
| `nimbus make:plugin` | Create a new plugin skeleton |
| `nimbus make:api-token` | Create an API token |
| `nimbus make:auth` | Scaffold auth (login/register/middleware) |
| `nimbus make:deploy-config` | Create deploy.yaml |

### Queue & Schedule Commands

| Command | Description |
|---------|-------------|
| `nimbus queue:work` | Start processing queue jobs |
| `nimbus schedule:run` | Run scheduled tasks |
| `nimbus schedule:list` | List all scheduled tasks |
| `nimbus horizon:forget` | Forget completed/failed jobs |
| `nimbus horizon:clear` | Clear all jobs from a queue |

### Deployment Commands

| Command | Description |
|---------|-------------|
| `nimbus deploy` | Deploy the application |
| `nimbus deploy:init` | Initialize deployment config |
| `nimbus deploy:status` | Check deployment status |
| `nimbus deploy:logs` | View deployment logs |
| `nimbus deploy:env` | Manage deployment env vars |
| `nimbus deploy:rollback` | Rollback to previous deployment |

### Plugin Commands

| Command | Description |
|---------|-------------|
| `nimbus plugin:install` | Install a plugin |
| `nimbus plugin:list` | List available plugins |

### Testing Commands

| Command | Description |
|---------|-------------|
| `nimbus test:generate` | Generate tests from controllers/handlers |

---

## Summary Statistics

| Metric | Count |
|--------|-------|
| **Core Packages** | 30+ |
| **Plugin Packages** | 9 |
| **CLI Commands** | 35+ |
| **Queue Adapters** | 5 (Sync, Redis, Database, SQS, Kafka) |
| **Cache Backends** | 5 (Memory, Redis, Memcached, DynamoDB, Cloudflare KV) |
| **Session Stores** | 4 (Memory, Redis, Database, Encrypted Cookie) |
| **Mail Drivers** | 5 (SMTP, SES, Mailgun, SendGrid, Postmark) |
| **Search Engines** | 3 (PostgreSQL, Meilisearch, Typesense) |
| **AI Providers** | 7 (OpenAI, Anthropic, Gemini, Mistral, Ollama, Cohere, xAI) |
| **Auth Guards** | 3 (Session, Token/PAT, Basic Auth) |
| **OAuth Providers** | 4+ (Google, GitHub, Discord, Apple + custom) |
| **Tenancy Strategies** | 3 (Row, Schema, Database) |

---

*Generated from source code audit of the Nimbus framework repository.*
