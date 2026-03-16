# Nimbus Framework — Gaps Status

Status of critical and important gaps from the Laravel-parity analysis.  
**Legend:** ✅ Done | ⚠️ Partial | ❌ Not done

---

## Critical Gaps (Must-Have for Production)

### 1. Authentication & Session
| Item | Status | Notes |
|------|--------|-------|
| Session middleware (reads/writes cookies) | ✅ | `session.Middleware(Config)` with cookie-based store |
| Session store (DB, Redis, cookie) | ✅ | MemoryStore, CookieStore, DatabaseStore, RedisStore |
| Password hashing (bcrypt) | ✅ | `hash.Make()`, `hash.Check()`, `hash.MakeWithCost()` |
| Auth scaffolding (make:auth) | ✅ | User model, users migration, AuthController, login/register/logout views |
| SessionGuard uses session store | ✅ | `NewSessionGuardWithLoader()` + `session.FromContext()` for persistent auth |
| Password reset flow | ✅ | `auth.PasswordResetBroker` with token store, SendResetLink, Reset |
| Email verification flow | ✅ | `auth.EmailVerifier` with SendVerification, Verify, MustVerifyEmail interface |
| Secure token store | ✅ | `auth.TokenStore` with HMAC-SHA256 hashing, TTL expiry, cleanup |

### 2. Database Seeders CLI
| Item | Status | Notes |
|------|--------|-------|
| nimbus db:seed command | ✅ | `nimbus db:seed` runs seeders from `database/seeders/registry.go` |
| make:seeder | ✅ | Scaffolds seeder, adds to registry |

### 3. Migration Registry (DX)
| Item | Status | Notes |
|------|--------|-------|
| Auto-register migrations | ✅ | `make:migration` auto-inserts into `database/migrations/registry.go` |
| migrate:fresh | ✅ | `Migrator.Fresh()` drops all tables and re-runs migrations |
| migrate:status | ✅ | `Migrator.PrintStatus()` shows Ran/Pending status with batch and timestamp |

### 4. API Resources
| Item | Status | Notes |
|------|--------|-------|
| Resource interface (ToJSON) | ✅ | `resource.Resource`, `resource.ResourceFunc`, `resource.Collection()` |

### 5. Validation Error Formatting
| Item | Status | Notes |
|------|--------|-------|
| ValidationErrors type | ✅ | `validation.ValidationErrors` (map[string][]string) |
| FromValidator / FormatValidationError | ✅ | Helpers for API JSON responses |

### 6. Mail Drivers
| Item | Status | Notes |
|------|--------|-------|
| SMTP | ✅ | `mail.SMTPDriver` with CC/BCC/Attachments/ReplyTo |
| SES (SMTP) | ✅ | `mail.SESDriver` (SMTP-backed) |
| SES (API) | ✅ | `mail.SESAPIDriver` (native HTTP API, no SDK required) |
| Mailgun (SMTP) | ✅ | `mail.MailgunDriver` (SMTP-backed) |
| Mailgun (API) | ✅ | `mail.MailgunAPIDriver` (native HTTP API) |
| SendGrid (API) | ✅ | `mail.SendGridDriver` (native v3 HTTP API) |
| Resend (API) | ✅ | `mail.ResendDriver` (native HTTP API) |
| Postmark | ✅ | `mail.PostmarkDriver` (SMTP-backed) |
| CC / BCC / Attachments | ✅ | `Message.AddCc()`, `AddBcc()`, `Attach()` with MIME multipart |

### 7. Rate Limiting
| Item | Status | Notes |
|------|--------|-------|
| In-memory | ✅ | `middleware.RateLimit()` |
| Redis-backed | ✅ | `middleware.RateLimitRedis()` for multi-instance |

---

## Important Gaps (Laravel Parity)

### 8. Notifications
| Item | Status | Notes |
|------|--------|-------|
| Notification interface | ✅ | `notification.Notification` with ToMail/ToBroadcast |
| Mail channel | ✅ | Via `mail` package |
| Broadcast channel | ✅ | Via Transmit SSE |
| Database channel | ✅ | `notification.DatabaseChannel` with GORM — store, unread, mark read, delete |
| Slack channel | ✅ | `notification.SlackChannel` via incoming webhooks with attachments |
| Discord channel | ✅ | `notification.DiscordChannel` via webhooks with embeds |

