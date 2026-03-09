// Package main is the Nimbus CLI (Cobra-based, AdonisJS Ace-style).
package main

import (
	"bufio"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"text/template"
	"time"

	"github.com/CodeSyncr/nimbus/internal/deploy"
	"github.com/CodeSyncr/nimbus/internal/repl"
	"github.com/CodeSyncr/nimbus/internal/release"
	"github.com/CodeSyncr/nimbus/internal/version"
	"github.com/spf13/cobra"
)

//go:embed templates/views/*.nimbus
var viewTemplates embed.FS

var rootCmd = &cobra.Command{
	Use:   "nimbus",
	Short: "Nimbus - Laravel-style framework for Go",
}

var newCmd = &cobra.Command{
	Use:     "new [app-name]",
	Aliases: []string{"create"},
	Short:   "Create a new Nimbus application",
	Long: `Create a new Nimbus application.

  nimbus new my-app                    Create a Nimbus app with default plugins (drive, redis, transmit)
  nimbus new my-app --no-default-plugins  Create a minimal app without default plugins
  nimbus new my-app --kit=react        Create an Inertia app with React
  nimbus new my-app --kit=vue          Create an Inertia app with Vue
  nimbus new my-app --kit=svelte       Create an Inertia app with Svelte`,
	Args: cobra.ExactArgs(1),
	RunE: runNew,
}

var (
	serveCmd = &cobra.Command{
		Use:   "serve",
		Short: "Run the application (AdonisJS ace serve style; run from app root)",
		RunE:  runServe,
	}
	serveWatch bool
)

func init() {
	serveCmd.Flags().BoolVarP(&serveWatch, "watch", "w", false, "Watch inertia/ files and rebuild on change (Inertia apps only)")
}

var makeModelCmd = &cobra.Command{
	Use:   "make:model [name]",
	Short: "Create a new model",
	Args:  cobra.ExactArgs(1),
	RunE:  runMakeModel,
}

var makeMigrationCmd = &cobra.Command{
	Use:   "make:migration [name]",
	Short: "Create a new migration",
	Args:  cobra.ExactArgs(1),
	RunE:  runMakeMigration,
}

var makeControllerCmd = &cobra.Command{
	Use:   "make:controller [name]",
	Short: "Create a new controller",
	Args:  cobra.ExactArgs(1),
	RunE:  runMakeController,
}

var makeMiddlewareCmd = &cobra.Command{
	Use:   "make:middleware [name]",
	Short: "Create a new middleware",
	Args:  cobra.ExactArgs(1),
	RunE:  runMakeMiddleware,
}

var dbMigrateCmd = &cobra.Command{
	Use:   "db:migrate",
	Short: "Run database migrations",
	RunE:  runDbMigrate,
}

var dbRollbackCmd = &cobra.Command{
	Use:   "db:rollback",
	Short: "Rollback the last migration",
	RunE:  runDbRollback,
}

var queueWorkCmd = &cobra.Command{
	Use:   "queue:work",
	Short: "Run the queue worker (processes background jobs)",
	Long:  "Starts a worker that processes jobs from the queue. Requires the queue plugin and Redis (or sync for dev).",
	RunE:  runQueueWork,
}

var makeJobCmd = &cobra.Command{
	Use:   "make:job [name]",
	Short: "Create a new queue job",
	Args:  cobra.ExactArgs(1),
	RunE:  runMakeJob,
}

var makeSeederCmd = &cobra.Command{
	Use:   "make:seeder [name]",
	Short: "Create a new database seeder",
	Args:  cobra.ExactArgs(1),
	RunE:  runMakeSeeder,
}

var makePluginCmd = &cobra.Command{
	Use:   "make:plugin [name]",
	Short: "Create a new plugin",
	Args:  cobra.ExactArgs(1),
	RunE:  runMakePlugin,
}

var pluginCmd = &cobra.Command{
	Use:   "plugin",
	Short: "Manage Nimbus plugins",
}

var pluginInstallCmd = &cobra.Command{
	Use:   "install [name]",
	Short: "Install and configure a plugin in the current project",
	Long:  "Installs a plugin via go get and configures bin/server.go and start/kernel.go as needed.",
	Args:  cobra.ExactArgs(1),
	RunE:  runPluginInstall,
}

var pluginListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available plugins",
	RunE:  runPluginList,
}

var makeValidatorCmd = &cobra.Command{
	Use:   "make:validator [name]",
	Short: "Create a new validator struct",
	Args:  cobra.ExactArgs(1),
	RunE:  runMakeValidator,
}

var makeCommandCmd = &cobra.Command{
	Use:   "make:command [name]",
	Short: "Create a new custom command (AdonisJS Ace style)",
	Long:  "Creates a command in commands/ with standard metadata and run logic. Use namespaces with colons, e.g. make:command make:controller.",
	Args:  cobra.ExactArgs(1),
	RunE:  runMakeCommand,
}

var replCmd = &cobra.Command{
	Use:   "repl",
	Short: "Start an interactive Go REPL",
	Long:  "Starts an interactive Go REPL in the current directory. Use :quit or Ctrl-D to exit. Run from your app root to evaluate code in your project's module context.",
	RunE:  runRepl,
}

var makeDeployConfigCmd = &cobra.Command{
	Use:   "make:deploy-config",
	Short: "Create deploy.yaml for nimbus deploy",
	Long:  "Creates deploy.yaml with target (fly, railway, aws, docker), migrations, and secrets config.",
	RunE:  runMakeDeployConfig,
}

var releaseCmd = &cobra.Command{
	Use:   "release [patch|minor|major]",
	Short: "Create next Nimbus release (run from nimbus repo)",
	Long: `Bump version, update templates, and create git tag.

  nimbus release patch   v0.1.1 -> v0.1.2
  nimbus release minor   v0.1.1 -> v0.2.0
  nimbus release major   v0.1.1 -> v1.0.0

Must be run from the Nimbus framework repo (github.com/CodeSyncr/nimbus).`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRelease,
}

