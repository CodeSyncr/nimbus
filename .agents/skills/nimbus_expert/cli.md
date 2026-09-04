# CLI — Nimbus

Complete reference for the `nimbus` command line interface. Every command the
binary registers is listed here with its arguments, aliases, flags, and what it
writes to disk.

**Source of truth:** commands live in `cli/commands/*.go` and self-register from
`init()` via `cli.RegisterCommand`. If a command exists in that directory it is
in this document.

---

## Installation

```bash
go install github.com/CodeSyncr/nimbus/cmd/nimbus@latest
nimbus --version
```

### PATH setup

| OS / shell | Command |
| --- | --- |
| macOS (zsh) | `echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.zshrc && source ~/.zshrc` |
| Linux (bash) | `echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.bashrc && source ~/.bashrc` |
| Windows (PowerShell) | `[Environment]::SetEnvironmentVariable("Path", [Environment]::GetEnvironmentVariable("Path", "User") + ";$env:USERPROFILE\go\bin", "User")` |
| Windows (CMD) | `setx PATH "%PATH%;%USERPROFILE%\go\bin"` |

### Global flags

Provided by Cobra on the root command (`cmd/nimbus/main.go`):

| Flag | Effect |
| --- | --- |
| `--version` | Print the framework version (`internal/version.Nimbus`) and exit |
| `-h`, `--help` | Help for the root or any subcommand |

On every start the CLI calls `ai.EnsureDefaultSkills()`, which materialises the
bundled agent skills under `~/.nimbus/` before any command runs.

---

## How commands are wired

`cli.Command` is the minimum interface; three optional interfaces extend it
(`cli/registry.go`):

```go
type Command interface {
    Name() string          // "make:model"
    Description() string   // one-line summary shown in help
    Run(ctx *Context) error
}

type CommandWithFlags   interface{ Command; Flags(cmd *cobra.Command) }
type CommandWithAliases interface{ Command; Aliases() []string }
type CommandWithArgs    interface{ Command; Args() int } // exact count, -1 = any
}
```

`Args()` returning `n >= 0` becomes `cobra.ExactArgs(n)`; returning `-1` accepts
any number of positional arguments.

`cli.RegisterRootAttach` adds *nested* command trees (`nimbus plugin install`)
alongside the flat colon-style names.

### Two kinds of command

1. **CLI-local** — implemented inside the `nimbus` binary (`new`, `make:*`,
   `serve`, `deploy`, `login`).
2. **App-delegated** — the CLI shells out to `go run . <subcommand>` in your app
   root, because the work needs your models, migrations, and container. These
   only work if your app's `main.go` dispatches that argument.

App-delegated commands and the argument they pass:

| Command | Runs |
| --- | --- |
| `db:migrate` | `go run . migrate` |
| `db:create` | `go run . db:create` |
| `db:seed` | `go run . seed` |
| `migrate:fresh` | `go run . migrate:fresh` |
| `migrate:status` | `go run . migrate:status` |
| `route:list` | `go run . route:list` |
| `queue:work` | `go run . queue:work` |
| `schedule:run` | `go run . schedule:run` |
| `schedule:list` | `go run . schedule:list` |

> **Gotcha:** the default starter's `main.go` dispatches only `migrate`, `seed`,
> `schedule:run`, and `schedule:list`. Any other app-delegated command falls
> through to `bin.Boot()` + `app.Run()` and **starts the web server instead**.
> Add the missing `os.Args[1]` branches to `main.go` to enable them.

All app-delegated commands (plus `serve`, `install`, `build`) first check
`isNimbusApp(ctx.AppRoot)` and refuse to run outside a project root.

---

## Complete command index

56 registered commands, plus 3 nested aliases.