### 9. Broadcasting
| Item | Status | Notes |
|------|--------|-------|
| WebSocket Hub | ✅ | `websocket.Hub` for WS; SSE via Transmit |
| Laravel-style broadcasting (Pusher, Redis) | ✅ | Transmit SSE + Redis transport for multi-instance broadcast |
| Channel authorization / presence | ✅ | Transmit `Authorize` / `CheckChannel` + `GetSubscribers` |

### 10. Telescope Completion
| Item | Status | Notes |
|------|--------|-------|
| Telescope panels | ⚠️ | Many use placeholder ("Coming soon"): commands, schedule, jobs, batches, cache, events, gates, http-client, logs, mail, notifications, redis |

### 11. Form Requests
| Item | Status | Notes |
|------|--------|-------|
| FormRequest base (Rules, Authorize, Messages) | ✅ | `validation.FormRequest[T]` + `BindAndValidate` |

### 12. Error Handling
| Item | Status | Notes |
|------|--------|-------|
| Global error handler | ✅ | `errors.Handler()` middleware with validation + HTTPError + AppError support |
| Error IDs + tracking | ✅ | `errors.AppError` with unique ID, logged + returned for support reference |
| Error reporting | ✅ | `errors.RegisterReporter()` — external reporters (Sentry, Bugsnag, etc.) |
| Error views (404, 500) | ⚠️ | Can be implemented per app; core returns JSON/text |
| JSON error responses for APIs | ✅ | 422 for validation, status-aware HTTPError JSON, error_id for 500s |

### 13. Localization
| Item | Status | Notes |
|------|--------|-------|
| i18n/l10n | ⚠️ | In-memory translations via `locale.AddTranslations` |
| T() / __() helper | ✅ | `locale.T` and `locale.TLocale` |
| Locale middleware | ✅ | `locale.Middleware()` (Accept-Language based) |

### 14. Task Scheduling
| Item | Status | Notes |
|------|--------|-------|
| schedule.Scheduler | ✅ | Named tasks, panic recovery, daily-at scheduling, non-blocking Start/Stop |
| scheduler (legacy) | ✅ | Deprecated — thin wrapper forwarding to `schedule` package |
| nimbus schedule:run | ✅ | CLI command delegates to app's `schedule:run` |
| schedule:list | ✅ | CLI delegates to app's `schedule:list` |
| Cron docs | ✅ | Detailed cron + schedule:run/list docs in scheduler.nimbus |

### 15. Health Checks
| Item | Status | Notes |
|------|--------|-------|
| /health endpoint | ✅ | `health.Checker` with DB/Redis checks |
| Handler() for JSON | ✅ | 200 OK / 503 Service Unavailable |

### 16. CLI Generators
| Item | Status | Notes |
|------|--------|-------|
| make:model | ✅ | GORM model with timestamps |
| make:controller | ✅ | HTTP controller scaffolding |
| make:migration | ✅ | Timestamped migration with Up/Down |
| make:middleware | ✅ | HTTP middleware wrapper |
| make:job | ✅ | Queue job with Handle/MaxRetries |
| make:seeder | ✅ | Database seeder |
| make:validator | ✅ | Validation schema |
| make:command | ✅ | Cobra CLI command |
| make:plugin | ✅ | Full plugin skeleton (9 files) |
| make:event | ✅ | Event constant + payload struct |
| make:listener | ✅ | Event listener function |
| make:notification | ✅ | Multi-channel notification |
| make:policy | ✅ | Authorization resource policy |
| make:resource | ✅ | API resource transformer |
| make:factory | ✅ | Model factory for testing |
| make:observer | ✅ | Model lifecycle observer |
| make:rule | ✅ | Custom validation rule |

### 17. Route & Migration CLI
| Item | Status | Notes |
|------|--------|-------|
| route:list | ✅ | `router.PrintRoutes()` — formatted table with Method/Path/Name/Summary |
| migrate:fresh | ✅ | `Migrator.Fresh()` — drops all tables, re-runs all migrations |
| migrate:status | ✅ | `Migrator.PrintStatus()` — shows Ran/Pending with batch number and timestamp |

---

## Production Hardening

