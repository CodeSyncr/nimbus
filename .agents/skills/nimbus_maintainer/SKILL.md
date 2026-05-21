---
name: nimbus_maintainer
description: >
  Mandatory workflow rules and sync procedures for maintaining the Nimbus Go web framework
  (Laravel/AdonisJS-inspired). Use this skill whenever you are adding, modifying, removing,
  or refactoring any part of the Nimbus framework — including routing, middleware, ORM/database
  layers, service providers, facades, view/template engine, CLI artisan-style commands, plugins,
  SSE/WebSocket support, or any core package (nimbus/str, nimbus/collect, nimbus/timex,
  nimbus/pipeline). Also triggers for changes to the nimbus-starter scaffold app, or updates to
  the nimbus_expert agent skill documentation. Never skip this skill for framework changes —
  documentation drift is a first-class bug.
---

# Nimbus Maintainer Skill

Nimbus is a Go web framework inspired by **Laravel** (PHP) and **AdonisJS** (Node.js). It
prioritizes developer ergonomics through conventions, first-class CLI tooling, an expressive ORM,
a service-container/IoC pattern, facade-style globals, and a `.nimbus` template engine — all
idiomatic Go underneath.

This skill enforces the **Golden Rule**: every code change must be immediately reflected in both
the `nimbus-starter` documentation app and the `nimbus_expert` agent skill files. Documentation
drift is treated as a bug.

---

## Paths Reference

| Target | Path |
|---|---|
| Nimbus framework source | `/Users/yashkumar/Documents/Projects/nimbus/` |
| nimbus-starter docs views | `/Users/yashkumar/Documents/Projects/nimbus-starter/resources/views/docs/` |
| nimbus_expert skill root | `/Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/` |
| nimbus_expert skill index | `/Users/yashkumar/Documents/Projects/nimbus/.agents/skills/nimbus_expert/SKILL.md` |

---

## The Three-Step Sync Rule

Every framework change MUST complete all three steps before the task is considered done.

### Step 1 — Make the framework change

Apply the code modification in the `nimbus` repository. Before writing anything:

- Confirm the Go API signature (receiver, parameters, return types, error handling pattern).
- Confirm the naming convention: Nimbus uses Laravel-style fluent builders and AdonisJS-style
  lifecycle hooks — keep this consistent.
- If adding a new concept (provider, facade, command, plugin), decide first where it lives in
  the package hierarchy.

Validate the framework itself compiles cleanly:

```bash
cd /Users/yashkumar/Documents/Projects/nimbus
go build ./...
go vet ./...
```

---

### Step 2 — Update `nimbus-starter` documentation

The `nimbus-starter` app is the living documentation — it doubles as a working scaffold that new
users clone. Its docs live as `.nimbus` templates under `resources/views/docs/`.

**Which file to update:**

| Area changed | Doc file to update |
|---|---|
| Routing, middleware, groups | `routing.nimbus` |
| ORM, models, migrations, seeds | `database.nimbus` |
| Views, layouts, components, slots | `views_templates.nimbus` |
| Service providers, container bindings | `providers.nimbus` |
| Facades | `facades.nimbus` |
| CLI commands (artisan-style) | `cli.nimbus` |
| Validation | `validation.nimbus` |
| Authentication & sessions | `auth.nimbus` |
| SSE / WebSocket / real-time | `realtime.nimbus` |
| Plugins / extension API | `plugins.nimbus` |
| Core packages (str, collect, timex, pipeline) | `helpers.nimbus` |
| Config loading, env | `configuration.nimbus` |

**What to update inside the doc file:**

1. Update prose explanations if behavior changed.
2. Replace all affected code snippets — never leave a stale example.
3. Update CLI invocation examples (flag names, subcommands, output format).
4. If a feature was removed, remove its section entirely — don't just comment it out.
5. If a feature is new, add a section following the existing doc structure (intro → basic usage
   → options table → full example → gotchas).

**Validate the starter app still works:**

```bash
cd /Users/yashkumar/Documents/Projects/nimbus-starter
go build ./...
# Then start the dev server and navigate to the affected doc page
go run . serve
```

---

### Step 3 — Update `nimbus_expert` skill files

The `nimbus_expert` skill is what AI agents use to answer questions and generate correct Nimbus
code. Stale skill files produce hallucinated APIs — this is a critical correctness issue.

**Which skill file to update:**