| Command | Aliases | Args | Group |
| --- | --- | --- | --- |
| `ai` | — | any | AI |
| `build` | — | 0 | Project |
| `db:create` | — | 0 | Database |
| `db:migrate` | — | 0 | Database |
| `db:rollback` | — | 0 | Database |
| `db:seed` | — | 0 | Database |
| `deploy` | `forge` | any | Deploy |
| `deploy:env` | — | any | Deploy |
| `deploy:init` | — | any | Deploy |
| `deploy:logs` | — | any | Deploy |
| `deploy:rollback` | — | any | Deploy |
| `deploy:status` | — | any | Deploy |
| `gen:client` | — | 0 | Codegen |
| `horizon:clear` | — | 0 | Queue |
| `horizon:forget` | — | any | Queue |
| `install` | — | 0 | Project |
| `key:generate` | `key:gen` | 0 | Maintenance |
| `livewire:layout` | — | 0 | Maintenance |
| `login` | `auth:login` | 0 | Cloud |
| `logout` | `auth:logout` | 0 | Cloud |
| `make:api-token` | — | 0 | Generator |
| `make:auth` | — | 0 | Generator |
| `make:command` | — | 1 | Generator |
| `make:controller` | — | 1 | Generator |
| `make:deploy-config` | — | 0 | Generator |
| `make:event` | — | 1 | Generator |
| `make:factory` | — | 1 | Generator |
| `make:job` | — | 1 | Generator |
| `make:lambda` | — | 0 | Generator |
| `make:listener` | — | 1 | Generator |
| `make:livewire` | — | 1 | Generator |
| `make:middleware` | — | 1 | Generator |
| `make:migration` | — | 1 | Generator |
| `make:model` | — | 1 | Generator |
| `make:notification` | — | 1 | Generator |
| `make:observer` | — | 1 | Generator |
| `make:plugin` | — | 1 | Generator |
| `make:policy` | — | 1 | Generator |
| `make:resource` | — | 1 | Generator |
| `make:rule` | — | 1 | Generator |
| `make:seeder` | — | 1 | Generator |
| `make:validator` | — | 1 | Generator |
| `migrate:fresh` | — | 0 | Database |
| `migrate:status` | — | 0 | Database |
| `new` | `create` | any | Project |
| `plugin:install` | — | 1 | Plugins |
| `plugin:list` | — | 0 | Plugins |
| `queue:work` | — | 0 | Queue |
| `release` | — | any | Maintenance |
| `repl` | — | 0 | Project |
| `route:list` | — | 0 | Database |
| `schedule:list` | — | 0 | Queue |
| `schedule:run` | — | 0 | Queue |
| `serve` | — | 0 | Project |
| `test:generate` | `test:gen`, `tg` | any | AI |
| `whoami` | `auth:status`, `auth:whoami`, `account` | 0 | Cloud |

Nested forms: `nimbus plugin install <name>`, `nimbus plugin list`,
`nimbus gen client [--out]`.

---

## Project lifecycle

### `nimbus new [name]` (alias `create`)