### HTTP Context & Middleware
| Item | Status | Notes |
|------|--------|-------|
| context.Context propagation | ✅ | `c.Ctx()`, `c.WithContext()`, `c.Done()`, `c.Err()` on http.Context |
| Request ID middleware | ✅ | `middleware.RequestID()` — crypto/rand 16-byte hex, X-Request-Id header |
| Request timeout middleware | ✅ | `middleware.Timeout(d)` — wraps request with context.WithTimeout |
| Body size limit middleware | ✅ | `middleware.BodyLimit(maxBytes)` — http.MaxBytesReader |
| Gzip compression middleware | ✅ | `middleware.Gzip()` — sync.Pool gzip writers, 256-byte min threshold |
| Secure headers middleware | ✅ | `middleware.SecureHeaders()` — HSTS, X-Frame-Options, XSS-Protection, etc. |
| Trusted proxies middleware | ✅ | `middleware.TrustedProxies()` — strips forwarding headers from untrusted IPs |
| Prometheus metrics middleware | ✅ | `middleware.Metrics()` — request count, duration histogram, in-flight gauge |
| Fallback route handler | ✅ | `router.Fallback()` — catch-all 404 handler |

### Metrics & Observability
| Item | Status | Notes |
|------|--------|-------|
| Prometheus counters | ✅ | `metrics.Counter` with labels, atomic operations |
| Prometheus gauges | ✅ | `metrics.Gauge` with Inc/Dec/Set/Add |
| Prometheus histograms | ✅ | `metrics.Histogram` with configurable buckets |
| Metrics registry | ✅ | `metrics.DefaultRegistry` with Expose() → Prometheus text format |
| Metrics HTTP handler | ✅ | `metrics.Handler()` for /metrics endpoint |
| Runtime stats | ✅ | `metrics.ReadRuntimeStats()` — goroutines, heap, GC |

### Logger
| Item | Status | Notes |
|------|--------|-------|
| Request-scoped logger | ✅ | `logger.ForRequest(c)` — carries request_id, `logger.WithContext()` |
| Log rotation | ✅ | `logger.RotatingWriter` — max size, max backups, auto-cleanup |

### Config & Container
| Item | Status | Notes |
|------|--------|-------|
| Env validation | ✅ | `config.ValidateEnv()` — Required, OneOf, Default rules |
| Required env shorthand | ✅ | `config.Required("APP_KEY", "DB_DSN")` |
| Container auto-wiring | ✅ | Constructor params auto-resolved by type from container bindings |

### Validation
| Item | Status | Notes |
|------|--------|-------|
| Conditional rules | ✅ | `validation.When(field, value, rule)` — apply rule when condition met |
| Function-based conditions | ✅ | `validation.WhenFn(predicate, rule)` — predicate-based conditional |
| Otherwise fallback | ✅ | `.Otherwise(rule)` — alternative rule when condition not met |

### Route Model Binding
| Item | Status | Notes |
|------|--------|-------|
| Bindable interface | ✅ | `router.Bindable` — RouteKey() + FindForRoute() |
| BindModel middleware | ✅ | `router.BindModel()` — auto-resolve {id} → model from DB |
| ParamInt / ParamInt64 | ✅ | Typed route parameter extraction helpers |

### Cache
| Item | Status | Notes |
|------|--------|-------|
| Cache locks | ✅ | `cache.Lock` — Acquire/Release/Block for thundering herd prevention |
| AtomicLock helper | ✅ | `cache.AtomicLock()` — acquire, run fn, release in one call |

---

## Nice-to-Have (Laravel Ecosystem)

| Feature | Laravel | Nimbus |
|---------|---------|--------|
| Horizon (queue dashboard) | ✅ | ⚠️ Basic Horizon plugin (`plugins/horizon`) with in-memory stats and dashboard |
| Telescope (full) | ✅ | ⚠️ Partial |
| Scout (search) | ✅ | ❌ |
| Socialite (OAuth) | ✅ | ❌ |
| Passport/Sanctum (API auth) | ✅ | ❌ |
| Pulse (monitoring) | ✅ | ❌ |
| Pest/PHPUnit (testing) | ✅ | ⚠️ Basic |
| Dusk (browser tests) | ✅ | ❌ |
| Laravel Echo (WebSocket client) | ✅ | ❌ |

---

## Summary