var deployCmd = &cobra.Command{
	Use:   "deploy [target]",
	Short: "One-command production deploy",
	Long: `Deploy your Nimbus app to production.

  nimbus deploy fly       Deploy to Fly.io (requires flyctl)
  nimbus deploy railway   Deploy to Railway (requires Railway CLI)
  nimbus deploy aws       Build for AWS (push to ECR, deploy to ECS/App Runner)
  nimbus deploy docker    Build Docker image locally (for K8s or custom deploy)

Handles: Docker build, migrations (release command), workers, secrets.
Config: deploy.yaml in app root. Creates Dockerfile and fly.toml if missing.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runDeploy,
}

var newNoDefaults bool

func init() {
	newCmd.Flags().String("kit", "", "Frontend kit: react, vue, svelte (empty = standard Nimbus app)")
	newCmd.Flags().BoolVar(&newNoDefaults, "no-default-plugins", false, "Skip auto-registering default plugins (drive, redis, transmit, etc.)")
}

func main() {
	pluginCmd.AddCommand(pluginInstallCmd, pluginListCmd)
	rootCmd.AddCommand(newCmd, serveCmd, makeModelCmd, makeMigrationCmd, makeControllerCmd, makeMiddlewareCmd, makeJobCmd, makeSeederCmd, makePluginCmd, makeValidatorCmd, makeCommandCmd, makeDeployConfigCmd, dbMigrateCmd, dbRollbackCmd, queueWorkCmd, replCmd, deployCmd, releaseCmd, pluginCmd)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runNew(cmd *cobra.Command, args []string) error {
	name := args[0]
	kit, _ := cmd.Flags().GetString("kit")
	kit = strings.ToLower(strings.TrimSpace(kit))

	if kit != "" && kit != "react" && kit != "vue" && kit != "svelte" {
		return fmt.Errorf("invalid --kit=%q: must be react, vue, svelte, or empty", kit)
	}

	dir := name
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	baseDirs := []string{
		filepath.Join(dir, "app", "controllers"),
		filepath.Join(dir, "app", "models"),
		filepath.Join(dir, "app", "middleware"),
		filepath.Join(dir, "app", "jobs"),
		filepath.Join(dir, "bin"),
		filepath.Join(dir, "config"),
		filepath.Join(dir, "database", "migrations"),
		filepath.Join(dir, "database", "seeders"),
		filepath.Join(dir, "start"),
		filepath.Join(dir, "public"),
	}
	if kit == "" {
		baseDirs = append(baseDirs, filepath.Join(dir, "views"))
	} else {
		baseDirs = append(baseDirs,
			filepath.Join(dir, "resources", "views"),
			filepath.Join(dir, "inertia", "pages", "home"),
			filepath.Join(dir, "inertia", "layouts"),
			filepath.Join(dir, "inertia", "Pages", "Home"),
		)
	}
	for _, d := range baseDirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return err
		}
	}

	mod := goModContent(name, kit)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0644); err != nil {
		return err
	}

	t := template.Must(template.New("main").Parse(mainTmpl))
	f, _ := os.Create(filepath.Join(dir, "main.go"))
	_ = t.Execute(f, map[string]string{"AppName": name})
	_ = f.Close()

	if kit == "" {
		var serverContent string
		if newNoDefaults {
			ts := template.Must(template.New("server").Parse(binServerTmpl))
			var buf strings.Builder
			_ = ts.Execute(&buf, map[string]string{"AppName": name})
			serverContent = buf.String()
		} else {
			serverContent = buildDefaultServerContent(name)
		}
		_ = os.WriteFile(filepath.Join(dir, "bin", "server.go"), []byte(serverContent), 0644)

		tr := template.Must(template.New("routes").Parse(routesStub))
		rf, _ := os.Create(filepath.Join(dir, "start", "routes.go"))
		_ = tr.Execute(rf, map[string]string{"AppName": name})
		_ = rf.Close()

		if err := copyViewTemplates(dir); err != nil {
			return err
		}
	} else {
		ts := template.Must(template.New("server").Parse(binServerInertiaTmpl))
		sf, _ := os.Create(filepath.Join(dir, "bin", "server.go"))
		_ = ts.Execute(sf, map[string]string{"AppName": name})
		_ = sf.Close()

		tr := template.Must(template.New("routes").Parse(routesInertiaStub))
		rf, _ := os.Create(filepath.Join(dir, "start", "routes.go"))
		_ = tr.Execute(rf, map[string]string{"AppName": name})
		_ = rf.Close()

		if err := createInertiaKit(dir, name, kit); err != nil {
			return err
		}
	}

	_ = os.WriteFile(filepath.Join(dir, "start", "kernel.go"), []byte(kernelStub), 0644)
	_ = createStartJobsGo(filepath.Join(dir, "start", "jobs.go"), name)
	_ = os.WriteFile(filepath.Join(dir, "config", "config.go"), []byte(configLoader), 0644)
	_ = os.WriteFile(filepath.Join(dir, "config", "app.go"), []byte(configApp), 0644)
	_ = os.WriteFile(filepath.Join(dir, "config", "database.go"), []byte(configDatabase), 0644)

	te := template.Must(template.New("env").Parse(envExample))
	ef, _ := os.Create(filepath.Join(dir, ".env.example"))
	_ = te.Execute(ef, map[string]string{"AppName": name})
	_ = ef.Close()
	if kit != "" {
		envEx, _ := os.ReadFile(filepath.Join(dir, ".env.example"))
		_ = os.WriteFile(filepath.Join(dir, ".env.example"), append(envEx, []byte("\nVITE_APP_NAME="+name+"\n")...), 0644)
	} else if !newNoDefaults {
		// Append default plugin env vars (drive, redis, etc.)
		if defEnv := defaultEnvVars(name); defEnv != "" {
			envEx, _ := os.ReadFile(filepath.Join(dir, ".env.example"))
			_ = os.WriteFile(filepath.Join(dir, ".env.example"), append(envEx, []byte(defEnv)...), 0644)
		}
	}

	envContent := "PORT=3333\nAPP_ENV=development\nAPP_NAME=" + name + "\nDB_DRIVER=sqlite\nDB_DSN=database.sqlite\nQUEUE_DRIVER=sync\n"
	if kit != "" {
		envContent += "VITE_APP_NAME=" + name + "\n"
	} else if !newNoDefaults {
		envContent += "REDIS_URL=redis://localhost:6379\n"
	}
	_ = os.WriteFile(filepath.Join(dir, ".env"), []byte(envContent), 0644)
	_ = os.WriteFile(filepath.Join(dir, ".air.toml"), []byte(airConfigTmpl), 0644)

	gitignore := ".env\n*.sqlite\ntmp/\n"
	if kit != "" {
		gitignore += "node_modules/\npublic/build/\n"
	}
	_ = os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(gitignore), 0644)

	_ = os.WriteFile(filepath.Join(dir, "database", "migrations", "registry.go"), []byte(migrationsRegistryStub), 0644)

	fmt.Printf("Created Nimbus app %q in ./%s\n", name, dir)
	if kit == "" {
		fmt.Println("Next: cd " + dir + " && go mod tidy && nimbus serve")
		fmt.Println("      (go mod tidy fetches air for hot reload; nimbus serve starts the app with live reload)")
	} else {
		fmt.Println("Next: cd " + dir + " && go mod tidy && npm install && nimbus serve")
		fmt.Println("      (nimbus serve runs npm run build automatically)")
	}
	return nil
}

func goModContent(name, kit string) string {
	mod := `module ` + name + `

go 1.21

require (
	github.com/CodeSyncr/nimbus ` + version.Nimbus + `
	github.com/joho/godotenv v1.5.1
	github.com/air-verse/air v1.52.3
)

replace github.com/CodeSyncr/nimbus => ../
`
	if kit != "" {
		mod = `module ` + name + `

go 1.21

require (
	github.com/CodeSyncr/nimbus ` + version.Nimbus + `
	github.com/joho/godotenv v1.5.1
	github.com/air-verse/air v1.52.3
	github.com/petaki/inertia-go v1.6.0
)

replace github.com/CodeSyncr/nimbus => ../
`
	}
	return mod
}

func createInertiaKit(dir, name, kit string) error {
	scriptPath := "/build/assets/app.js"
	layoutContent := strings.Replace(inertiaLayoutNimbus, "{{SCRIPT_SRC}}", scriptPath, 1)
	_ = os.WriteFile(filepath.Join(dir, "resources", "views", "inertia_layout.nimbus"), []byte(layoutContent), 0644)

	switch kit {
	case "react":
		_ = os.WriteFile(filepath.Join(dir, "inertia", "app.tsx"), []byte(inertiaAppReact), 0644)
		_ = os.WriteFile(filepath.Join(dir, "inertia", "layouts", "default.tsx"), []byte(inertiaLayoutDefault), 0644)
		_ = os.WriteFile(filepath.Join(dir, "inertia", "pages", "home", "index.tsx"), []byte(inertiaPageHomeReact), 0644)
	case "vue":
		_ = os.WriteFile(filepath.Join(dir, "inertia", "app.js"), []byte(inertiaAppVue), 0644)
		_ = os.WriteFile(filepath.Join(dir, "inertia", "pages", "home", "index.vue"), []byte(inertiaPageHomeVue), 0644)
	case "svelte":
		_ = os.WriteFile(filepath.Join(dir, "inertia", "app.js"), []byte(inertiaAppSvelte), 0644)
		_ = os.WriteFile(filepath.Join(dir, "inertia", "pages", "home", "index.svelte"), []byte(inertiaPageHomeSvelte), 0644)
	}

	_ = os.WriteFile(filepath.Join(dir, "package.json"), []byte(inertiaPackageJSON(kit)), 0644)
	_ = os.WriteFile(filepath.Join(dir, "vite.config.js"), []byte(inertiaViteConfig(kit)), 0644)
	_ = os.WriteFile(filepath.Join(dir, "index.html"), []byte(inertiaIndexHTML(kit)), 0644)
	if kit == "react" {
		_ = os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(inertiaTsconfig), 0644)
		_ = os.WriteFile(filepath.Join(dir, "tsconfig.node.json"), []byte(inertiaTsconfigNode), 0644)
		_ = os.WriteFile(filepath.Join(dir, "inertia", "tsconfig.json"), []byte(inertiaTsconfigInertia), 0644)
		_ = os.WriteFile(filepath.Join(dir, "inertia", "types.ts"), []byte(inertiaTypesTS), 0644)
	}
	return nil
}

// ── main.go ────────────────────────────────────────────────────
const mainTmpl = `/*
|--------------------------------------------------------------------------
| Nimbus Application Entry Point
|--------------------------------------------------------------------------
|
| DO NOT MODIFY THIS FILE — it is the bootstrap entrypoint for the
| Nimbus application.
|
| Configuration  → config/
| Middleware      → start/kernel.go
| Routes          → start/routes.go
| Server boot     → bin/server.go
|
| Run migrations: go run . migrate  (or: nimbus db:migrate)
|
*/

package main

import (
	"os"

	"{{.AppName}}/bin"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		bin.RunMigrations()
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "queue:work" {
		bin.RunQueueWorker()
		return
	}
	app := bin.Boot()
	_ = app.Run()
}
`

// ── bin/server.go ──────────────────────────────────────────────
const binServerTmpl = `/*
|--------------------------------------------------------------------------
| HTTP Server
|--------------------------------------------------------------------------
|
| This file boots the Nimbus application: it loads environment
| variables, connects to the database, registers middleware via the
| kernel, and wires up routes.
|
| Run migrations: go run . migrate  (or: nimbus db:migrate)
|
*/

package bin

import (
	"context"
	"fmt"
	"os"

	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/cache"
	"github.com/CodeSyncr/nimbus/database"
	"github.com/CodeSyncr/nimbus/queue"

	"{{.AppName}}/config"
	"{{.AppName}}/database/migrations"
	"{{.AppName}}/start"
)

func Boot() *nimbus.App {
	config.Load()

	app := nimbus.New()

	start.RegisterMiddleware(app)

	start.RegisterRoutes(app)

	cache.Boot(nil)

	_, _ = database.Connect(config.Database.Driver, config.Database.DSN)

	queue.Boot(&queue.BootConfig{RegisterJobs: start.RegisterQueueJobs})

	return app
}

// RunQueueWorker runs the queue worker. Called when main is invoked with "queue:work" arg.
func RunQueueWorker() {
	app := Boot()
	if err := app.Boot(); err != nil {
		fmt.Fprintf(os.Stderr, "Boot failed: %v\n", err)
		os.Exit(1)
	}
	ctx := context.Background()
	queue.RunWorker(ctx, "default")
}

// RunMigrations runs database migrations. Called when main is invoked with "migrate" arg.
func RunMigrations() {
	config.Load()
	db, err := database.Connect(config.Database.Driver, config.Database.DSN)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Database connection failed: %%v\n", err)
		os.Exit(1)
	}
	migrator := database.NewMigrator(db, migrations.All())
	if err := migrator.Up(); err != nil {
		fmt.Fprintf(os.Stderr, "Migration failed: %%v\n", err)
		os.Exit(1)
	}
	fmt.Println("Migrations completed.")
}
`

// ── start/kernel.go ────────────────────────────────────────────
const kernelStub = `/*
|--------------------------------------------------------------------------
| HTTP Kernel
|--------------------------------------------------------------------------
|
| The HTTP kernel file is used to register the middleware with the
| server or the router.
|
*/