Scaffolds a new application. With no name it runs an interactive wizard.

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--kit` | string | `""` | Frontend kit: `react`, `vue`, `svelte` (Inertia) |
| `--starter` | string | `none` | Starter kit for Basic apps: `none`, `breeze`, `livewire`, `jetstream` |
| `--teams` | bool | `false` | Jetstream only — scaffold team features (requires `--starter=jetstream`) |
| `--no-default-plugins` | bool | `false` | Skip auto-registering the default plugins |
| `--lambda` | bool | `false` | Add an AWS Lambda deployment target (serverless) |

```bash
nimbus new blog
nimbus new shop --kit=react
nimbus new saas --starter=jetstream --teams
nimbus new api --lambda
```

### `nimbus install`

Runs `go mod tidy` in the app root. If an `inertia/` directory *and*
`package.json` exist, it also installs JS dependencies — `pnpm install` when
`pnpm` is on `PATH`, otherwise `npm install`. Refuses to run outside a project.

### `nimbus serve`

Starts the development server through [Air](https://github.com/air-verse/air)
(`go run github.com/air-verse/air@v1.52.3`) with `NIMBUS_SERVE=1` set.

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `-w`, `--watch` | bool | `true` | Watch for file changes and reload |

Behaviour:

- Writes/patches `.air.toml` on every run (`ensureAirConfig`). Existing configs
  are upgraded in place: `workspaces` is added to `exclude_dir`, and on Windows
  the binary is renamed to `tmp/main.exe` because Air's `cmd /c` runner cannot
  execute an extension-less file.
- Watches `.go` and `.nimbus` files; excludes `tmp`, `vendor`, `node_modules`,
  `public`, `workspaces`.
- Shows an animated `Compiling…` spinner while Air builds.
- For Inertia apps it runs `npm run build` once, then `npx vite` beside the
  server with `VITE_DEV=1`, HMR on `localhost:5173`.
- On ready, the app emits a `__NIMBUS_READY__` marker carrying the whole boot
  report and the CLI renders the startup view (logo, version, environment,
  database, providers, routes + compile time, view engine, Local/Network URLs,
  boot time). See `internal/startupview`.
- Ctrl-C kills the whole process group — Air gets 2s to interrupt the app and
  honour its 1s `kill_delay`; Vite gets 100ms.

### `nimbus build`

Copies `resources/css` → `public/css` and `resources/js` → `public/js`
recursively. Missing source directories are skipped silently. It does **not**
compile a Go binary — `nimbus serve`'s Air config runs `nimbus build && go build`.

### `nimbus repl`

Starts an interactive REPL session against the application container.

---

## Code generators (`make:*`)

Naming: the argument is converted with `cli.ToSnake` / `cli.ToPascal`, so
`make:model BlogPost`, `make:model blog_post`, and `make:model blogPost` all
produce `app/models/blog_post.go` with type `BlogPost`.

| Command | Arg | Writes |
| --- | --- | --- |
| `make:model <Name>` | 1 | `app/models/<snake>.go` |
| `make:controller <Name>` | 1 | `app/controllers/<snake>.go` |
| `make:middleware <Name>` | 1 | `app/middleware/<snake>.go` |
| `make:job <Name>` | 1 | `app/jobs/<snake>.go` |
| `make:validator <Name>` | 1 | `app/validators/<snake>.go` |
| `make:event <Name>` | 1 | `app/events/<snake>.go` |
| `make:listener <Name>` | 1 | `app/listeners/<snake>.go` |
| `make:notification <Name>` | 1 | `app/notifications/<snake>.go` |
| `make:policy <Name>` | 1 | `app/policies/<snake>.go` |
| `make:resource <Name>` | 1 | `app/resources/<snake>.go` |
| `make:observer <Name>` | 1 | `app/observers/<snake>.go` |
| `make:rule <Name>` | 1 | `app/rules/<snake>.go` |
| `make:command <name>` | 1 | `commands/<snake>.go` (colons in the name become underscores) |
| `make:plugin <Name>` | 1 | `app/plugins/<snake>/` — full skeleton |
| `make:migration <name>` | 1 | `database/migrations/<timestamp>_<name>.go` |
| `make:seeder <Name>` | 1 | `database/seeders/<snake>.go` |
| `make:factory <Name>` | 1 | `database/factories/<snake>_factory.go` |
| `make:livewire <Name>` | 1 | `app/livewire/<lower>.go` + `resources/views/{pages,components}/<name>.nimbus` |
| `make:auth` | 0 | Models, controllers, views and routes for session auth |
| `make:api-token` | 0 | `database/migrations/<ts>_create_api_tokens.go` + controller, registers in `registry.go` |
| `make:lambda` | 0 | `cmd/lambda/main.go`, `template.yaml`, `Makefile` |
| `make:deploy-config` | 0 | `deploy.yaml` in the app root |

**`make:migration` flag**

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--auto-detect` | bool | `false` | When the name is `fix_y2038`, scan MySQL `TIMESTAMP` columns and generate a fix migration |

Migrations and API-token scaffolds print a reminder to add the generated struct
to `database/migrations/registry.go` — generators do not edit the registry for
ordinary migrations.

---

## Database

All of these are app-delegated (see the table above) and must run from the app
root.

| Command | Purpose |
| --- | --- |
| `db:create` | Create the database from the configured driver/DSN |
| `db:migrate` | Run pending migrations |
| `db:rollback` | **Prints instructions only.** It does not roll anything back — it tells you to call `migrator.Down()` from your app (e.g. `go run . rollback`) |
| `db:seed` | Run the registered seeders |
| `migrate:fresh` | Drop every table and re-run all migrations |
| `migrate:status` | Print each migration and whether it has run |
| `route:list` | Print the registered route table |