| Area changed | Skill file |
|---|---|
| Routing | `routing.md` |
| ORM / Database | `orm.md` |
| Views & Templates | `views_templates.md` |
| Service container / Providers | `providers.md` |
| Facades | `facades.md` |
| CLI commands | `cli.md` |
| Validation | `validation.md` |
| Auth | `auth.md` |
| SSE / Real-time | `realtime.md` |
| Plugins | `plugin.md` |
| Core helper packages | `helpers.md` |
| Config / Env | `configuration.md` |

**What to update inside each skill file:**

1. All Go function/method signatures — name, receiver, parameter list, return types.
2. All struct field names and their types.
3. CLI command syntax, flags, and example output.
4. Configuration keys and their valid values.
5. Remove any reference to deleted APIs entirely.
6. Add new APIs with a minimal working example snippet.

**If you added a brand-new top-level concept (new plugin, new subsystem):**

1. Create a new `.md` file under `nimbus_expert/`.
2. Add an entry to `nimbus_expert/SKILL.md` in the index table with a one-line description and
   the trigger condition for when an agent should read this file.

---

## Naming & Convention Rules

Nimbus follows these conventions — maintain them in both code and docs:

| Concept | Convention (Laravel/AdonisJS analogy) |
|---|---|
| Route registration | `app.Get("/path", handler)` — fluent, chainable |
| Middleware | `nimbus.Use(fn)` at app or group level |
| ORM model definition | Struct embedding `nimbus.Model` with GORM-style tags |
| Migration files | Timestamped: `2024_01_01_000001_create_users_table.go` |
| Service providers | Implement `nimbus.Provider` interface (`Register`, `Boot`) |
| Facades | Package-level vars that delegate to container-resolved services |
| CLI commands | Implement `nimbus.Command` interface, registered in `Kernel` |
| Template engine | `.nimbus` files with `@extends`, `@section`, `@yield`, `@component` directives |
| Events | `nimbus.Emit(event)` / `nimbus.On(event, listener)` |
| Config access | `nimbus.Config("app.name")` — dot-notation |

---

## Code Snippet Quality Rules

All snippets in docs and skill files must meet these standards:

- **Compile-correct**: mentally trace through the snippet — imports, receivers, and types must
  all be valid Go.
- **Minimal but complete**: show enough context to be copy-pasteable (package declaration +
  imports when non-obvious).
- **Idiomatic**: use Nimbus conventions, not raw `net/http` patterns, unless explicitly
  demonstrating the lower layer.
- **Error handling shown**: Go snippets must handle errors — never use `_` to discard errors in
  doc examples.
- **No placeholder hallucinations**: do not write `// ... rest of implementation`. Show real
  code or explicitly note what the user must add.

---

## Completion Checklist

Before marking any framework task done, verify:

- [ ] `go build ./...` passes in the `nimbus` repo
- [ ] `go vet ./...` passes in the `nimbus` repo
- [ ] Affected `.nimbus` doc template(s) in `nimbus-starter` updated
- [ ] `go build ./...` passes in the `nimbus-starter` repo
- [ ] Affected `.md` skill file(s) in `nimbus_expert/` updated
- [ ] If new concept added: new skill file created and linked in `nimbus_expert/SKILL.md`
- [ ] All code snippets in updated docs are syntax-correct Go
- [ ] Removed APIs are fully purged from docs and skill files (no dead references)

---

## Reference Files

Read these from `nimbus_expert/` when you need deep API detail on a specific subsystem.
Only read the files relevant to the current change.

- `routing.md` — Route registration, groups, named routes, resource routes
- `orm.md` — Models, migrations, query builder, relationships, seeds
- `views_templates.md` — `.nimbus` template syntax, layouts, components, slots, directives
- `providers.md` — Service container, binding, resolution, provider lifecycle
- `facades.md` — Facade pattern implementation and adding new facades
- `cli.md` — Command definition, flags, prompts, scheduling
- `plugin.md` — Plugin interface, hooks, registration
- `helpers.md` — `nimbus/str`, `nimbus/collect`, `nimbus/timex`, `nimbus/pipeline` APIs
- `realtime.md` — SSE, WebSocket, broadcasting
- `auth.md` — Authentication guards, sessions, tokens
- `validation.md` — Rule definition, custom rules, error formatting
- `configuration.md` — Config files, env binding, dot-notation access