package start

import (
	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/middleware"
	"github.com/CodeSyncr/nimbus/router"
)

func RegisterMiddleware(app *nimbus.App) {

	// Server Middleware
	// Runs on every HTTP request, even if there is no route
	// registered for the request URL.
	app.Router.Use(
		middleware.Logger(),
		middleware.Recover(),
	)

	// Router Middleware
	// Runs on all HTTP requests with a registered route.
	// app.Router.Use(
	//     middleware.CORS("*"),
	//     middleware.CSRF(middleware.NewMemoryCSRFStore()),
	// )
}

// Named Middleware
// Must be explicitly assigned to routes or route groups.
var Middleware = map[string]router.Middleware{
	// "auth":  middleware.RequireAuth(),
	// "guest": guestMiddleware(),
}
`

// ── start/routes.go ────────────────────────────────────────────
const routesStub = `/*
|--------------------------------------------------------------------------
| Routes
|--------------------------------------------------------------------------
|
| This file defines all HTTP routes for the application. Register
| your controllers, resource routes, and page handlers here.
|
*/

package start

import (
	"net/http"

	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/context"
)

func RegisterRoutes(app *nimbus.App) {
	app.Router.Get("/", homeHandler)
	app.Router.Get("/health", healthHandler)
}

func homeHandler(c *context.Context) error {
	return c.View("home", map[string]any{
		"title":   "Welcome",
		"appName": "Nimbus",
		"tagline": "AdonisJS-style framework for Go",
	})
}

func healthHandler(c *context.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}
`

// ── Inertia kit: bin/server.go ─────────────────────────────────
const binServerInertiaTmpl = `/*
|--------------------------------------------------------------------------
| HTTP Server (Inertia)
|--------------------------------------------------------------------------
|
| Boots the Nimbus application with Inertia.js for Vue/React/Svelte.
|
*/

package bin

import (
	"context"
	"fmt"
	"os"

	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/cache"
	"github.com/CodeSyncr/nimbus/database"
	"github.com/CodeSyncr/nimbus/plugins/inertia"
	"github.com/CodeSyncr/nimbus/queue"

	"{{.AppName}}/config"
	"{{.AppName}}/database/migrations"
	"{{.AppName}}/start"
)

func Boot() *nimbus.App {
	config.Load()

	app := nimbus.New()

	app.Use(inertia.New(inertia.Config{
		URL:          "http://localhost:3333",
		RootTemplate: "resources/views/inertia_layout.nimbus",
		Version:      "1",
	}))

	start.RegisterMiddleware(app)
	start.RegisterRoutes(app)

	cache.Boot(nil)

	_, _ = database.Connect(config.Database.Driver, config.Database.DSN)

	queue.Boot(&queue.BootConfig{RegisterJobs: start.RegisterQueueJobs})

	return app
}

func RunQueueWorker() {
	app := Boot()
	if err := app.Boot(); err != nil {
		fmt.Fprintf(os.Stderr, "Boot failed: %v\n", err)
		os.Exit(1)
	}
	queue.RunWorker(context.Background(), "default")
}

func RunMigrations() {
	config.Load()
	db, err := database.Connect(config.Database.Driver, config.Database.DSN)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Database connection failed: %%v\n", err)
		os.Exit(1)
	}
	migrator := database.NewMigrator(db, migrations.All())
	if err := migrator.Up(); err != nil {
		fmt.Fprintf(os.Stderr, "Migration failed: %%v\n", err)
		os.Exit(1)
	}
	fmt.Println("Migrations completed.")
}
`

// ── Inertia kit: start/routes.go ────────────────────────────────
const routesInertiaStub = `/*
|--------------------------------------------------------------------------
| Routes (Inertia)
|--------------------------------------------------------------------------
*/

