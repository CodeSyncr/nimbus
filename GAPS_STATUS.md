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

### 2. Database Seeders CLI
| Item | Status | Notes |
|------|--------|-------|
| nimbus db:seed command | ✅ | `nimbus db:seed` runs seeders from `database/seeders/registry.go` |
| make:seeder | ✅ | Scaffolds seeder, adds to registry |

### 3. Migration Registry (DX)
| Item | Status | Notes |
|------|--------|-------|
| Auto-register migrations | ✅ | `make:migration` auto-inserts into `database/migrations/registry.go` |

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
| SMTP | ✅ | `mail.SMTPDriver` |
| SES | ✅ | `mail.SESDriver` (SMTP-backed) |
| Mailgun | ✅ | `mail.MailgunDriver` (SMTP-backed) |
| SendGrid | ✅ | `mail.SendGridDriver` (SMTP-backed) |
| Postmark | ✅ | `mail.PostmarkDriver` (SMTP-backed) |

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
| Channels (mail, broadcast) | ✅ | Mail via `mail` package, realtime via Transmit |

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
| Global error handler | ✅ | `errors.Handler()` middleware with validation + HTTPError support |
| Error views (404, 500) | ⚠️ | Can be implemented per app; core returns JSON/text |
| JSON error responses for APIs | ✅ | 422 for validation, status-aware HTTPError JSON |

### 13. Localization
| Item | Status | Notes |
|------|--------|-------|
| i18n/l10n | ⚠️ | In-memory translations via `locale.AddTranslations` |
| T() / __() helper | ✅ | `locale.T` and `locale.TLocale` |
| Locale middleware | ✅ | `locale.Middleware()` (Accept-Language based) |

### 14. Task Scheduling
| Item | Status | Notes |
|------|--------|-------|
| queue.Scheduler / scheduler.Scheduler | ✅ | Core schedulers for jobs and tasks |
| nimbus schedule:run | ✅ | CLI command delegates to app's `schedule:run` |
| schedule:list | ✅ | CLI delegates to app's `schedule:list` (starter implements listing via start.RegisterSchedule) |
| Cron docs | ✅ | Detailed cron + schedule:run/list docs in scheduler.nimbus |

### 15. Health Checks
| Item | Status | Notes |
|------|--------|-------|
| /health endpoint | ✅ | `health.Checker` with DB/Redis checks |
| Handler() for JSON | ✅ | 200 OK / 503 Service Unavailable |

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
- Auth & sessions (middleware, DB/cookie store, bcrypt, make:auth)
- db:seed CLI
- Migration auto-registry
- Validation errors (structured format)
- API resources
- Redis rate limiting
- Health checks

**Partial:**
- Error views (HTML pages) and Telescope panels remain partial; core scheduling + CLI + cron docs are complete.
