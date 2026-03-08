# Nimbus Framework — Full Architecture & TODO (AdonisJS + Laravel Parity)

Nimbus is a batteries-included backend framework for Go focused on developer experience similar to AdonisJS and Laravel while maintaining Go performance.

Goal:

* Laravel-level DX
* AdonisJS-like structure
* Go performance
* Strong typing
* Modular plugin ecosystem

---

# Core Goals

Nimbus should provide the following out of the box:

* HTTP Server
* Routing
* Middleware
* Controllers
* IoC Container
* ORM
* Database migrations
* Authentication
* Authorization
* Templating engine
* Job queues
* Event system
* Websockets
* Mail
* File storage
* Validation
* CLI scaffolding
* Plugin system
* Configuration system
* Logging
* Caching
* Rate limiting
* Scheduler
* Testing utilities

---

# High Level Architecture

```
nimbus/
 ├── cmd/
 │    └── nimbus-cli
 │
 ├── core/
 │    ├── application
 │    ├── container
 │    ├── lifecycle
 │    ├── config
 │    ├── logger
 │    └── environment
 │
 ├── http/
 │    ├── router
 │    ├── context
 │    ├── middleware
 │    ├── request
 │    └── response
 │
 ├── database/
 │    ├── connection
 │    ├── migrations
 │    ├── seeds
 │    └── models
 │
 ├── auth/
 │    ├── guards
 │    ├── providers
 │    └── middleware
 │
 ├── queue/
 ├── mail/
 ├── cache/
 ├── events/
 ├── validation/
 ├── scheduler/
 ├── websocket/
 ├── filesystem/
 ├── view/
 └── testing/
```

---

# Application Lifecycle

Boot stages:

```
init
register providers
boot providers
start server
shutdown hooks
```

Provider interface:

```go
type Provider interface {
	Register(app *Application) error
	Boot(app *Application) error
}
```

---

# IoC Container

Purpose:

* Dependency injection
* Service resolution
* Plugin extensibility

Features:

* Singleton bindings
* Scoped bindings
* Lazy loading

Example:

```go
container.Bind("db", NewDatabase)
container.Make("db")
```

Recommended library:

* uber-go/dig or uber-go/fx

---

# HTTP Layer

Router responsibilities:

* route matching
* middleware execution
* parameter extraction

Recommended router:

* gofiber/fiber or chi

Example:

```
GET /users
POST /users
PUT /users/:id
DELETE /users/:id
```

Controller example:

```go
func GetUsers(ctx *nimbus.Context) error {
 users := userService.All()
 return ctx.JSON(users)
}
```

---

# Middleware System

Features:

* global middleware
* route middleware
* group middleware

Example:

```
auth
csrf
cors
rateLimit
logging
```

Execution pipeline:

```
request
 → middleware
 → controller
 → response
```

---

# ORM Layer

Recommended ORM:

entgo.io/ent

Reasons:

* type safe
* migrations
* relations
* code generation

Alternative:

gorm.io/gorm

Features required:

* model relationships
* soft deletes
* eager loading
* pagination
* query builder

---

# Database Migrations

Commands:

```
nimbus make:migration create_users_table
nimbus migrate
nimbus migrate:rollback
nimbus migrate:reset
```

Migration file example:

```go
func Up(tx *sql.Tx) {
 createTable("users")
}

func Down(tx *sql.Tx) {
 dropTable("users")
}
```

---

# Authentication

Features:

* session auth
* token auth
* JWT
* API keys

Components:

```
guards
providers
middleware
```

Example:

```
auth:web
auth:api
```

---

# Authorization

Policies + roles.

Example:

```
userPolicy.Update(user)
```

---

# Validation

Features:

* request validation
* custom rules
* nested validation

Example:

```go
validator.Validate(data, rules)
```

Rules:

```
required
email
min
max
unique
exists
```

---

# View Engine

Nimbus templating engine using `.nimbus` extension.

Example:

```
home.nimbus
layout.nimbus
```

Syntax:

```
{{ variable }}
@if(condition)
@each(list)
@component
```

View folder:

```
views/
 layouts/
 pages/
 components/
```

Rendering:

```go
ctx.View("home", data)
```

---

# Events System

Event bus for internal communication.

Example:

```
UserRegistered
OrderCreated
PaymentCompleted
```