package start

import (
	"net/http"

	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/context"
	"github.com/CodeSyncr/nimbus/plugins/inertia"
)

func RegisterRoutes(app *nimbus.App) {
	app.Router.Get("/build/*", buildAssetsHandler)
	app.Router.Get("/", homeHandler)
	app.Router.Get("/health", healthHandler)
}

func buildAssetsHandler(c *context.Context) error {
	fs := http.StripPrefix("/build", http.FileServer(http.Dir("public/build")))
	fs.ServeHTTP(c.Response, c.Request)
	return nil
}

func homeHandler(c *context.Context) error {
	return inertia.Render(c, "home/index", map[string]any{
		"title":   "Welcome",
		"appName": "Nimbus",
		"tagline": "Inertia.js with Go",
	})
}

func healthHandler(c *context.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}
`

// ── Inertia kit: resources/views/inertia_layout.nimbus ───────────
// AdonisJS-style layout. With nimbus serve -w: Vite dev server (HMR). Else: built assets.
const inertiaLayoutNimbus = `<!DOCTYPE html>
<html>
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <link rel="icon" href="data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 100 100'><text y='.9em' font-size='90'>⚡</text></svg>" />
    <title inertia>Nimbus</title>

    <!-- @viteReactRefresh() @vite(['inertia/app.tsx']) -->
    {{ if .viteDev }}
    <script type="module">
      import RefreshRuntime from 'http://localhost:5173/@react-refresh'
      RefreshRuntime.injectIntoGlobalHook(window)
      window.$RefreshReg$ = () => {}
      window.$RefreshSig$ = () => (type) => type
      window.__vite_plugin_react_preamble_installed__ = true
    </script>
    <script type="module" src="http://localhost:5173/@vite/client"></script>
    <script type="module" src="http://localhost:5173/inertia/app.tsx"></script>
    {{ else }}
    <link rel="stylesheet" href="/build/assets/app.css" />
    <script type="module" src="{{SCRIPT_SRC}}"></script>
    {{ end }}
    <!-- @inertiaHead() -->
    <!-- @stack('dumper') -->
  </head>

  <body>
    <div id="app" data-page="{{ marshal .page }}"></div>
  </body>

</html>
`

// ── Inertia kit: inertia/app.tsx (React) ─────────────────────────
const inertiaAppReact = `import type { ComponentType } from 'react'
import { createRoot } from 'react-dom/client'
import { createInertiaApp } from '@inertiajs/react'
import Layout from '~/layouts/default'

const appName = import.meta.env.VITE_APP_NAME || 'Nimbus'

createInertiaApp({
  title: (title) => (title ? title + ' - ' + appName : appName),
  resolve: (name) => {
    const pages = import.meta.glob<{ default: ComponentType }>('./pages/**/*.tsx')
    const path = './pages/' + name.replace(/\./g, '/') + '.tsx'
    const key = path in pages ? path : Object.keys(pages).find((k) => k.toLowerCase() === path.toLowerCase())
    const loader = key ? pages[key] : undefined
    if (!loader || typeof loader !== 'function') {
      throw new Error('Inertia page not found: ' + name + ' (resolved: ' + path + ')')
    }
    return (loader as () => Promise<{ default: ComponentType }>)().then((mod) => {
      const Page = mod.default
      return { default: (props) => <Layout><Page {...props} /></Layout> }
    })
  },
  setup({ el, App, props }) {
    createRoot(el).render(<App {...props} />)
  },
  progress: {
    color: '#4B5563',
  },
})
`

// ── Inertia kit: resources/inertia/layouts/default.tsx ─────────────
const inertiaLayoutDefault = `import { ReactNode } from 'react'

interface Props {
  children: ReactNode
}

export default function Layout({ children }: Props) {
  return (
    <div className="min-h-screen bg-gray-50">
      {children}
    </div>
  )
}
`

// ── Inertia kit: resources/inertia/pages/home/index.tsx ────────────
const inertiaPageHomeReact = `interface Props {
  title?: string
  appName?: string
  tagline?: string
}

export default function Index({ title, appName, tagline }: Props) {
  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-100">
      <div className="text-center">
        <h1 className="text-4xl font-bold text-gray-800">{title}</h1>
        <p className="mt-2 text-xl text-gray-600">{appName}</p>
        <p className="mt-1 text-gray-500">{tagline}</p>
      </div>
    </div>
  )
}
`

// ── Inertia kit: inertia/app.js (Vue) ────────────────────────────────
const inertiaAppVue = `import { createApp } from 'vue'
import { createInertiaApp } from '@inertiajs/vue3'

createInertiaApp({
  resolve: (name) => {
    const pages = import.meta.glob('./pages/**/*.vue')
    const path = './pages/' + name.replace(/\./g, '/') + '.vue'
    const key = path in pages ? path : Object.keys(pages).find((k) => k.toLowerCase() === path.toLowerCase())
    const loader = key ? pages[key] : undefined
    if (!loader || typeof loader !== 'function') {
      throw new Error('Inertia page not found: ' + name + ' (resolved: ' + path + ')')
    }
    return loader()
  },
  setup({ el, App, props, plugin }) {
    createApp(App).use(plugin).mount(el)
  },
})
`

// ── Inertia kit: resources/js/Pages/Home/Index.vue ───────────────
const inertiaPageHomeVue = `<template>
  <div class="min-h-screen flex items-center justify-center bg-gray-100">
    <div class="text-center">
      <h1 class="text-4xl font-bold text-gray-800">{{ title }}</h1>
      <p class="mt-2 text-xl text-gray-600">{{ appName }}</p>
      <p class="mt-1 text-gray-500">{{ tagline }}</p>
    </div>
  </div>
</template>

<script setup>
defineProps(['title', 'appName', 'tagline'])
</script>
`

// ── Inertia kit: inertia/app.js (Svelte) ─────────────────────────────
const inertiaAppSvelte = `import { createInertiaApp } from '@inertiajs/svelte'

createInertiaApp({
  resolve: (name) => {
    const pages = import.meta.glob('./pages/**/*.svelte')
    const path = './pages/' + name.replace(/\./g, '/') + '.svelte'
    const key = path in pages ? path : Object.keys(pages).find((k) => k.toLowerCase() === path.toLowerCase())
    const loader = key ? pages[key] : undefined
    if (!loader || typeof loader !== 'function') {
      throw new Error('Inertia page not found: ' + name + ' (resolved: ' + path + ')')
    }
    return loader()
  },
})
`

// ── Inertia kit: resources/js/Pages/Home/Index.svelte ─────────────
const inertiaPageHomeSvelte = `<script>
  export let title, appName, tagline
</script>

<div class="min-h-screen flex items-center justify-center bg-gray-100">
  <div class="text-center">
    <h1 class="text-4xl font-bold text-gray-800">{title}</h1>
    <p class="mt-2 text-xl text-gray-600">{appName}</p>
    <p class="mt-1 text-gray-500">{tagline}</p>
  </div>
