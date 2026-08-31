---
name: nimbus_expert
description: Deep expertise in the Nimbus Go framework and its AI integration capabilities.
---

# Nimbus Expert Skill

This skill provides comprehensive knowledge of the Nimbus Go framework, an AdonisJS-style framework for building web applications in Go. It includes details on the framework's architecture, ORM, NoSQL support, plugin system, advanced AI integration, and core utility services.

## When to Use

Use this skill when:
- Designing or implementing features in a Nimbus application.
- Troubleshooting issues related to routing, middleware, database, or background services.
- Integrating AI capabilities (Agents, Tools, RAG) using the `plugins/ai` package.
- Setting up MCP (Model Context Protocol) servers within Nimbus.
- Building custom CLI commands or project generators.
- Writing tests for Nimbus applications.

## Knowledge Areas

### Core Framework
- [Framework Overview](file:///Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/framework_overview.md): Core architecture and lifecycle.
- [Configuration](file:///Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/configuration.md): Layered config and `.env` loading.
- [Routing & Controllers](file:///Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/routing_controllers.md): Route groups, params, resources, controllers.
- [Validation](file:///Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/validation.md): Chainable rules and Form Requests.
- [Views & Templates](file:///Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/views_templates.md): `.nimbus` engine, layouts, and components.
- [CLI](file:///Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/cli.md): Scaffolding and generator commands.
- [Type-Safe Client (Hive)](file:///Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/hive.md): End-to-end type safety with client proxies.


### Database & Security
- [ORM (SQL)](file:///Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/orm.md): GORM-based database management.
- [NoSQL](file:///Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/nosql.md): MongoDB and Redis integration.
- [Authentication](file:///Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/auth.md): Auth guards, tokens, and policies.
- [CORS & Security](file:///Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/cors.md): Middleware for security.
- [Advanced Features](file:///Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/advanced_features.md): Health checks, Shield, and Service Container.

### Services
- [Queue & Jobs](file:///Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/queue_jobs.md): Background job processing.
- [Cache](file:///Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/cache.md): Multi-driver caching and locks.
- [Redis](file:///Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/redis.md): Direct Redis client — strings, hashes, lists, sets, pub/sub.
- [Mail](file:///Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/mail.md): Unified mail API.

- [Scheduler](file:///Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/scheduler.md): Recurring task management.
- [Notification](file:///Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/notification.md): Multi-channel notifications.
- [Search](file:///Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/search.md): Full-text search (Meilisearch, Typesense, PostgreSQL).
- [WebSocket](file:///Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/websocket.md): WebSocket server and channels.

### AI & Integration
- [AI SDK](file:///Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/ai_plugin_deep_dive.md): Advanced AI orchestration.
- [MCP Integration](file:///Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/mcp_integration.md): Model Context Protocol support.

### Plugins
- [Plugin System](file:///Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/plugin.md): Extensibility and plugin development.
- [Drive](file:///Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/drive_plugin.md): File storage (Local, S3, GCS, R2, Spaces, Supabase).
- [Transmit](file:///Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/transmit_plugin.md): SSE real-time push.
- [Horizon](file:///Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/horizon_plugin.md): Queue dashboard.
- [Inertia](file:///Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/inertia_plugin.md): Vue/React/Svelte SPA adapter.
- **Livewire** — server-driven reactive components. Its skill lives in its own repo: `nimbus-livewire/.agents/skills/livewire_expert/` (`SKILL.md` + topic files).
- [Reverb](file:///Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/reverb_plugin.md): WebSocket broadcasting.
- [Supabase](file:///Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/supabase_plugin.md): Supabase services integration.
- [Cashier](file:///Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/cashier_plugin.md): Multi-gateway payments (Stripe, Razorpay, PayU) with default selection & paywall.
- [Passport](file:///Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/passport_plugin.md): OAuth2 authorization server (authorization_code+PKCE, client_credentials, refresh) with introspection & scopes.
- [Captcha](file:///Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/captcha.md): Automated AI captcha solving (CapSolver alternative via Nimbus Cloud) & bot verification middleware.
- [Admin](file:///Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/admin_plugin.md): Nova/Filament-style reflection-driven CRUD admin panel over your models.

### Development
- [Testing](file:///Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/testing.md): HTTP helpers and AI test generation.
- [Browser Testing (Dusk)](file:///Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/browser_testing.md): In-process E2E browser — visit, forms, links, fluent assertions.
- [Serverless / AWS Lambda](file:///Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/serverless.md): Run the app on Lambda via the `serverless` adapter; `make:lambda` / `new --lambda`.
- [Best Practices](file:///Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/usage_best_practices.md): Development guidelines.

## Instructions

1. **Always refer to the specific documentation files** for detailed information on components.
2. **Follow AdonisJS conventions** when suggesting code structures.
3. **Prioritize the AI SDK** for any AI-related tasks.
4. **Use CLI Generators** for scaffolding boilerplate.
5. **Always include tests** in your suggested code, leveraging Nimbus testing patterns.