**Completed (production-ready):**
- Auth & sessions (middleware, DB/cookie store, bcrypt, make:auth, password reset, email verification)
- Mail with CC/BCC/attachments (SMTP, SES, Mailgun, SendGrid, Postmark)
- Notifications (mail, broadcast, database, Slack, Discord channels)
- db:seed, migrate:fresh, migrate:status CLI
- Migration auto-registry
- Validation errors (structured format) + DateRule, ArrayRule, FileRule, MapRule
- API resources
- Redis rate limiting
- Health checks
- 17 CLI generators (model, controller, migration, middleware, job, seeder, validator, command, plugin, event, listener, notification, policy, resource, factory, observer, rule)
- route:list — formatted route table with method/path/name/summary
- **HTTP Context** — full request helpers (Input, Bind, Cookie, File, IP, UserAgent, IsAjax, IsJSON) + response helpers (NoContent, Created, BadRequest, HTML, SSE, Stream, Download, CacheControl, etc.)
- **Query Builder** — WhereIn, WhereNotIn, WhereBetween, WhereLike, WhereRaw, Join, LeftJoin, RightJoin, GroupBy, Having, Count, Sum, Avg, Max, Min, Distinct, Preload, Scopes, Unscoped, Create, Update, Updates, Delete, Pluck, Exists, Paginate, Last, Find, FirstOrFail, Raw, Exec
- **Encryption** — AES-256-GCM with Encrypt/Decrypt, EncryptString/DecryptString, key generation, deterministic encryption
- **S3 Storage Driver** — Put, Get, Delete, DeleteMany, Exists, Size, LastModified, Copy, Move, URL, TemporaryURL, TemporaryUploadURL, List
- **Logger** — structured logging with JSON/console format, multiple channels (stdout/stderr/file), dynamic level changes, channel-specific configuration, WithFields, Sync
- **Cursor Pagination** — CursorPaginate (keyset pagination) + SimplePaginate (no total count)
- **Testing DB Assertions** — AssertDatabaseHas, AssertDatabaseMissing, AssertDatabaseCount, AssertSoftDeleted, AssertNotSoftDeleted, AssertModelExists, AssertModelMissing
- **Queue Batch/Chain/Unique** — Job chains (sequential), batches (concurrent with Then/Catch/Finally), unique jobs (dedup with locks), WithoutOverlapping
- **Multi-DB Connections** — ConnectionManager with AddConnection, Connection(name), ConnectAll, On(name) query builder, OnModel, SetDefault, CloseAll
- **NoSQL / MongoDB** — Full MongoDB driver (nosql.Driver/Collection interfaces), ConnectMongo, InsertOne/Many, FindOne/Find/FindByID, UpdateOne/Many/Upsert, DeleteOne/Many, Aggregate, Distinct, CreateIndex, DropIndex
- **NoSQL Query Builder** — Fluent API: Query("mongo","users").Where().WhereIn().WhereBetween().Sort().Limit().Skip().Select().Get() + Insert/Update/Delete/Upsert/Paginate/Count/Exists/Distinct/Aggregate
- **NoSQL Connection Manager** — Register/Connection/CloseAll for named NoSQL connections, nosql.Model (mirrors database.Model)

**Partial:**
- Error views (HTML pages) and Telescope panels remain partial; core scheduling + CLI + cron docs are complete.

**Production Hardening (new):**
- context.Context propagation in HTTP context
- 7 new middleware: RequestID, Timeout, BodyLimit, Gzip, SecureHeaders, TrustedProxies, Metrics
- Prometheus-compatible metrics (Counter, Gauge, Histogram) with /metrics endpoint
- Request-scoped logger with request_id correlation
- Log file rotation with max size + max backups
- Env validation (Required, OneOf, Default) at boot
- Container auto-wiring (constructor params resolved by type)
- Route model binding (Bindable interface, BindModel middleware)
- Fallback route handler for custom 404 pages
- Cache locks for thundering herd prevention
- Conditional validation rules (When, WhenFn, Otherwise)
- Error IDs + external error reporting (Sentry, Bugsnag integration points)
- Native mail API drivers: SendGrid v3, Mailgun API, SES API, Resend
- Scheduler consolidated: `schedule/` is canonical, `scheduler/` is deprecated wrapper