</div>
`

// ── Inertia kit: package.json ──────────────────────────────────
func inertiaPackageJSON(kit string) string {
	deps := `"@inertiajs/react": "^1.0.0", "react": "^18.2.0", "react-dom": "^18.2.0"`
	devDeps := `"@types/react": "^18.2.0", "@types/react-dom": "^18.2.0", "@vitejs/plugin-react": "4.2.1", "typescript": "^5.0.0", "vite": "^5.0.0"`
	if kit == "vue" {
		deps = `"@inertiajs/vue3": "^1.0.0", "vue": "^3.4.0"`
		devDeps = `"@vitejs/plugin-vue": "5.0.0", "vite": "^5.0.0"`
	} else if kit == "svelte" {
		deps = `"@inertiajs/svelte": "^1.0.0", "svelte": "^4.0.0"`
		devDeps = `"@sveltejs/vite-plugin-svelte": "^3.0.0", "vite": "^5.0.0"`
	}
	return `{
  "name": "nimbus-inertia-app",
  "private": true,
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "vite build",
    "preview": "vite preview"
  },
  "devDependencies": {
    ` + devDeps + `
  },
  "dependencies": {
    ` + deps + `
  }
}
`
}

// ── Inertia kit: vite.config.js ─────────────────────────────────
func inertiaViteConfig(kit string) string {
	importLine := `import react from '@vitejs/plugin-react'`
	pluginLine := `react()`
	input := "inertia/app.tsx"
	if kit == "vue" {
		importLine = `import vue from '@vitejs/plugin-vue'`
		pluginLine = `vue()`
		input = "inertia/app.js"
	} else if kit == "svelte" {
		importLine = `import { svelte } from '@sveltejs/vite-plugin-svelte'`
		pluginLine = `svelte()`
		input = "inertia/app.js"
	}
	resolveAlias := ""
	if kit == "react" {
		resolveAlias = `
  resolve: {
    alias: { '~': path.resolve(process.cwd(), 'inertia') },
  },`
	}
	return `import { defineConfig } from 'vite'
` + importLine + `
import path from 'path'

export default defineConfig({
  plugins: [` + pluginLine + `],
  root: '.',
  publicDir: 'public',` + resolveAlias + `
  build: {
    outDir: 'public/build',
    manifest: true,
    rollupOptions: {
      input: { app: '` + input + `' },
      output: {
        entryFileNames: 'assets/[name].js',
        chunkFileNames: 'assets/[name].js',
        assetFileNames: 'assets/[name].[ext]',
      },
    },
  },
  server: {
    watch: { ignored: ['**/storage/**', '**/tmp/**'] },
  },
})
`
}

// ── Inertia kit: tsconfig.json (React) ───────────────────────────
const inertiaTsconfig = `{
  "compilerOptions": {
    "types": ["vite/client"],
    "target": "ES2020",
    "useDefineForClassFields": true,
    "lib": ["ES2020", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "skipLibCheck": true,
    "moduleResolution": "bundler",
    "allowImportingTsExtensions": true,
    "resolveJsonModule": true,
    "isolatedModules": true,
    "noEmit": true,
    "jsx": "react-jsx",
    "strict": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "noFallthroughCasesInSwitch": true,
    "baseUrl": ".",
    "paths": { "~/*": ["inertia/*"] }
  },
  "include": ["inertia/**/*.ts", "inertia/**/*.tsx"]
}
`

// ── Inertia kit: tsconfig.node.json (React) ──────────────────────
const inertiaTsconfigNode = `{
  "compilerOptions": {
    "composite": true,
    "skipLibCheck": true,
    "module": "ESNext",
    "moduleResolution": "bundler",
    "allowSyntheticDefaultImports": true
  },
  "include": ["vite.config.js"]
}
`

// ── Inertia kit: inertia/tsconfig.json (React) ────────────────────
const inertiaTsconfigInertia = `{
  "extends": "../tsconfig.json",
  "compilerOptions": {
    "baseUrl": ".",
    "paths": { "~/*": ["./*"] }
  },
  "include": ["./**/*.ts", "./**/*.tsx"]
}
`

// ── Inertia kit: inertia/types.ts (React) ─────────────────────────
const inertiaTypesTS = `import type { PropsWithChildren } from 'react'

/** JSON-serializable values (props passed from Go backend) */
export type JSONDataTypes =
  | string
  | number
  | boolean
  | null
  | JSONDataTypes[]
  | { [key: string]: JSONDataTypes }

/** Shared props available on all Inertia pages (extend as needed) */
export type SharedProps = Record<string, JSONDataTypes>

export type InertiaProps<T extends SharedProps = {}> = PropsWithChildren<
  SharedProps & T
>
`

// ── Inertia kit: index.html (Vite dev) ───────────────────────────
func inertiaIndexHTML(kit string) string {
	entry := "/inertia/app.tsx"
	if kit != "react" {
		entry = "/inertia/app.js"
	}
	return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Nimbus · Inertia</title>
</head>
<body>
  <div id="app"></div>
  <script type="module" src="` + entry + `"></script>
</body>
</html>
`
}

// ── config/config.go ───────────────────────────────────────────
const configLoader = `package config

import (
	nimbusconfig "github.com/CodeSyncr/nimbus/config"
)

func Load() {
	_ = nimbusconfig.LoadAuto()
	_ = nimbusconfig.LoadInto(&App)
	_ = nimbusconfig.LoadInto(&Database)
	buildDatabaseDSN()
}
`

// ── config/app.go ──────────────────────────────────────────────
const configApp = `package config

type AppConfig struct {
	Name string ` + "`config:\"app.name\" env:\"APP_NAME\" default:\"nimbus\"`" + `
	Env  string ` + "`config:\"app.env\" env:\"APP_ENV\" default:\"development\"`" + `
	Port int    ` + "`config:\"app.port\" env:\"PORT\" default:\"3333\"`" + `
}

var App AppConfig
`

// ── config/database.go ─────────────────────────────────────────
const configDatabase = `package config

import "fmt"

type DatabaseConfig struct {
	Driver   string ` + "`config:\"database.driver\" env:\"DB_DRIVER\" default:\"sqlite\"`" + `
	DSN      string ` + "`config:\"database.dsn\" env:\"DB_DSN\" default:\"\"`" + `
	Host     string ` + "`config:\"database.host\" env:\"DB_HOST\" default:\"localhost\"`" + `
	Port     string ` + "`config:\"database.port\" env:\"DB_PORT\" default:\"\"`" + `
	User     string ` + "`config:\"database.user\" env:\"DB_USER\" default:\"\"`" + `
	Password string ` + "`config:\"database.password\" env:\"DB_PASSWORD\" default:\"\"`" + `
	Database string ` + "`config:\"database.database\" env:\"DB_DATABASE\" default:\"nimbus\"`" + `
}

var Database DatabaseConfig

func buildDatabaseDSN() {
	if Database.DSN != "" {
		return
	}
	switch Database.Driver {
	case "postgres", "pg":
		if Database.Port == "" {
			Database.Port = "5432"
		}
		Database.DSN = fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
			Database.Host, Database.Port, Database.User, Database.Password, Database.Database)
	case "mysql":
		if Database.Port == "" {
			Database.Port = "3306"
		}
		Database.DSN = fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True",
			Database.User, Database.Password, Database.Host, Database.Port, Database.Database)
	default:
		Database.DSN = "database.sqlite"
	}
}
`

// ── .env.example ───────────────────────────────────────────────
const envExample = `PORT=3333
APP_ENV=development
APP_NAME={{.AppName}}
DB_DRIVER=sqlite
DB_DSN=database.sqlite
QUEUE_DRIVER=sync
`

// copyViewTemplates copies embedded templates/views/*.nimbus into the app's views/ folder.
func copyViewTemplates(appDir string) error {
	viewsDir := filepath.Join(appDir, "views")
	if err := os.MkdirAll(viewsDir, 0755); err != nil {
		return err
	}
	return fs.WalkDir(viewTemplates, "templates/views", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, err := fs.ReadFile(viewTemplates, path)
		if err != nil {
			return err
		}
		name := filepath.Base(path)
		dest := filepath.Join(viewsDir, name)
		return os.WriteFile(dest, body, 0644)
	})
}