The underlying engine is `database.Migrator` (`database/migrate.go`) with
`Up()`, `Down()`, `Fresh()`, `Status()`, and `PrintStatus()`. Migrations are
tracked in a `schema_migrations` table with batch numbers, and DDL runs inside a
transaction on dialects that support transactional DDL.

---

## Queue & scheduler

| Command | Purpose |
| --- | --- |
| `queue:work` | Start a worker processing jobs (app-delegated) |
| `schedule:run` | Start the scheduler; blocks (app-delegated) |
| `schedule:list` | List registered scheduled tasks (app-delegated) |

### `horizon:clear`

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--queue` | string | `default` | Name of the queue to clear |

### `horizon:forget`

Forget completed or failed jobs.

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--all` | bool | `false` | Forget all jobs rather than a specific id |

---

## Plugins

### `plugin:install <name>` / `nimbus plugin install <name>`

Installs a plugin and patches the project for it. Each plugin is described by a
`PluginDef` (`cli/commands/plugin.go`) carrying its import path, alias, the
insert for `bin/server.go`, kernel imports/inserts, `.env` variables, and any
config files to scaffold. Installing therefore edits `bin/server.go`,
`config/config.go`, `.env.example`, and writes the plugin's config files.

### `plugin:list` / `nimbus plugin list`

Lists available plugins and marks which are installed.

---

## Codegen — Nimbus Hive

### `gen:client` / `nimbus gen client`

Generates the TypeScript client manifest for Nimbus Hive.

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--out` | string | `.nimbus-client` | Output directory (nested `gen client` form only) |

Related: setting `NIMBUS_DUMP_ROUTES=1` makes the app write the route manifest
(`router.ManifestFileName`) and exit without serving — this is how the CLI
harvests routes before generating.

---

## Deploy — Nimbus Forge

### `nimbus deploy` (alias `forge`)

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--target` | string | `""` | `fly`, `railway`, `docker`, `render`, `aws`, `gcp`, `netlify` |
| `--region` | string | `""` | Deployment region |
| `--app` | string | `""` | Application name |
| `--env` | string | `production` | Environment (`production`, `staging`) |
| `--tag` | string | `""` | Docker image tag (defaults to the git SHA) |
| `--skip-build` | bool | `false` | Skip the build step |
| `--dry-run` | bool | `false` | Show what would be deployed without deploying |

### `nimbus deploy:init`

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--target` | string | `""` | `fly`, `railway`, `docker`, `render`, `netlify` |

### `nimbus deploy:env`

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--set` | string | `""` | Set a variable: `KEY=VALUE` |
| `--unset` | string | `""` | Unset a variable: `KEY` |
| `--list` | bool | `false` | List all variables |

### `nimbus deploy:status` / `deploy:logs` / `deploy:rollback`

Check the current deployment, stream its logs, and roll back to the previous
release. No flags.

Generated artefacts: `make:deploy-config` writes `deploy.yaml`; the Dockerfile
template lives in `cli/commands/deploy.go` and produces a multi-stage build on
`alpine:3.19` running as a non-root `nimbus` user with a `HEALTHCHECK`.

---

## Nimbus Cloud account

| Command | Aliases | Purpose |
| --- | --- | --- |
| `login` | `auth:login` | Browser OAuth against `https://nimbusgo.space` |
| `logout` | `auth:logout` | Clear saved credentials |
| `whoami` | `auth:status`, `auth:whoami`, `account` | Show account, email, subscription tier |

**`login` flag**

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--server` | string | `""` | Custom Nimbus Cloud server URL (default `https://nimbusgo.space`) |

---

## AI

### `nimbus ai [prompt]`

AI Copilot — plans and executes architectural code changes, powered by
nimbusgo.space. With no prompt it opens the interactive TUI; with a prompt it
runs a single turn and then drops into chat.

| Flag | Short | Type | Default | Meaning |
| --- | --- | --- | --- | --- |
| `--model` | `-m` | string | `""` | AI model (default: optimal on Nimbus Cloud, or GPT-4o / Sonnet) |
| `--server` | | string | `""` | Nimbus Cloud server URL |
| `--resume` | | string | `""` | Resume a saved session id (`.nimbus/ai-sessions/<id>.json`) |
| `--dry-run` | | bool | `false` | Show diffs and plans without writing to disk |
| `--yes` | | bool | `false` | Approve flagged actions automatically (CI / non-interactive) |
| `--plan-only` | | bool | `false` | Stop after generating and reviewing the plan |
| `--permission-mode` | | string | `""` | `auto` (assess each action, ask about risky ones), `ask` (confirm every change), `allow` (run anything not refused). Defaults to the `permission_mode` setting |