Listener registration:

```
events.Listen(UserRegistered, SendWelcomeEmail)
```

---

# Job Queue

Features:

* background jobs
* retries
* scheduling
* workers

Example job:

```
SendEmailJob
ProcessVideoJob
```

Queue backends:

* Redis
* RabbitMQ
* SQS

---

# Websocket System

Realtime communication.

Example:

```
chat
notifications
live dashboard
```

Recommended libraries:

* gorilla/websocket
* nhooyr/websocket

---

# Mail System

Drivers:

```
SMTP
SES
Mailgun
Sendgrid
```

Mail template example:

```
emails/welcome.nimbus
```

---

# File Storage

Drivers:

```
local
s3
gcs
```

Example:

```
storage.Put(file)
storage.Get(path)
```

---

# Cache System

Drivers:

```
memory
redis
memcached
```

Features:

```
cache.Set
cache.Get
cache.Remember
```

---

# Rate Limiting

Strategies:

```
IP based
User based
API key based
```

Recommended:

ulule/limiter

---

# Scheduler (Cron)

Example:

```
schedule.EveryMinute()
schedule.Daily()
schedule.Weekly()
```

Example job:

```
cleanupLogs
sendReports
```

---

# CLI Tool

Use cobra.

Commands:

```
nimbus new app
nimbus serve
nimbus make:controller
nimbus make:model
nimbus make:migration
nimbus make:middleware
nimbus make:job
```

---

# Plugin System

Plugins are service providers.

Example:

```
nimbus add auth
nimbus add redis
nimbus add mail
```

Plugin structure:

```
plugin/
 provider.go
 config.go
 routes.go
```

---

# Configuration System

Sources:

```
.env
yaml
json
```

Example:

```
config/app.go
config/database.go
```

Access:

```
config.Get("database.default")
```

---

# Logging

Recommended:

uber-go/zap

Features:

```
structured logs
log levels
file output
JSON logs
```

---

# Testing Utilities

Features:

* HTTP test client
* database transactions
* test helpers

Example:

```
nimbus test
```

---

# Dev Tools

Add:

* hot reload
* file watcher
* debug dashboard
* request logging

Hot reload library:

air

---

# Full Feature Parity Checklist

Core

* [x] Application kernel
* [x] Service providers
* [x] IoC container
* [x] config loader
* [x] env loader

HTTP

* [x] router
* [x] middleware pipeline
* [x] request context
* [x] response helpers

Database

* [x] ORM integration
* [x] migrations (basic Migrator)
* [x] seeds (Seeder interface, SeedRunner, make:seeder)

Security

* [x] auth guards (Guard, SessionGuard, RequireAuth middleware)
* [x] roles (Policy interface, auth.Policy)
* [x] csrf (CSRF middleware, MemoryCSRFStore)

Views

* [x] template compiler (.nimbus, {{ }}, @if, @each, view.Render)
* [x] components (via template funcs / partials)
* [x] layouts (views/ folder, view.SetRoot)

Async

* [x] job queue (queue.Job, Queue, make:job)
* [x] events (events.Listen, events.Dispatch)

Infrastructure

* [x] cache (cache.Store, MemoryStore, Set/Get/Remember)
* [x] storage (storage.Driver, LocalDriver, Put/Get)
* [x] mail (mail.Driver, SMTPDriver, mail.Send)

Realtime

* [x] websockets (websocket.Hub, Upgrade, Broadcast)

CLI

* [x] project generator
* [x] scaffolding (make:model, make:migration, make:controller, make:middleware, make:job, make:seeder)
* [x] Cobra-based CLI
* [x] make:controller, make:middleware
* [x] migrate, migrate:rollback (stubs; run migrations from app)

DX

* [x] hot reload (documented: air)
* [x] testing utilities (testing.TestClient, Get/Post/Do, AssertStatus)

---

# Example Nimbus Application

```
app/
 controllers/
 models/
 middleware/

routes/
 web.go

config/

views/

main.go
```

main.go:

```go
func main() {
 app := nimbus.New()

 app.Register(DatabaseProvider{})
 app.Register(AuthProvider{})

 app.Start()
}
```

---

# Future Ideas

* server components
* API auto generation
* GraphQL module
* distributed job system
* edge deployment support
* plugin marketplace