const airConfigTmpl = `# Nimbus hot reload — do not edit (regenerated by nimbus serve)
root = "."
tmp_dir = "tmp"

[build]
  cmd = "go build -o ./tmp/main ."
  bin = "./tmp/main"
  delay = 1000
  exclude_dir = ["tmp", "vendor", "node_modules"]
  exclude_regex = ["_test.go"]
  include_ext = ["go", "nimbus"]
  send_interrupt = true
  kill_delay = "1s"

[log]
  time = false
  main_only = false

[misc]
  clean_on_exit = true
`

func runQueueWork(cmd *cobra.Command, args []string) error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	if !isNimbusApp(dir) {
		fmt.Println("Not a Nimbus app. Run 'nimbus queue:work' from your app root.")
		fmt.Println("Install queue: nimbus plugin install queue")
		return nil
	}
	c := exec.Command("go", "run", ".", "queue:work")
	c.Dir = dir
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

func runServe(cmd *cobra.Command, args []string) error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	if !isNimbusApp(dir) {
		fmt.Println("Not a Nimbus app. Run 'nimbus serve' from your app root (where go.mod and main.go are).")
		fmt.Println("Create an app with: nimbus new myapp")
		return nil
	}

	ensureAirConfig(dir)
	printServeBanner(dir)

	// Air (hot reload) - use process group so we can kill the whole tree on signal
	c := exec.Command("go", "run", "github.com/air-verse/air@v1.52.3")
	c.Dir = dir
	c.Stdin = os.Stdin
	if isInertiaApp(dir) {
		c.Env = append(os.Environ(), "VITE_DEV=1")
	}
	filter := newAirFilter(os.Stdout)
	c.Stdout = filter
	c.Stderr = filter
	if runtime.GOOS != "windows" {
		c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}

	var viteCmd *exec.Cmd
	if isInertiaApp(dir) {
		if err := ensureInertiaBuild(dir); err != nil {
			return err
		}
		viteCmd = exec.Command("npx", "vite")
		viteCmd.Dir = dir
		viteCmd.Env = append(os.Environ(), "FORCE_COLOR=1")
		viteCmd.Stdout = io.Discard
		viteCmd.Stderr = io.Discard
		if runtime.GOOS != "windows" {
			viteCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		}
		if err := viteCmd.Start(); err != nil {
			return err
		}
	}

	if err := c.Start(); err != nil {
		if viteCmd != nil && viteCmd.Process != nil {
			_ = viteCmd.Process.Kill()
		}
		return err
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	done := make(chan error, 1)
	go func() { done <- c.Wait() }()

	select {
	case sig := <-quit:
		fmt.Printf("\n  \033[33m⚠\033[0m  Received %v, shutting down...\n", sig)
		killProcessGroup(c, viteCmd)
		<-done
		return nil
	case err := <-done:
		if viteCmd != nil && viteCmd.Process != nil {
			_ = viteCmd.Process.Kill()
		}
		if err != nil && !strings.Contains(err.Error(), "signal") {
			return err
		}
		return nil
	}
}

// killProcessGroup kills the command and its process group (Unix). On Windows, kills the process only.
func killProcessGroup(air *exec.Cmd, vite *exec.Cmd) {
	if runtime.GOOS == "windows" {
		if air.Process != nil {
			_ = air.Process.Kill()
		}
		if vite != nil && vite.Process != nil {
			_ = vite.Process.Kill()
		}
		return
	}
	if air.Process != nil {
		_ = syscall.Kill(-air.Process.Pid, syscall.SIGTERM)
		time.Sleep(100 * time.Millisecond)
		_ = syscall.Kill(-air.Process.Pid, syscall.SIGKILL)
	}
	if vite != nil && vite.Process != nil {
		_ = syscall.Kill(-vite.Process.Pid, syscall.SIGTERM)
		time.Sleep(100 * time.Millisecond)
		_ = syscall.Kill(-vite.Process.Pid, syscall.SIGKILL)
	}
}

func ensureAirConfig(dir string) {
	path := filepath.Join(dir, ".air.toml")
	_ = os.WriteFile(path, []byte(airConfigTmpl), 0644)
}

// ---------------------------------------------------------------------------
// Pretty console output
// ---------------------------------------------------------------------------

const (
	cReset   = "\033[0m"
	cBold    = "\033[1m"
	cDim     = "\033[2m"
	cGreen   = "\033[32m"
	cYellow  = "\033[33m"
	cCyan    = "\033[36m"
	cMagenta = "\033[35m"
)

func printServeBanner(dir string) {
	fmt.Println()
	fmt.Printf("  %s%sNIMBUS%s %sDev Server%s\n", cBold, cCyan, cReset, cDim, cReset)
	fmt.Printf("  %s────────────────────────────────────%s\n", cDim, cReset)
	fmt.Printf("  %s➜%s  Mode:     %sdevelopment%s %s(hot reload)%s\n", cGreen, cReset, cYellow, cReset, cDim, cReset)
	fmt.Printf("  %s➜%s  Watching: %s.go%s, %s.nimbus%s files\n", cGreen, cReset, cCyan, cReset, cCyan, cReset)
	if isInertiaApp(dir) {
		fmt.Printf("  %s➜%s  Frontend: %sinertia/%s %s(HMR at localhost:5173)%s\n", cGreen, cReset, cCyan, cReset, cDim, cReset)
	}
	fmt.Println()
}

// airFilter is an io.Writer that strips Air's branded output (ASCII
// banner, "watching", "building", "running" lines) and replaces them
// with clean Nimbus-styled messages.
type airFilter struct {
	out     io.Writer
	scanner bool
	pw      *io.PipeWriter
	drop    *regexp.Regexp
}

func newAirFilter(out io.Writer) *airFilter {
	drop := regexp.MustCompile(
		`(?i)` +
			`(^\s*$)` + // blank lines
			`|(__\s+_\s+___)` + // Air ASCII art line 1
			`|(/ /\\)` + // Air ASCII art line 2
			`|(/_/--\\)` + // Air ASCII art line 3
			`|(watching\s+)` + // watching ...
			`|(!exclude\s+)` + // !exclude ...
			`|(see you again)` + // exit message
			`|(cleaning\.\.\.)`, // cleanup message
	)

	pr, pw := io.Pipe()
	f := &airFilter{out: out, pw: pw, drop: drop}

	go func() {
		sc := bufio.NewScanner(pr)
		for sc.Scan() {
			line := sc.Text()

			if f.drop.MatchString(line) {
				continue
			}

			trimmed := strings.TrimSpace(line)
			if strings.Contains(trimmed, "building") {
				fmt.Fprintf(out, "  %s⟳%s  Building...\n", cYellow, cReset)
				continue
			}
			if strings.Contains(trimmed, "running") && !strings.Contains(trimmed, "error") {
				fmt.Fprintf(out, "  %s✓%s  Ready\n\n", cGreen, cReset)
				continue
			}

			fmt.Fprintln(out, line)
		}
	}()

	return f
}

func (f *airFilter) Write(p []byte) (int, error) {
	return f.pw.Write(p)
}

// isNimbusApp reports whether dir contains a Nimbus app (go.mod requires nimbus + main.go).
func isNimbusApp(dir string) bool {
	modPath := filepath.Join(dir, "go.mod")
	mainPath := filepath.Join(dir, "main.go")
	mod, err := os.ReadFile(modPath)
	if err != nil {
		return false
	}
	if _, err := os.Stat(mainPath); err != nil {
		return false
	}
	return strings.Contains(string(mod), "CodeSyncr/nimbus") || strings.Contains(string(mod), "nimbus-framework/nimbus")
}

// isInertiaApp reports whether dir contains an Inertia frontend (package.json + inertia/).
func isInertiaApp(dir string) bool {
	pkgPath := filepath.Join(dir, "package.json")
	inertiaDir := filepath.Join(dir, "inertia")
	if _, err := os.Stat(pkgPath); err != nil {
		return false
	}
	if _, err := os.Stat(inertiaDir); err != nil {
		return false
	}
	return true
}

