# Livewire install file manifest (Nimbus)

This document lists **which files are created** and **which files are modified** when adding Livewire to a Nimbus app.

It covers two flows:

- **Plugin install (existing app)**: `nimbus plugin install livewire`
- **Starter scaffold (new app)**: `nimbus new --starter=livewire <app>`

## Plugin install (existing app)

Command: `nimbus plugin install livewire`

### Modified files

- **`bin/server.go`**
  - Adds the import `github.com/CodeSyncr/nimbus-livewire/livewire`
  - Inserts `app.Use(livewire.New())` into `Boot()`

- **`.env.example`**
  - Appends `APP_KEY=` if it doesn’t already exist (required for cookie/session encryption/signing in many Nimbus setups).

- **`go.mod` / `go.sum`**
  - Updated by `go get github.com/CodeSyncr/nimbus-livewire/livewire`
  - Updated again by the final `go mod tidy`

### Created files

- **None by default** for the Livewire plugin install path.
  - (Other plugins may scaffold config files via `ConfigFiles`, but Livewire currently does not.)

## Starter scaffold (new app)

Command: `nimbus new --starter=livewire <app>`

This flow creates a Basic (server-rendered) app pre-wired for Livewire (session + CSRF middleware, Livewire layout, and a dashboard page that renders a sample component).

### Created files (scaffolded)

- **`start/kernel.go`**
  - Uses the Livewire kernel stub (session + Shield CSRF; no Unpoly).

- **`bin/server.go`**
  - Uses the Livewire server template that registers the plugin: `app.Use(livewire.New())`

- **`app/controllers/auth/auth.go`**
  - Livewire starter auth controller template (login/register/logout flow for Basic apps).

- **`app/controllers/controllers.go`**
  - Livewire starter controllers template (includes a dashboard that renders a Livewire component via `livewire.Render(...)`).

- **Views**
  - **`resources/views/layouts/app.nimbus`** (includes `{{ livewireStyles }}` + `{{ livewireScripts }}`)
  - **`resources/views/pages/auth/login.nimbus`**
  - **`resources/views/pages/auth/register.nimbus`**
  - **`resources/views/pages/dashboard.nimbus`** (dashboard page with embedded Livewire component)
  - **`resources/views/pages/profile.nimbus`**
  - **`resources/views/home.nimbus`**

### Modified files

- **`go.mod` / `go.sum`**
  - Patched by the scaffold to include the Livewire module (see `patchGoModForNimbusLivewire` in `cli/commands/new_templates.go`).

## Notes

- **Component generation commands** (not part of “install”, but commonly used next):
  - `nimbus livewire:layout` creates `resources/views/layouts/app.nimbus` if missing.
  - `nimbus make:livewire <name>` creates:
    - `resources/views/components/<name>.nimbus` (or `resources/views/pages/...` for `pages::...`)
    - `app/livewire/<component>.go`