**Slash commands** (`internal/ai/tui/commands.go`) — type `/` to open the menu:

| Command | Aliases | Does |
| --- | --- | --- |
| `/help` | `help`, `?` | Commands and keyboard shortcuts |
| `/context` | | What the agent knows about this project |
| `/compact` | | Summarise earlier turns to free up context |
| `/skills` | | Agent skills available for this request |
| `/settings` | `/config` | Change and save how Nimbus AI behaves |
| `/session` | | Session id, memory, and how to resume it |
| `/clear` | `clear` | Clear the transcript on screen |
| `/exit` | `/quit`, `exit`, `quit`, `q` | Quit Nimbus AI |

**Settings layering** (`internal/ai/settings.go`) — later files win:

```
~/.nimbus/settings.json               user, across every project
<app>/.nimbus/settings.json           project, usually committed
<app>/.nimbus/settings.local.json     this checkout, usually gitignored
```

### `nimbus test:generate` (aliases `test:gen`, `tg`)

Generates tests from controllers and handlers by static AST analysis — fully
offline, no model call.

| Flag | Short | Type | Default | Meaning |
| --- | --- | --- | --- | --- |
| `--controller` | `-c` | string | `""` | Specific controller file to generate tests for |
| `--output` | `-o` | string | `""` | Output directory (default: alongside the source with a `_test.go` suffix) |
| `--all` | `-a` | bool | `false` | Generate tests for all controllers |

---

## Maintenance

### `nimbus key:generate` (alias `key:gen`)

Generates an `APP_KEY` and writes it into `.env`.

| Flag | Short | Type | Default | Meaning |
| --- | --- | --- | --- | --- |
| `--show` | | bool | `false` | Print the generated key without modifying `.env` |
| `--force` | `-f` | bool | `false` | Overwrite an existing `APP_KEY` without confirming |

### `nimbus livewire:layout`

Writes the default Livewire layout to `resources/views/layouts/app.nimbus`.

### `nimbus release`

Framework-maintainer command that cuts a new Nimbus release and updates
`internal/version.Nimbus`. Not intended for application developers.

---

## Writing your own commands

`nimbus make:command Greet` writes `commands/greet.go`. Register it from the
app so it appears in `nimbus --help`:

```go
package commands

import "github.com/CodeSyncr/nimbus/cli"

func init() { cli.RegisterCommand(&GreetCommand{}) }

type GreetCommand struct{ loud bool }

func (c *GreetCommand) Name() string        { return "greet" }
func (c *GreetCommand) Description() string { return "Greet somebody" }
func (c *GreetCommand) Args() int           { return 1 }
func (c *GreetCommand) Aliases() []string   { return []string{"hi"} }

func (c *GreetCommand) Flags(cmd *cobra.Command) {
    cmd.Flags().BoolVar(&c.loud, "loud", false, "Shout the greeting")
}

func (c *GreetCommand) Run(ctx *cli.Context) error {
    name := ctx.Args[0]
    if c.loud {
        name = strings.ToUpper(name)
    }
    ctx.UI.Successf("Hello, %s!", name)
    return nil
}
```

`ctx` (`cli.Context`) carries `Args`, `AppRoot`, `Stdin`/`Stdout`/`Stderr`, and
`UI` with `Infof`, `Successf`, `Warnf`, `Errorf`.

---

## Best practices

1. Prefer generators over hand-written boilerplate — they keep naming and
   registry conventions consistent.
2. Run `nimbus serve` during development; the startup view tells you at a glance
   which environment, database, and plugin set you actually booted.
3. Keep `.air.toml` in version control — the CLI patches it forward, so an old
   config self-upgrades rather than breaking.
4. Use `--dry-run` on `deploy` and `ai` before letting either write anything.
5. If an app-delegated command silently starts the web server, your `main.go` is
   missing that `os.Args[1]` branch.