// ensureInertiaBuild checks npm install was run and runs npm run build. Returns error if npm install missing.
func ensureInertiaBuild(dir string) error {
	nodeModules := filepath.Join(dir, "node_modules")
	if _, err := os.Stat(nodeModules); err != nil {
		return fmt.Errorf("node_modules not found. Run 'npm install' first")
	}
	fmt.Println()
	fmt.Println("  Building frontend (npm run build)...")
	build := exec.Command("npm", "run", "build")
	build.Dir = dir
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		return fmt.Errorf("npm run build failed: %w", err)
	}
	fmt.Println()
	return nil
}

func runMakeModel(cmd *cobra.Command, args []string) error {
	name := args[0]
	snake := toSnake(name)
	dir := "app/models"
	_ = os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, snake+".go")
	content := fmt.Sprintf(modelTmpl, name, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return err
	}
	fmt.Printf("Created model %s at %s\n", name, path)
	return nil
}

const modelTmpl = `package models

import "github.com/CodeSyncr/nimbus/database"

// %s embeds the base model (ID, timestamps).
type %s struct {
	database.Model
	// Add fields here
}
`

func runMakeMigration(cmd *cobra.Command, args []string) error {
	name := args[0]
	dir := "database/migrations"
	_ = os.MkdirAll(dir, 0755)
	ts := time.Now().Format("20060102150405")
	snake := toSnake(name)
	filename := ts + "_" + snake + ".go"
	path := filepath.Join(dir, filename)
	pascal := toPascal(snake)
	tableName := snake
	if strings.HasPrefix(snake, "create_") {
		tableName = strings.TrimPrefix(snake, "create_")
	} else if strings.HasPrefix(snake, "add_") {
		tableName = strings.TrimPrefix(snake, "add_") + "_table"
	}
	content := fmt.Sprintf(migrationTmpl, pascal, pascal, pascal, tableName, pascal, tableName, pascal, tableName)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return err
	}
	fmt.Printf("Created migration %s at %s\n", name, path)
	fmt.Printf("Add it to database/migrations/registry.go: {Name: %q, Up: (&%s{}).Up, Down: (&%s{}).Down},\n", ts+"_"+snake, pascal, pascal)
	return nil
}

const migrationTmpl = `package migrations

import (
	"gorm.io/gorm"

	"github.com/CodeSyncr/nimbus/database/schema"
)

// %s migration — AdonisJS Lucid-style schema.
type %s struct {
	schema.BaseSchema
}

// TableName returns the migration name for tracking.
func (m *%s) TableName() string {
	return %q
}

// Up creates or alters the schema.
func (m *%s) Up(db *gorm.DB) error {
	return schema.New(db).CreateTable(%q, func(t *schema.Table) {
		t.Increments("id")
		t.Timestamps()
	})
}

// Down reverts the migration.
func (m *%s) Down(db *gorm.DB) error {
	return schema.New(db).DropTable(%q)
}
`

func runMakeController(cmd *cobra.Command, args []string) error {
	name := args[0]
	snake := toSnake(name)
	dir := "app/controllers"
	_ = os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, snake+".go")
	content := fmt.Sprintf(controllerTmpl, name, name, name, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return err
	}
	fmt.Printf("Created controller %s at %s\n", name, path)
	return nil
}

const controllerTmpl = `package controllers

import (
	"net/http"

	"github.com/CodeSyncr/nimbus/context"
)

// %s controller.
type %s struct{}

// Index lists resources.
func (c *%s) Index(ctx *context.Context) error {
	return ctx.JSON(http.StatusOK, map[string]string{"message": "index"})
}

// Show returns one resource.
func (c *%s) Show(ctx *context.Context) error {
	id := ctx.Param("id")
	return ctx.JSON(http.StatusOK, map[string]string{"id": id})
}
`

func runMakeMiddleware(cmd *cobra.Command, args []string) error {
	name := args[0]
	snake := toSnake(name)
	dir := "app/middleware"
	_ = os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, snake+".go")
	content := fmt.Sprintf(middlewareTmpl, name, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return err
	}
	fmt.Printf("Created middleware %s at %s\n", name, path)
	return nil
}

const middlewareTmpl = `package middleware

import (
	"github.com/CodeSyncr/nimbus/context"
	"github.com/CodeSyncr/nimbus/router"
)

// %s returns a middleware that runs before the handler.
func %s() router.Middleware {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *context.Context) error {
			// Before handler
			err := next(c)
			// After handler
			return err
		}
	}
}
`

func runMakeJob(cmd *cobra.Command, args []string) error {
	name := args[0]
	snake := toSnake(name)
	dir := "app/jobs"
	_ = os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, snake+".go")
	content := fmt.Sprintf(jobTmpl, name, name, name, name, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return err
	}
	fmt.Printf("Created job %s at %s\n", name, path)
	return nil
}

const jobTmpl = `package jobs

import "context"

// %s implements queue.Job. Register in start/jobs.go and dispatch with:
//   queue.Dispatch(&jobs.%s{...}).Dispatch(ctx)
type %s struct {
	// Add payload fields
}

func (j *%s) Handle(ctx context.Context) error {
	// Do work
	return nil
}
`

func runMakeSeeder(cmd *cobra.Command, args []string) error {
	name := args[0]
	snake := toSnake(name)
	dir := "database/seeders"
	_ = os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, snake+".go")
	content := fmt.Sprintf(seederTmpl, name, name, name, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return err
	}
	fmt.Printf("Created seeder %s at %s\n", name, path)
	return nil
}

const seederTmpl = `package seeders

import "gorm.io/gorm"

// %s seeds data. Add to database.NewSeedRunner(db, []database.Seeder{&%s{}}).Run()
type %s struct{}

func (s *%s) Run(db *gorm.DB) error {
	// db.Create(&Model{...})
	return nil
}
`

func runMakeValidator(cmd *cobra.Command, args []string) error {
	name := args[0]
	snake := toSnake(name)
	dir := "app/validators"
	_ = os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, snake+".go")
	pascal := toPascal(snake)
	content := fmt.Sprintf(validatorTmpl, pascal, pascal, pascal)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return err
	}
	fmt.Printf("Created validator %s at %s\n", pascal, path)
	return nil
}

const validatorTmpl = "package validators\n\nimport \"github.com/CodeSyncr/nimbus/validation\"\n\n// %s validates request input. Use with validation.ValidateStruct().\ntype %s struct {\n\tTitle string `form:\"title\" validate:\"required,min=1,max=255\"`\n}\n\nfunc (v *%s) Validate() error {\n\treturn validation.ValidateStruct(v)\n}\n"

func runMakeCommand(cmd *cobra.Command, args []string) error {
	name := args[0]
	// File name: greet -> greet.go, make:controller -> make_controller.go
	fileSnake := toSnake(strings.ReplaceAll(name, ":", "_"))
	dir := "commands"
	_ = os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, fileSnake+".go")
	pascal := toPascal(fileSnake)
	content := fmt.Sprintf(commandTmpl, pascal, pascal, pascal, name, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return err
	}
	fmt.Printf("Created command %q at %s\n", name, path)
	fmt.Println("")
	fmt.Println("Wire main.go with command.Run(os.Args[1:]) to run: go run . " + name)
	fmt.Println("See: /docs/creating-commands")
	return nil
}

const commandTmpl = `package commands

import (
	"fmt"

	"github.com/CodeSyncr/nimbus/command"
)

func init() {
	command.Register(%sCommand())
}

// %sCommand returns the command (AdonisJS Ace style).
func %sCommand() *command.Command {
	return command.New(%q, "Description of the command").
		Long("Longer help text for --help").
		RunE(func(ctx *command.Ctx) error {
			fmt.Println("Hello from " + %q)
			return nil
		})
}
`

func runMakePlugin(cmd *cobra.Command, args []string) error {
	name := args[0]
	snake := toSnake(name)
	pascal := toPascal(snake)
	dir := filepath.Join("app", "plugins", snake)
	_ = os.MkdirAll(dir, 0755)

	files := map[string]string{
		filepath.Join(dir, "plugin.go"):     fmt.Sprintf(pluginMainTmpl, snake, pascal),
		filepath.Join(dir, "config.go"):     fmt.Sprintf(pluginConfigTmpl, snake, pascal),
		filepath.Join(dir, "routes.go"):     fmt.Sprintf(pluginRoutesTmpl, snake, pascal),
		filepath.Join(dir, "middleware.go"): fmt.Sprintf(pluginMiddlewareTmpl, snake, pascal),
	}

	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return err
		}
	}

	fmt.Printf("Created plugin %s at %s/\n", pascal, dir)
	fmt.Println("")
	fmt.Println("Register it in bin/server.go:")
	fmt.Printf("  app.Use(&%s.%sPlugin{})\n", snake, pascal)
	return nil
}

const pluginMainTmpl = `package %[1]s

import "github.com/CodeSyncr/nimbus"

// Compile-time interface checks.
var (
	_ nimbus.Plugin        = (*%[2]sPlugin)(nil)
	_ nimbus.HasRoutes     = (*%[2]sPlugin)(nil)
	_ nimbus.HasMiddleware = (*%[2]sPlugin)(nil)
	_ nimbus.HasConfig     = (*%[2]sPlugin)(nil)
)

// %[2]sPlugin is a Nimbus plugin.
type %[2]sPlugin struct {
	nimbus.BasePlugin
}

// New creates a new %[2]sPlugin instance.
func New() *%[2]sPlugin {
	return &%[2]sPlugin{
		BasePlugin: nimbus.BasePlugin{
			PluginName:    "%[1]s",
			PluginVersion: "0.1.0",
		},
	}
}
`

const pluginConfigTmpl = `package %[1]s

// DefaultConfig returns the default configuration for the %[2]s plugin.
// These values can be overridden by the application's .env file.
func (p *%[2]sPlugin) DefaultConfig() map[string]any {
	return map[string]any{
		"enabled": true,
	}
}
`

const pluginRoutesTmpl = `package %[1]s

import (
	"net/http"

	"github.com/CodeSyncr/nimbus/context"
	"github.com/CodeSyncr/nimbus/router"
)

// RegisterRoutes mounts the plugin's HTTP routes onto the application router.
func (p *%[2]sPlugin) RegisterRoutes(r *router.Router) {
	r.Get("/%[1]s/status", p.statusHandler)
}

func (p *%[2]sPlugin) statusHandler(c *context.Context) error {
	return c.JSON(http.StatusOK, map[string]string{
		"plugin":  p.Name(),
		"version": p.Version(),
		"status":  "ok",
	})
}
`

const pluginMiddlewareTmpl = `package %[1]s

import (
	"github.com/CodeSyncr/nimbus/context"
	"github.com/CodeSyncr/nimbus/router"
)

// Middleware returns named middleware provided by this plugin.
// Assign them to routes in start/routes.go:
//
//	app.Router.Get("/protected", handler, start.Middleware["%[1]s"])
func (p *%[2]sPlugin) Middleware() map[string]router.Middleware {
	return map[string]router.Middleware{
		"%[1]s": p.exampleMiddleware(),
	}
}

func (p *%[2]sPlugin) exampleMiddleware() router.Middleware {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *context.Context) error {
			// before handler
			err := next(c)
			// after handler
			return err
		}
	}
}
`

func runDbMigrate(cmd *cobra.Command, args []string) error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	if !isNimbusApp(dir) {
		fmt.Println("Not a Nimbus app. Run 'nimbus db:migrate' from your app root.")
		return nil
	}
	c := exec.Command("go", "run", ".", "migrate")
	c.Dir = dir
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

func runDbRollback(cmd *cobra.Command, args []string) error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	if !isNimbusApp(dir) {
		fmt.Println("Not a Nimbus app. Run 'nimbus db:rollback' from your app root.")
		return nil
	}
	// Rollback requires a separate entrypoint; for now run migrate with -rollback flag if we add it
	fmt.Println("Rollback: run migrator.Down() from your app (e.g. go run . rollback when implemented).")
	return nil
}

func runRepl(cmd *cobra.Command, args []string) error {
	return repl.Run()
}

func runMakeDeployConfig(cmd *cobra.Command, args []string) error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	if !isNimbusApp(dir) {
		return fmt.Errorf("not a Nimbus app. Run from app root")
	}
	path := filepath.Join(dir, "deploy.yaml")
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("deploy.yaml already exists")
	}
	appName := filepath.Base(dir)
	content := strings.Replace(deploy.DeployYAMLExample, "my-app", appName, 1)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return err
	}
	fmt.Printf("Created deploy.yaml. Edit and run: nimbus deploy fly\n")
	return nil
}

func runRelease(cmd *cobra.Command, args []string) error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	if !release.IsNimbusRepo(dir) {
		return fmt.Errorf("not the Nimbus repo. Run 'nimbus release' from github.com/CodeSyncr/nimbus")
	}
	bump := release.BumpPatch
	if len(args) > 0 {
		bump = release.BumpType(strings.ToLower(args[0]))
		if bump != release.BumpPatch && bump != release.BumpMinor && bump != release.BumpMajor {
			return fmt.Errorf("invalid bump %q. Use: patch, minor, major", args[0])
		}
	}
	oldVer := version.Nimbus
	newVer, err := release.BumpVersion(oldVer, bump)
	if err != nil {
		return err
	}
	fmt.Printf("  Bumping %s -> %s\n", oldVer, newVer)
	if err := release.UpdateVersionConstant(dir, newVer); err != nil {
		return err
	}
	fmt.Println("  Updated internal/version/version.go")
	if err := release.GitTag(dir, newVer); err != nil {
		return fmt.Errorf("git tag failed: %w", err)
	}
	fmt.Printf("  Created git tag %s\n", newVer)
	fmt.Println()
	fmt.Printf("  Next: git add -A && git commit -m %q && git push && git push --tags\n", "release "+newVer)
	return nil
}

func runDeploy(cmd *cobra.Command, args []string) error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	if !isNimbusApp(dir) {
		return fmt.Errorf("not a Nimbus app. Run 'nimbus deploy' from your app root")
	}
	cfg, _ := deploy.LoadConfig(dir)
	target := ""
	if len(args) > 0 {
		target = strings.TrimSpace(strings.ToLower(args[0]))
	}
	// Always prompt when no target in args (even if deploy.yaml exists)
	if target == "" {
		var promptErr error
		target, cfg, promptErr = deploy.PromptTarget(dir, cfg)
		if promptErr != nil {
			return promptErr
		}
	} else if cfg == nil {
		cfg = &deploy.Config{}
	}
	return deploy.Deploy(dir, target, cfg)
}

func readModulePath(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(line[7:])
		}
	}
	return ""
}

const migrationsRegistryStub = `package migrations

import "github.com/CodeSyncr/nimbus/database"

// All returns migrations in run order. Add new migrations here when you run make:migration.
func All() []database.Migration {
	return []database.Migration{}
}
`

func toSnake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		if r >= 'A' && r <= 'Z' {
			r = r + ('a' - 'A')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func toPascal(snake string) string {
	var b strings.Builder
	up := true
	for _, r := range snake {
		if r == '_' {
			up = true
			continue
		}
		if up && r >= 'a' && r <= 'z' {
			r = r - ('a' - 'A')
			up = false
		}
		b.WriteRune(r)
	}
	return b.String()
}
