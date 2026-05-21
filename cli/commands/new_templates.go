package commands

import (
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

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
	if len(os.Args) > 1 && os.Args[1] == "db:create" {
		bin.RunDbCreate()
		return
	}
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
const binServerTmpl = `package bin

import (
	"context"
	"fmt"
	"os"

	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/auth"
	"github.com/CodeSyncr/nimbus/cache"
	"github.com/CodeSyncr/nimbus/database"
	"github.com/CodeSyncr/nimbus/queue"
	"github.com/CodeSyncr/nimbus/view"
	"github.com/CodeSyncr/nimbus/lucid"

	"{{.AppName}}/config"
	"{{.AppName}}/database/migrations"
	"{{.AppName}}/start"
)

func Boot() *nimbus.App {
	config.Load()

	app := nimbus.New()

	// Basic apps keep their templates under resources/views.
	view.SetRoot("resources/views")

	bootCache()
	bootDatabase(app)
	bootQueue()
	bootAuth(app)

	start.RegisterMiddleware(app)
	start.RegisterRoutes(app)

	return app
}

func bootAuth(app *nimbus.App) {
	if config.AuthGuard == "stateless" {
		bootStatelessAuth(app)
	}
}

func bootStatelessAuth(app *nimbus.App) {
	var driver auth.TokenDriver
	if config.StatelessToken.Driver == "paseto" {
		driver = auth.NewPasetoDriver(config.StatelessToken.Secret)
	} else {
		driver = auth.NewJWTDriver(config.StatelessToken.Secret)
	}

	guard := auth.NewStatelessGuard(driver, auth.UserLoaderFunc(func(ctx context.Context, id string) (auth.User, error) {
		// Load the user for the token subject (e.g. after make:auth):
		//   var u models.User
		//   if err := database.Get().First(&u, "id = ?", id).Error; err != nil {
		//     return nil, err
		//   }
		//   return &u, nil
		_ = ctx
		_ = id
		return nil, fmt.Errorf("stateless auth: implement UserLoader in bootStatelessAuth (see comment above)")
	}))

	app.Container.Singleton("auth.stateless", func() *auth.StatelessGuard {
		return guard
	})
}

func bootCache() {
	cache.Boot(nil)
}

func bootDatabase(app *nimbus.App) {
	db, err := database.ConnectWithConfig(database.ConnectConfig{
		Driver: config.Database.Driver,
		DSN:    config.Database.DSN,
		Debug:  config.App.Env == "development",
		PoolConfig: database.PoolConfigFromFields(
			config.Database.MaxOpenConns,
			config.Database.MaxIdleConns,
			config.Database.ConnMaxLifetime,
			config.Database.ConnMaxIdleTime,
		),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Database connection failed: %v\n", err)
		os.Exit(1)
	}

	// Keep global DB in sync for packages/components using nimbus.GetDB().
	nimbus.SetDB(db)

	app.Container.Singleton("db", func() *lucid.DB {
		return db
	})
}

func bootQueue() {
	queue.Boot(&queue.BootConfig{RegisterJobs: start.RegisterQueueJobs})
}

func RunQueueWorker() {
	app := Boot()
	if err := app.Boot(); err != nil {
		fmt.Fprintf(os.Stderr, "Boot failed: %v\n", err)
		os.Exit(1)
	}
	ctx := context.Background()
	queue.RunWorker(ctx, "default")
}

func RunMigrations() {
	config.Load()
	if err := database.CreateDatabaseIfNotExists(database.CreateConfig{
		Driver:   config.Database.Driver,
		Host:     config.Database.Host,
		Port:     config.Database.Port,
		User:     config.Database.User,
		Password: config.Database.Password,
		Database: config.Database.Database,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Database create failed: %v\n", err)
		os.Exit(1)
	}
	db, err := database.ConnectWithConfig(database.ConnectConfig{
		Driver: config.Database.Driver,
		DSN:    config.Database.DSN,
		Debug:  config.App.Env == "development",
		PoolConfig: database.PoolConfigFromFields(
			config.Database.MaxOpenConns,
			config.Database.MaxIdleConns,
			config.Database.ConnMaxLifetime,
			config.Database.ConnMaxIdleTime,
		),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Database connection failed: %v\n", err)
		os.Exit(1)
	}
	migrator := database.NewMigrator(db, migrations.All())
	if err := migrator.Up(); err != nil {
		fmt.Fprintf(os.Stderr, "Migration failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Migrations completed.")
}

func RunDbCreate() {
	config.Load()
	if err := database.CreateDatabaseIfNotExists(database.CreateConfig{
		Driver:   config.Database.Driver,
		Host:     config.Database.Host,
		Port:     config.Database.Port,
		User:     config.Database.User,
		Password: config.Database.Password,
		Database: config.Database.Database,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Database create failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Database created (or already exists).")
}
`

// ── start/kernel.go ────────────────────────────────────────────
const kernelStub = `package start

import (
	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/auth"
	"github.com/CodeSyncr/nimbus/errors"
	"github.com/CodeSyncr/nimbus/middleware"
	"github.com/CodeSyncr/nimbus/router"
)

func RegisterMiddleware(app *nimbus.App) {
	// Logger (outer) → errors.Handler → Recover (inner): panics become AppError
	// inside Recover and are rendered by Handler (HTML/JSON).
	app.Router.Use(
		middleware.Logger(),
		errors.Handler(),
		middleware.Recover(),
	)

	// ── Named Middleware ──────────────────────────────────
	// Register stateless auth if available in container
	if g, err := app.Container.Make("auth.stateless"); err == nil {
		Middleware["auth:api"] = auth.RequireStatelessToken(g.(*auth.StatelessGuard))
	}
}

var Middleware = map[string]router.Middleware{}
`

// ── start/routes.go ────────────────────────────────────────────
const routesStub = `package start

import (
	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/http"
)

func RegisterRoutes(app *nimbus.App) {
	app.Router.Get("/", homeHandler)
	app.Router.Get("/health", healthHandler)
}

func homeHandler(c *http.Context) error {
	return c.View("home", map[string]any{
		"title":   "Welcome",
		"appName": "Nimbus",
		"version": "{{.NimbusVersion}}",
		"env":     "development",
		"tagline": "A Laravel-inspired web framework for Go — expressive, elegant, and blazing fast. From scaffolding to production in minutes.",
	})
}

func healthHandler(c *http.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}
`

// ── Inertia kit: bin/server.go ─────────────────────────────────
const binServerInertiaTmpl = `package bin

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

	if _, err := database.ConnectWithConfig(database.ConnectConfig{
		Driver: config.Database.Driver,
		DSN:    config.Database.DSN,
		Debug:  config.App.Env == "development",
		PoolConfig: database.PoolConfigFromFields(
			config.Database.MaxOpenConns,
			config.Database.MaxIdleConns,
			config.Database.ConnMaxLifetime,
			config.Database.ConnMaxIdleTime,
		),
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Database connection failed: %v\n", err)
		os.Exit(1)
	}

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
	db, err := database.ConnectWithConfig(database.ConnectConfig{
		Driver: config.Database.Driver,
		DSN:    config.Database.DSN,
		Debug:  config.App.Env == "development",
		PoolConfig: database.PoolConfigFromFields(
			config.Database.MaxOpenConns,
			config.Database.MaxIdleConns,
			config.Database.ConnMaxLifetime,
			config.Database.ConnMaxIdleTime,
		),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Database connection failed: %v\n", err)
		os.Exit(1)
	}
	migrator := database.NewMigrator(db, migrations.All())
	if err := migrator.Up(); err != nil {
		fmt.Fprintf(os.Stderr, "Migration failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Migrations completed.")
}

func RunDbCreate() {
	config.Load()
	if err := database.CreateDatabaseIfNotExists(database.CreateConfig{
		Driver:   config.Database.Driver,
		Host:     config.Database.Host,
		Port:     config.Database.Port,
		User:     config.Database.User,
		Password: config.Database.Password,
		Database: config.Database.Database,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Database create failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Database created (or already exists).")
}
`

// ── Inertia kit: start/routes.go ────────────────────────────────
const routesInertiaStub = `package start

import (
	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/plugins/inertia"
)

func RegisterRoutes(app *nimbus.App) {
	app.Router.Get("/build/*", buildAssetsHandler)
	app.Router.Get("/", homeHandler)
	app.Router.Get("/health", healthHandler)
}

func buildAssetsHandler(c *http.Context) error {
	fs := http.StripPrefix("/build", http.FileServer(http.Dir("public/build")))
	fs.ServeHTTP(c.Response, c.Request)
	return nil
}

func homeHandler(c *http.Context) error {
	return inertia.Render(c, "home/index", map[string]any{
		"title":   "Welcome",
		"appName": "Nimbus",
		"version": "{{.NimbusVersion}}",
		"tagline": "A Laravel-inspired web framework for Go — expressive, elegant, and blazing fast. From scaffolding to production in minutes.",
		"env":     "development",
	})
}

func healthHandler(c *http.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}
`

// ── Inertia kit: resources/views/inertia_layout.nimbus ───────────
const inertiaLayoutNimbus = `<!DOCTYPE html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title inertia>Nimbus</title>
    <link rel="preconnect" href="https://fonts.googleapis.com">
    <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
    <link href="https://fonts.googleapis.com/css2?family=Fraunces:ital,opsz,wght@0,9..144,300;0,9..144,400;0,9..144,500;1,9..144,300;1,9..144,400&family=DM+Sans:wght@300;400;500&family=JetBrains+Mono:wght@400;500&display=swap" rel="stylesheet">
    <style>` + nimbusLandingPageCSS + `    </style>
    {{ if .viteDev }}
    {{DEV_SCRIPTS}}
    {{ else }}
    <link rel="stylesheet" href="/build/assets/app.css" />
    <script type="module" src="{{SCRIPT_SRC}}"></script>
    {{ end }}
  </head>
  <body>
    <div id="app" data-page="{{ marshal .page }}"></div>
  </body>
</html>
`

// ── Inertia kit: inertia/app.tsx (React) ─────────────────────────
const inertiaAppReact = `import { createRoot } from 'react-dom/client'
import { createInertiaApp } from '@inertiajs/react'

createInertiaApp({
  resolve: (name) => {
    const pages = import.meta.glob('./pages/**/*.tsx')
    return pages['./pages/' + name.replace(/\./g, '/') + '.tsx']()
  },
  setup({ el, App, props }) {
    createRoot(el).render(<App {...props} />)
  },
})
`

// ── Inertia kit: resources/inertia/layouts/default.tsx ─────────────
const inertiaLayoutDefault = `import { ReactNode } from 'react'

export default function Layout({ children }: { children: ReactNode }) {
  return <div>{children}</div>
}
`

// ── Inertia kit: inertia/app.ts (Vue) ────────────────────────────────
const inertiaAppVue = `import { createApp, h } from 'vue'
import { createInertiaApp } from '@inertiajs/vue3'

createInertiaApp({
  resolve: (name) => {
    const pages = import.meta.glob('./pages/**/*.vue')
    return pages['./pages/' + name.replace(/\./g, '/') + '.vue']()
  },
  setup({ el, App, props, plugin }) {
    createApp({ render: () => h(App, props) }).use(plugin).mount(el)
  },
})
`

// ── Inertia kit: inertia/app.ts (Svelte) ─────────────────────────────
const inertiaAppSvelte = `import { createInertiaApp } from '@inertiajs/svelte'
import { mount } from 'svelte'

createInertiaApp({
  resolve: (name) => {
    const pages = import.meta.glob('./pages/**/*.svelte')
    return pages['./pages/' + name.replace(/\./g, '/') + '.svelte']()
  },
  setup({ el, App, props }) {
    mount(App, { target: el, props })
  },
})
`

// ── Inertia kit: package.json ──────────────────────────────────
func inertiaPackageJSON(kit string) string {
	switch kit {
	case "vue":
		return `{
  "name": "nimbus-inertia-app",
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "vite build"
  },
  "dependencies": {
    "@inertiajs/vue3": "^1.0.0",
    "vue": "^3.4.0"
  },
  "devDependencies": {
    "@vitejs/plugin-vue": "^5.0.0",
    "typescript": "^5.3.0",
    "vite": "^5.0.0"
  }
}
`
	case "svelte":
		return `{
  "name": "nimbus-inertia-app",
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "vite build"
  },
  "dependencies": {
    "@inertiajs/svelte": "^1.0.0",
    "svelte": "^5.0.0"
  },
  "devDependencies": {
    "@sveltejs/vite-plugin-svelte": "^4.0.0",
    "typescript": "^5.3.0",
    "vite": "^5.0.0"
  }
}
`
	}
	return `{
  "name": "nimbus-inertia-app",
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "vite build"
  },
  "dependencies": {
    "@inertiajs/react": "^1.0.0",
    "react": "^18.2.0",
    "react-dom": "^18.2.0"
  },
  "devDependencies": {
    "@types/react": "^18.2.0",
    "@types/react-dom": "^18.2.0",
    "@vitejs/plugin-react": "^4.2.0",
    "typescript": "^5.3.0",
    "vite": "^5.0.0"
  }
}
`
}

func inertiaViteConfig(kit string) string {
	switch kit {
	case "vue":
		return `import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  server: {
    origin: 'http://localhost:5173',
    cors: true,
    hmr: {
      host: 'localhost',
    },
  },
  build: {
    outDir: 'public/build',
    manifest: true,
    rollupOptions: { input: { app: 'inertia/app.ts' } }
  }
})
`
	case "svelte":
		return `import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

export default defineConfig({
  plugins: [svelte()],
  server: {
    origin: 'http://localhost:5173',
    cors: true,
    hmr: {
      host: 'localhost',
    },
  },
  build: {
    outDir: 'public/build',
    manifest: true,
    rollupOptions: { input: { app: 'inertia/app.ts' } }
  }
})
`
	}
	return `import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    origin: 'http://localhost:5173',
    cors: true,
    hmr: {
      host: 'localhost',
    },
  },
  build: {
    outDir: 'public/build',
    manifest: true,
    rollupOptions: { input: { app: 'inertia/app.tsx' } }
  }
})
`
}

const inertiaTsconfig = `{
  "compilerOptions": {
    "target": "ES2020",
    "useDefineForClassFields": true,
    "lib": ["ES2020", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "skipLibCheck": true,
    "moduleResolution": "bundler",
    "allowImportingTsExtensions": true,
    "isolatedModules": true,
    "moduleDetection": "force",
    "noEmit": true,
    "jsx": "react-jsx",
    "strict": true,
    "noUnusedLocals": false,
    "noUnusedParameters": false,
    "noFallthroughCasesInSwitch": true,
    "esModuleInterop": true,
    "forceConsistentCasingInFileNames": true
  },
  "include": ["inertia"]
}`

const inertiaTsconfigVueSvelte = `{
  "compilerOptions": {
    "target": "ES2020",
    "lib": ["ES2020", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "skipLibCheck": true,
    "moduleResolution": "bundler",
    "allowImportingTsExtensions": true,
    "isolatedModules": true,
    "moduleDetection": "force",
    "noEmit": true,
    "strict": true,
    "esModuleInterop": true,
    "forceConsistentCasingInFileNames": true
  },
  "include": ["inertia", "inertia/types.ts"]
}`

const inertiaTsconfigNode = `{
  "compilerOptions": {
    "target": "ES2022",
    "lib": ["ES2023"],
    "module": "ESNext",
    "skipLibCheck": true,
    "moduleResolution": "bundler",
    "allowImportingTsExtensions": true,
    "isolatedModules": true,
    "moduleDetection": "force",
    "noEmit": true,
    "strict": true,
    "noUnusedLocals": false,
    "noUnusedParameters": false,
    "noFallthroughCasesInSwitch": true
  },
  "include": ["vite.config.ts"]
}`

const inertiaTsconfigInertia = `{
  "compilerOptions": {
    "target": "ES2020",
    "useDefineForClassFields": true,
    "lib": ["ES2020", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "skipLibCheck": true,
    "moduleResolution": "bundler",
    "allowImportingTsExtensions": true,
    "isolatedModules": true,
    "moduleDetection": "force",
    "noEmit": true,
    "jsx": "react-jsx",
    "strict": true,
    "esModuleInterop": true,
    "forceConsistentCasingInFileNames": true
  },
  "include": ["."]
}`

const inertiaTypesTS = `/// <reference types="vite/client" />

declare module '*.tsx' {
  const component: React.FC<any>
  export default component
}

export {}`

const inertiaTypesVueSvelte = `/// <reference types="vite/client" />

declare module '*.vue' {
  const component: any
  export default component
}

declare module '*.svelte' {
  const component: any
  export default component
}

export {}`

func inertiaIndexHTML(kit string) string {
	return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>Nimbus App</title>
</head>
<body>
  <div id="app"></div>
  <script src="/inertia/app.tsx" type="module"></script>
</body>
</html>`
}

// ── config/config.go ───────────────────────────────────────────
const configLoader = `package config

import nimbusconfig "github.com/CodeSyncr/nimbus/config"

func Load() {
	_ = nimbusconfig.LoadAuto()
	_ = nimbusconfig.LoadInto(&App)
	_ = nimbusconfig.LoadInto(&Database)
	buildDatabaseDSN()

	loadBodyParser()
	loadCache()
	loadCORS()
	loadHash()
	loadLimiter()
	loadLogger()
	loadMail()
	loadSession()
	loadShield()
	loadStatic()
	loadStorage()
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

// DatabaseConnectionConfig holds settings for a single named connection.
type DatabaseConnectionConfig struct {
	Driver string
	DSN    string
}

// DatabaseConfig holds primary and additional connection settings.
type DatabaseConfig struct {
	// Primary SQL connection
	Driver   string ` + "`config:\"database.driver\" env:\"DB_DRIVER\" default:\"sqlite\"`" + `
	DSN      string ` + "`config:\"database.dsn\" env:\"DB_DSN\" default:\"\"`" + `
	Host     string ` + "`config:\"database.host\" env:\"DB_HOST\" default:\"localhost\"`" + `
	Port     string ` + "`config:\"database.port\" env:\"DB_PORT\" default:\"\"`" + `
	User     string ` + "`config:\"database.user\" env:\"DB_USER\" default:\"\"`" + `
	Password string ` + "`config:\"database.password\" env:\"DB_PASSWORD\" default:\"\"`" + `
	Database string ` + "`config:\"database.database\" env:\"DB_DATABASE\" default:\"nimbus\"`" + `

	// SQL pool (optional). For production, set max open conns and ConnMaxLifetime
	// so connections recycle before proxies or the server drops them.
	// Durations: Go time.ParseDuration, e.g. "5m", "90s". Empty = leave default.
	MaxOpenConns    int    ` + "`config:\"database.pool.max_open\" env:\"DB_MAX_OPEN_CONNS\" default:\"0\"`" + `
	MaxIdleConns    int    ` + "`config:\"database.pool.max_idle\" env:\"DB_MAX_IDLE_CONNS\" default:\"0\"`" + `
	ConnMaxLifetime string ` + "`config:\"database.pool.conn_max_lifetime\" env:\"DB_CONN_MAX_LIFETIME\" default:\"\"`" + `
	ConnMaxIdleTime string ` + "`config:\"database.pool.conn_max_idle_time\" env:\"DB_CONN_MAX_IDLE_TIME\" default:\"\"`" + `

	// Additional named SQL connections (e.g. "analytics", "logs").
	// Register them in start/kernel.go with database.ConnectAll().
	Connections map[string]DatabaseConnectionConfig

	// MongoDB / NoSQL
	MongoURI      string ` + "`config:\"mongo.uri\" env:\"MONGO_URI\" default:\"\"`" + `
	MongoDatabase string ` + "`config:\"mongo.database\" env:\"MONGO_DATABASE\" default:\"\"`" + `
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
	case "supabase":
		// Supabase: use SUPABASE_DB_URL directly if available.
		if v := env("SUPABASE_DB_URL", ""); v != "" {
			Database.DSN = v
		} else {
			// Fallback: construct from standard DB_ vars.
			if Database.Port == "" {
				Database.Port = "5432"
			}
			Database.DSN = fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
				Database.Host, Database.Port, Database.User, Database.Password, Database.Database)
		}
	default:
		Database.DSN = "database.sqlite"
	}
}
`

// ── config/env.go ──────────────────────────────────────────────
const configEnv = `package config

import (
	"os"
	"strconv"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}
`

// ── config/cors.go ─────────────────────────────────────────────
const configCORS = `/*
|--------------------------------------------------------------------------
| CORS Configuration
|--------------------------------------------------------------------------
|
| Cross-Origin Resource Sharing controls which external domains can
| make requests to your API.
|
| AllowOrigins:
|   - ["*"]                         - allow all origins
|   - ["https://app.example.com"]   - specific domain(s)
|
| When AllowCredentials is true, browsers reject the "*" origin.
| Nimbus automatically reflects the requesting origin instead.
|
*/

package config

var CORS CORSConfig

type CORSConfig struct {
	Enabled          bool
	AllowOrigins     []string
	AllowMethods     []string
	AllowHeaders     []string
	ExposeHeaders    []string
	AllowCredentials bool
	MaxAge           int
}

func loadCORS() {
	CORS = CORSConfig{
		Enabled:          envBool("CORS_ENABLED", true),
		AllowOrigins:     []string{env("CORS_ORIGIN", "*")},
		AllowMethods:     []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Content-Type", "Authorization", "X-Requested-With"},
		ExposeHeaders:    []string{},
		AllowCredentials: envBool("CORS_CREDENTIALS", false),
		MaxAge:           envInt("CORS_MAX_AGE", 86400),
	}
}
`

// ── config/session.go ──────────────────────────────────────────
const configSession = `/*
|--------------------------------------------------------------------------
| Session Configuration
|--------------------------------------------------------------------------
|
| Supported drivers: "cookie", "memory", "redis", "database"
|
*/

package config

var Session SessionConfig

type SessionConfig struct {
	Driver     string
	CookieName string
	MaxAge     int
	HttpOnly   bool
	Secure     bool
	SameSite   string
}

func loadSession() {
	Session = SessionConfig{
		Driver:     env("SESSION_DRIVER", "cookie"),
		CookieName: env("SESSION_COOKIE", "nimbus_session"),
		MaxAge:     envInt("SESSION_MAX_AGE", 604800),
		HttpOnly:   true,
		Secure:     env("APP_ENV", "development") == "production",
		SameSite:   "lax",
	}
}
`

// ── config/hash.go ─────────────────────────────────────────────
const configHash = `/*
|--------------------------------------------------------------------------
| Hashing Configuration
|--------------------------------------------------------------------------
|
| Algorithm and cost for password hashing.
|
*/

package config

var Hash HashConfig

type HashConfig struct {
	Driver     string
	BcryptCost int
}

func loadHash() {
	Hash = HashConfig{
		Driver:     env("HASH_DRIVER", "bcrypt"),
		BcryptCost: envInt("HASH_BCRYPT_COST", 10),
	}
}
`

// ── config/logger.go ───────────────────────────────────────────
const configLogger = `/*
|--------------------------------------------------------------------------
| Logger Configuration
|--------------------------------------------------------------------------
|
| Structured logging settings (backed by uber-go/zap).
|
*/

package config

var Logger LoggerConfig

type LoggerConfig struct {
	Level  string
	Format string
}

func loadLogger() {
	Logger = LoggerConfig{
		Level:  env("LOG_LEVEL", "info"),
		Format: env("LOG_FORMAT", "json"),
	}
}
`

// ── config/mail.go ─────────────────────────────────────────────
const configMail = `/*
|--------------------------------------------------------------------------
| Mail Configuration
|--------------------------------------------------------------------------
|
| SMTP driver settings for outbound email.
|
*/

package config

var Mail MailConfig

type MailConfig struct {
	Driver string
	SMTP   SMTPConfig
}

type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	FromName string
}

func loadMail() {
	Mail = MailConfig{
		Driver: env("MAIL_DRIVER", "smtp"),
		SMTP: SMTPConfig{
			Host:     env("SMTP_HOST", "localhost"),
			Port:     envInt("SMTP_PORT", 1025),
			Username: env("SMTP_USERNAME", ""),
			Password: env("SMTP_PASSWORD", ""),
			From:     env("MAIL_FROM", "noreply@example.com"),
			FromName: env("MAIL_FROM_NAME", "Nimbus App"),
		},
	}
}
`

// ── config/queue.go ────────────────────────────────────────────
const configQueue = `/*
|--------------------------------------------------------------------------
| Queue Configuration
|--------------------------------------------------------------------------
|
| Supported drivers: "sync", "redis", "database", "sqs", "kafka"
|
*/

package config

var Queue QueueConfig

type QueueConfig struct {
	Driver       string
	RedisURL     string
	SQSQueueURL  string
	KafkaBrokers string
	KafkaTopic   string
	KafkaGroupID string
}

func loadQueue() {
	Queue = QueueConfig{
		Driver:       env("QUEUE_DRIVER", "sync"),
		RedisURL:     env("REDIS_URL", ""),
		SQSQueueURL:  env("SQS_QUEUE_URL", ""),
		KafkaBrokers: env("KAFKA_BROKERS", ""),
		KafkaTopic:   env("KAFKA_TOPIC", "nimbus-queue"),
		KafkaGroupID: env("KAFKA_GROUP_ID", "nimbus-queue"),
	}
}
`

// ── config/storage.go ──────────────────────────────────────────
const configStorage = `/*
|--------------------------------------------------------------------------
| Storage / Drive Configuration
|--------------------------------------------------------------------------
|
| File storage driver for uploads and generated files.
|
*/

package config

var Storage StorageConfig

type StorageConfig struct {
	Driver string
	Local  LocalStorageConfig
}

type LocalStorageConfig struct {
	Root string
}

func loadStorage() {
	Storage = StorageConfig{
		Driver: env("STORAGE_DRIVER", "local"),
		Local: LocalStorageConfig{
			Root: env("STORAGE_ROOT", "storage"),
		},
	}
}
`

// ── config/static.go ───────────────────────────────────────────
const configStatic = `/*
|--------------------------------------------------------------------------
| Static Files Configuration
|--------------------------------------------------------------------------
|
| Serve static assets from the public/ directory.
|
*/

package config

var Static StaticConfig

type StaticConfig struct {
	Enabled bool
	Root    string
	Prefix  string
	MaxAge  int
}

func loadStatic() {
	Static = StaticConfig{
		Enabled: envBool("STATIC_ENABLED", true),
		Root:    env("STATIC_ROOT", "public"),
		Prefix:  env("STATIC_PREFIX", "/public"),
		MaxAge:  envInt("STATIC_MAX_AGE", 86400),
	}
}
`

// ── config/bodyparser.go ───────────────────────────────────────
const configBodyParser = `/*
|--------------------------------------------------------------------------
| Body Parser Configuration
|--------------------------------------------------------------------------
|
| Limits and allowed content types for incoming request bodies.
|
*/

package config

var BodyParser BodyParserConfig

type BodyParserConfig struct {
	JSONLimit      string
	FormLimit      string
	MultipartLimit string
	AllowedTypes   []string
}

func loadBodyParser() {
	BodyParser = BodyParserConfig{
		JSONLimit:      env("BODY_JSON_LIMIT", "1mb"),
		FormLimit:      env("BODY_FORM_LIMIT", "1mb"),
		MultipartLimit: env("BODY_MULTIPART_LIMIT", "10mb"),
		AllowedTypes:   []string{"application/json", "application/x-www-form-urlencoded", "multipart/form-data"},
	}
}
`

// ── config/limiter.go ──────────────────────────────────────────
const configLimiter = `/*
|--------------------------------------------------------------------------
| Rate Limiter Configuration
|--------------------------------------------------------------------------
|
| Throttle requests per client. KeyFunc: "ip" | "user" | "custom".
| Store: "memory" (single instance) or "redis" (distributed).
|
*/

package config

import "time"

var Limiter LimiterConfig

type LimiterConfig struct {
	Enabled       bool
	Requests      int
	Window        time.Duration
	KeyFunc       string
	Store         string
	RedisURL      string
	Headers       bool
	BlockDuration time.Duration
}

func loadLimiter() {
	window := time.Duration(envInt("RATE_LIMIT_WINDOW_SECONDS", 60)) * time.Second
	Limiter = LimiterConfig{
		Enabled:       envBool("RATE_LIMIT_ENABLED", true),
		Requests:      envInt("RATE_LIMIT_REQUESTS", 100),
		Window:        window,
		KeyFunc:       env("RATE_LIMIT_KEY", "ip"),
		Store:         env("RATE_LIMIT_STORE", "memory"),
		RedisURL:      env("REDIS_URL", ""),
		Headers:       envBool("RATE_LIMIT_HEADERS", true),
		BlockDuration: window,
	}
}
`

// ── config/cache.go ────────────────────────────────────────────
const configCache = `/*
|--------------------------------------------------------------------------
| Cache Configuration
|--------------------------------------------------------------------------
|
| Supported drivers: "memory", "redis", "memcached", "dynamodb"
|
*/

package config

import "time"

var Cache CacheConfig

type CacheConfig struct {
	Driver     string
	DefaultTTL time.Duration
}

func loadCache() {
	Cache = CacheConfig{
		Driver:     env("CACHE_DRIVER", "memory"),
		DefaultTTL: time.Duration(envInt("CACHE_TTL_MINUTES", 60)) * time.Minute,
	}
}
`

// ── config/shield.go ───────────────────────────────────────────
const configShield = `/*
|--------------------------------------------------------------------------
| Shield Configuration
|--------------------------------------------------------------------------
|
| Shield protects your app by setting security HTTP headers and
| providing CSRF protection.
|
| ExceptPaths: routes to exclude from CSRF validation (e.g. webhooks).
|
*/

package config

import "net/http"

var Shield ShieldConfig

type ShieldConfig struct {
	ContentTypeNosniff bool
	XSSProtection      string
	FrameGuard         string
	ReferrerPolicy     string
	CSRF               CSRFConfig
}

type CSRFConfig struct {
	Enabled     bool
	CookieName  string
	HeaderName  string
	FieldName   string
	MaxAge      int
	Secure      bool
	SameSite    http.SameSite
	Path        string
	HttpOnly    bool
	ExceptPaths []string
}

func loadShield() {
	isProd := env("APP_ENV", "development") == "production"
	Shield = ShieldConfig{
		ContentTypeNosniff: true,
		XSSProtection:      "0",
		FrameGuard:         "SAMEORIGIN",
		ReferrerPolicy:     "strict-origin-when-cross-origin",
		CSRF: CSRFConfig{
			Enabled:     envBool("CSRF_ENABLED", true),
			CookieName:  "__nimbus_csrf",
			HeaderName:  "X-CSRF-Token",
			FieldName:   "_csrf",
			MaxAge:      86400,
			Secure:      isProd,
			SameSite:    http.SameSiteLaxMode,
			Path:        "/",
			HttpOnly:    true,
			ExceptPaths: []string{},
		},
	}
}
`

// ── .env.example ───────────────────────────────────────────────
const envExample = `PORT=3333
APP_ENV=development
APP_NAME={{.AppName}}

DB_DRIVER=sqlite
DB_HOST=localhost
DB_PORT=
DB_USER=
DB_PASSWORD=
DB_DATABASE={{.AppName}}
DB_DSN=

QUEUE_DRIVER=sync
REDIS_URL=redis://localhost:6379
`

const migrationsRegistryStub = `package migrations

import "github.com/CodeSyncr/nimbus/database"

func All() []database.Migration {
	return []database.Migration{}
}
`

const layoutViewTmpl = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>{{ .title }}</title>
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link href="https://fonts.googleapis.com/css2?family=Fraunces:ital,opsz,wght@0,9..144,300;0,9..144,400;0,9..144,500;1,9..144,300;1,9..144,400&family=DM+Sans:wght@300;400;500&family=JetBrains+Mono:wght@400;500&display=swap" rel="stylesheet">
  <style>` + nimbusLandingPageCSS + `</style>
</head>
<body>

<nav>
  <a href="/" class="nav-logo">
    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
      <path d="M17.5 19H9a7 7 0 1 1 6.71-9h1.79a4.5 4.5 0 1 1 0 9Z"/>
    </svg>
    {{ if .appName }}{{ .appName }}{{ else }}Nimbus{{ end }}
    <span class="nav-badge">{{ if .version }}v{{ .version }}{{ else }}v1.0{{ end }}</span>
  </a>
  <div class="nav-links">
    <a href="/health" class="nav-link">Health</a>
    <a href="https://github.com/CodeSyncr/nimbus/tree/main/docs" target="_blank" rel="noopener" class="nav-link">Docs</a>
    <a href="https://github.com/CodeSyncr/nimbus" target="_blank" rel="noopener" class="nav-link primary">
      <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor"><path d="M12 2C6.477 2 2 6.484 2 12.017c0 4.425 2.865 8.18 6.839 9.504.5.092.682-.217.682-.483 0-.237-.008-.868-.013-1.703-2.782.605-3.369-1.343-3.369-1.343-.454-1.158-1.11-1.466-1.11-1.466-.908-.62.069-.608.069-.608 1.003.07 1.531 1.032 1.531 1.032.892 1.53 2.341 1.088 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.113-4.555-4.951 0-1.093.39-1.988 1.029-2.688-.103-.253-.446-1.272.098-2.65 0 0 .84-.27 2.75 1.026A9.564 9.564 0 0 1 12 6.844a9.59 9.59 0 0 1 2.504.337c1.909-1.296 2.747-1.027 2.747-1.027.546 1.379.202 2.398.1 2.651.64.7 1.028 1.595 1.028 2.688 0 3.848-2.339 4.695-4.566 4.943.359.309.678.92.678 1.855 0 1.338-.012 2.419-.012 2.747 0 .268.18.58.688.482A10.02 10.02 0 0 0 22 12.017C22 6.484 17.522 2 12 2Z"/></svg>
      GitHub
    </a>
  </div>
</nav>

<div class="wrapper">
  {{ .embed }}

  <footer>
    <div class="footer-inner">
      <p class="footer-text">
        Built with <a href="https://github.com/CodeSyncr/nimbus" target="_blank" rel="noopener">Nimbus</a>
        — Laravel-inspired framework for Go
      </p>
      <div class="footer-links">
        <a href="https://github.com/CodeSyncr/nimbus" target="_blank" rel="noopener">GitHub</a>
        <a href="/health">Health</a>
        <a href="https://github.com/CodeSyncr/nimbus/issues" target="_blank" rel="noopener">Issues</a>
      </div>
    </div>
  </footer>
</div>

<script>
document.addEventListener('DOMContentLoaded', function() {
  var observer = new IntersectionObserver(function(entries) {
    entries.forEach(function(e, i) {
      if (e.isIntersecting) {
        setTimeout(function() { e.target.classList.add('visible'); }, i * 80);
        observer.unobserve(e.target);
      }
    });
  }, { threshold: 0.12 });
  document.querySelectorAll('.fade-up').forEach(function(el) { observer.observe(el); });
});
</script>
</body>
</html>
`

// scaffoldLangENJSON is written to resources/lang/en.json for new apps.
// locale.BootFromEnv() (called from nimbus.New) loads resources/lang automatically.
const scaffoldLangENJSON = `{
  "app.welcome": "Welcome",
  "app.tagline": "Built with Nimbus"
}
`

// ── Starter kits (Basic) ─────────────────────────────────────────

const breezeKernelStub = `package start

import (
	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/errors"
	"github.com/CodeSyncr/nimbus/middleware"
	"github.com/CodeSyncr/nimbus/packages/shield"
	"github.com/CodeSyncr/nimbus/plugins/unpoly"
	"github.com/CodeSyncr/nimbus/session"

	"{{.AppName}}/config"
)

func RegisterMiddleware(app *nimbus.App) {
	store := session.NewMemoryStore()

	shieldCfg := shield.Config{
		ContentTypeNosniff: config.Shield.ContentTypeNosniff,
		XSSProtection:      config.Shield.XSSProtection,
		FrameGuard:         config.Shield.FrameGuard,
		ReferrerPolicy:     config.Shield.ReferrerPolicy,
		CSRF: shield.CSRFConfig{
			Enabled:    config.Shield.CSRF.Enabled,
			CookieName: config.Shield.CSRF.CookieName,
			HeaderName: config.Shield.CSRF.HeaderName,
			FieldName:  config.Shield.CSRF.FieldName,
			MaxAge:     config.Shield.CSRF.MaxAge,
			Secure:     config.Shield.CSRF.Secure,
			SameSite:   config.Shield.CSRF.SameSite,
			Path:       config.Shield.CSRF.Path,
			HttpOnly:   config.Shield.CSRF.HttpOnly,
			ExceptPaths: config.Shield.CSRF.ExceptPaths,
		},
	}

	app.Router.Use(
		middleware.Logger(),
		errors.Handler(),
		middleware.Recover(),
		shield.Guard(shieldCfg),
		shield.CSRFGuard(shieldCfg.CSRF),
		session.Middleware(session.Config{
			Store:      store,
			CookieName: "nimbus_session",
		}),
		unpoly.ServerProtocol(),
	)
}
`

// livewireKernelStub: session + Shield CSRF only (no Unpoly). Livewire plugin is registered in bin/server.go.
const livewireKernelStub = `package start

import (
	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/errors"
	"github.com/CodeSyncr/nimbus/middleware"
	"github.com/CodeSyncr/nimbus/packages/shield"
	"github.com/CodeSyncr/nimbus/session"

	"{{.AppName}}/config"
)

func RegisterMiddleware(app *nimbus.App) {
	store := session.NewMemoryStore()

	shieldCfg := shield.Config{
		ContentTypeNosniff: config.Shield.ContentTypeNosniff,
		XSSProtection:      config.Shield.XSSProtection,
		FrameGuard:         config.Shield.FrameGuard,
		ReferrerPolicy:     config.Shield.ReferrerPolicy,
		CSRF: shield.CSRFConfig{
			Enabled:     config.Shield.CSRF.Enabled,
			CookieName:  config.Shield.CSRF.CookieName,
			HeaderName:  config.Shield.CSRF.HeaderName,
			FieldName:   config.Shield.CSRF.FieldName,
			MaxAge:      config.Shield.CSRF.MaxAge,
			Secure:      config.Shield.CSRF.Secure,
			SameSite:    config.Shield.CSRF.SameSite,
			Path:        config.Shield.CSRF.Path,
			HttpOnly:    config.Shield.CSRF.HttpOnly,
			ExceptPaths: config.Shield.CSRF.ExceptPaths,
		},
	}

	app.Router.Use(
		middleware.Logger(),
		errors.Handler(),
		middleware.Recover(),
		shield.Guard(shieldCfg),
		shield.CSRFGuard(shieldCfg.CSRF),
		session.Middleware(session.Config{
			Store:      store,
			CookieName: "nimbus_session",
		}),
	)
}
`

// binServerLivewireTmpl is binServerTmpl + Livewire plugin registration.
const binServerLivewireTmpl = `package bin

import (
	"context"
	"fmt"
	"os"

	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/auth"
	"github.com/CodeSyncr/nimbus/cache"
	"github.com/CodeSyncr/nimbus/database"
	"github.com/CodeSyncr/nimbus-livewire/livewire"
	"github.com/CodeSyncr/nimbus/queue"
	"github.com/CodeSyncr/nimbus/view"
	"github.com/CodeSyncr/nimbus/lucid"

	"{{.AppName}}/config"
	"{{.AppName}}/database/migrations"
	"{{.AppName}}/start"
)

func Boot() *nimbus.App {
	config.Load()

	app := nimbus.New()

	// Register Livewire plugin (routes + template funcs + asset injection middleware).
	app.Use(livewire.New())

	// Basic apps keep their templates under resources/views.
	view.SetRoot("resources/views")

	bootCache()
	bootDatabase(app)
	bootQueue()
	bootAuth(app)

	start.RegisterMiddleware(app)
	start.RegisterRoutes(app)

	return app
}

func bootAuth(app *nimbus.App) {
	if config.AuthGuard == "stateless" {
		bootStatelessAuth(app)
	}
}

func bootStatelessAuth(app *nimbus.App) {
	var driver auth.TokenDriver
	if config.StatelessToken.Driver == "paseto" {
		driver = auth.NewPasetoDriver(config.StatelessToken.Secret)
	} else {
		driver = auth.NewJWTDriver(config.StatelessToken.Secret)
	}

	guard := auth.NewStatelessGuard(driver, auth.UserLoaderFunc(func(ctx context.Context, id string) (auth.User, error) {
		// Load the user for the token subject (e.g. after make:auth):
		//   var u models.User
		//   if err := database.Get().First(&u, "id = ?", id).Error; err != nil {
		//     return nil, err
		//   }
		//   return &u, nil
		_ = ctx
		_ = id
		return nil, fmt.Errorf("stateless auth: implement UserLoader in bootStatelessAuth (see comment above)")
	}))

	app.Container.Singleton("auth.stateless", func() *auth.StatelessGuard {
		return guard
	})
}

func bootCache() {
	cache.Boot(nil)
}

func bootDatabase(app *nimbus.App) {
	db, err := database.ConnectWithConfig(database.ConnectConfig{
		Driver: config.Database.Driver,
		DSN:    config.Database.DSN,
		Debug:  config.App.Env == "development",
		PoolConfig: database.PoolConfigFromFields(
			config.Database.MaxOpenConns,
			config.Database.MaxIdleConns,
			config.Database.ConnMaxLifetime,
			config.Database.ConnMaxIdleTime,
		),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Database connection failed: %v\n", err)
		os.Exit(1)
	}

	// Keep global DB in sync for packages/components using nimbus.GetDB().
	nimbus.SetDB(db)

	app.Container.Singleton("db", func() *lucid.DB {
		return db
	})
}

func bootQueue() {
	queue.Boot(&queue.BootConfig{RegisterJobs: start.RegisterQueueJobs})
}

func RunQueueWorker() {
	app := Boot()
	if err := app.Boot(); err != nil {
		fmt.Fprintf(os.Stderr, "Boot failed: %v\n", err)
		os.Exit(1)
	}
	ctx := context.Background()
	queue.RunWorker(ctx, "default")
}

func RunMigrations() {
	config.Load()
	if err := database.CreateDatabaseIfNotExists(database.CreateConfig{
		Driver:   config.Database.Driver,
		Host:     config.Database.Host,
		Port:     config.Database.Port,
		User:     config.Database.User,
		Password: config.Database.Password,
		Database: config.Database.Database,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Database create failed: %v\n", err)
		os.Exit(1)
	}
	db, err := database.ConnectWithConfig(database.ConnectConfig{
		Driver: config.Database.Driver,
		DSN:    config.Database.DSN,
		Debug:  config.App.Env == "development",
		PoolConfig: database.PoolConfigFromFields(
			config.Database.MaxOpenConns,
			config.Database.MaxIdleConns,
			config.Database.ConnMaxLifetime,
			config.Database.ConnMaxIdleTime,
		),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Database connection failed: %v\n", err)
		os.Exit(1)
	}
	migrator := database.NewMigrator(db, migrations.All())
	if err := migrator.Up(); err != nil {
		fmt.Fprintf(os.Stderr, "Migration failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Migrations completed.")
}

func RunDbCreate() {
	config.Load()
	if err := database.CreateDatabaseIfNotExists(database.CreateConfig{
		Driver:   config.Database.Driver,
		Host:     config.Database.Host,
		Port:     config.Database.Port,
		User:     config.Database.User,
		Password: config.Database.Password,
		Database: config.Database.Database,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Database create failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Database created (or already exists).")
}
`

// starterRoutesTmpl: Jetstream block adds 2FA routes only when .Jetstream is true (avoids missing package on Breeze/Livewire).
const starterRoutesTmpl = `package start

import (
	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/http"

	"{{.AppName}}/app/controllers"
	authc "{{.AppName}}/app/controllers/auth"
	"{{.AppName}}/app/middleware"
{{- if .Jetstream}}

	"{{.AppName}}/app/controllers/settings"
{{- end}}
)

func RegisterRoutes(app *nimbus.App) {
	db := app.Container.MustMake("db").(*nimbus.DB)

	middleware.Guard = controllers.NewSessionGuard(db)

	auth := &authc.AuthController{DB: db, Guard: middleware.Guard}

	app.Router.Get("/", func(c *http.Context) error {
		c.Redirect(http.StatusFound, "/dashboard")
		return nil
	})

	guest := app.Router.Group("", middleware.Guest())
	guest.Get("/login", auth.LoginForm)
	guest.Post("/login", auth.Login)
	guest.Get("/register", auth.RegisterForm)
	guest.Post("/register", auth.Register)

	protected := app.Router.Group("", middleware.Authenticate())
	protected.Get("/dashboard", (&controllers.Dashboard{}).Index)
	protected.Get("/profile", (&controllers.Profile{}).Show)
	protected.Post("/logout", auth.Logout)
{{- if .Jetstream }}

	tf := &settings.TwoFactor{DB: db}
	protected.Get("/settings/two-factor", tf.Show)
	protected.Post("/settings/two-factor/enable", tf.Enable)
	protected.Post("/settings/two-factor/disable", tf.Disable)
{{- end }}
}
`

const breezeAuthControllerTmpl = `package auth

import (
	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/auth"
	"github.com/CodeSyncr/nimbus/http"

	"{{.AppName}}/app/models"
)

// AuthController handles login, register, logout.
type AuthController struct {
	DB    *nimbus.DB
	Guard auth.Guard
}

func (c *AuthController) LoginForm(ctx *http.Context) error {
	return ctx.View("auth/login", map[string]any{"title": "Login"})
}

func (c *AuthController) Login(ctx *http.Context) error {
	email := ctx.Request.FormValue("email")
	password := ctx.Request.FormValue("password")
	if email == "" || password == "" {
		return ctx.View("auth/login", map[string]any{"title": "Login", "error": "Email and password required"})
	}
	var u models.User
	if err := c.DB.Where("email = ?", email).First(&u).Error; err != nil {
		return ctx.View("auth/login", map[string]any{"title": "Login", "error": "Invalid credentials"})
	}
	if !u.CheckPassword(password) {
		return ctx.View("auth/login", map[string]any{"title": "Login", "error": "Invalid credentials"})
	}
	if c.Guard != nil {
		_ = c.Guard.Login(ctx.Request.Context(), &u)
	}
	ctx.Redirect(http.StatusFound, "/dashboard")
	return nil
}

func (c *AuthController) RegisterForm(ctx *http.Context) error {
	return ctx.View("auth/register", map[string]any{"title": "Register"})
}

func (c *AuthController) Register(ctx *http.Context) error {
	email := ctx.Request.FormValue("email")
	password := ctx.Request.FormValue("password")
	if email == "" || password == "" {
		return ctx.View("auth/register", map[string]any{"title": "Register", "error": "Email and password required"})
	}
	u := models.User{Email: email}
	if err := u.SetPassword(password); err != nil {
		return ctx.View("auth/register", map[string]any{"title": "Register", "error": "Failed to hash password"})
	}
	if err := c.DB.Create(&u).Error; err != nil {
		return ctx.View("auth/register", map[string]any{"title": "Register", "error": "Email already exists"})
	}
	if c.Guard != nil {
		_ = c.Guard.Login(ctx.Request.Context(), &u)
	}
	ctx.Redirect(http.StatusFound, "/dashboard")
	return nil
}

func (c *AuthController) Logout(ctx *http.Context) error {
	if c.Guard != nil {
		_ = c.Guard.Logout(ctx.Request.Context())
	}
	ctx.Redirect(http.StatusFound, "/login")
	return nil
}
`

const breezeUserModelTmpl = `package models

import (
	"context"
	"fmt"

	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/auth"
	"github.com/CodeSyncr/nimbus/database"
	"github.com/CodeSyncr/nimbus/hash"
)

// User implements auth.User for session-based authentication.
type User struct {
	database.Model
	Email        string
	PasswordHash string
}

func (u *User) GetID() string { return fmt.Sprintf("%d", u.ID) }

func (u *User) SetPassword(plain string) error {
	h, err := hash.Make(plain)
	if err != nil {
		return err
	}
	u.PasswordHash = h
	return nil
}

func (u *User) CheckPassword(plain string) bool {
	return hash.Check(plain, u.PasswordHash)
}

// UserByID loads a user for auth.SessionGuard.
func UserByID(db *nimbus.DB) auth.UserLoaderFunc {
	return func(ctx context.Context, id string) (auth.User, error) {
		var u User
		if err := db.WithContext(ctx).First(&u, "id = ?", id).Error; err != nil {
			return nil, nil
		}
		return &u, nil
	}
}
`

const breezeControllersTmpl = `package controllers

import (
	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/auth"
	"github.com/CodeSyncr/nimbus/http"

	"{{.AppName}}/app/models"
)

func NewSessionGuard(db *nimbus.DB) *auth.SessionGuard {
	return auth.NewSessionGuardWithLoader(models.UserByID(db))
}

type Dashboard struct{}

func (d *Dashboard) Index(c *http.Context) error {
	u := auth.UserFromContext(c.Request.Context())
	return c.View("dashboard", map[string]any{
		"title": "Dashboard",
		"user":  u,
	})
}

type Profile struct{}

func (p *Profile) Show(c *http.Context) error {
	u := auth.UserFromContext(c.Request.Context())
	return c.View("profile", map[string]any{
		"title": "Profile",
		"user":  u,
	})
}
`

const breezeGuestMiddlewareTmpl = `package middleware

import (
	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

// Guest redirects authenticated users away from auth pages.
func Guest() router.Middleware {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *http.Context) error {
			if Guard != nil {
				u, _ := Guard.User(c.Request.Context())
				if u != nil {
					c.Redirect(http.StatusFound, "/dashboard")
					return nil
				}
			}
			return next(c)
		}
	}
}
`

const breezeViewsLayout = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width,initial-scale=1"/>
  <title>{{ .title }}</title>
  <style>
    :root{--bg:#0b0d10;--panel:#11141a;--muted:#7b8290;--text:#e9edf4;--accent:#00e5b0;--border:rgba(255,255,255,.08);}
    *{box-sizing:border-box} body{margin:0;font-family:ui-sans-serif,system-ui;background:var(--bg);color:var(--text)}
    a{color:inherit;text-decoration:none} .wrap{max-width:980px;margin:0 auto;padding:28px}
    .top{display:flex;align-items:center;justify-content:space-between;margin-bottom:18px}
    .brand{font-weight:800;letter-spacing:.02em}
    .nav{display:flex;gap:10px;align-items:center}
    .btn{display:inline-flex;align-items:center;gap:8px;border:1px solid var(--border);background:var(--panel);color:var(--text);padding:8px 12px;border-radius:10px}
    .btn.primary{border-color:rgba(0,229,176,.25);background:rgba(0,229,176,.08);color:var(--accent)}
    .card{background:var(--panel);border:1px solid var(--border);border-radius:14px;padding:18px}
    .field{display:flex;flex-direction:column;gap:6px;margin:10px 0}
    input{background:#0e1015;border:1px solid var(--border);border-radius:10px;padding:10px 12px;color:var(--text)}
    .muted{color:var(--muted);font-size:13px}
    .err{color:#ff4d6a;margin-top:10px}
  </style>
</head>
<body>
  <div class="wrap">
    <div class="top">
      <div class="brand">{{ if .appName }}{{ .appName }}{{ else }}Nimbus{{ end }}</div>
      <div class="nav">
        <a class="btn" href="/dashboard" up-follow>Dashboard</a>
        <a class="btn" href="/profile" up-follow>Profile</a>
        <form method="POST" action="/logout" style="margin:0;" up-submit>
          {{ .csrfField }}
          <button class="btn" type="submit">Logout</button>
        </form>
      </div>
    </div>
    {{ .embed }}
  </div>
</body>
</html>`

const breezeViewLogin = `@layout('layouts/app')
<div class="card">
  <h2>Login</h2>
  <p class="muted">Sign in to your account.</p>
  @if(.error)<div class="err">\{{ .error }}</div>@endif
  <form method="POST" action="/login" up-submit>
    {{ .csrfField }}
    <div class="field"><label>Email</label><input type="email" name="email" required></div>
    <div class="field"><label>Password</label><input type="password" name="password" required></div>
    <button class="btn primary" type="submit">Login</button>
  </form>
  <p class="muted" style="margin-top:12px;">No account? <a class="btn" href="/register" up-follow>Register</a></p>
</div>`

const breezeViewRegister = `@layout('layouts/app')
<div class="card">
  <h2>Create account</h2>
  <p class="muted">Register to get started.</p>
  @if(.error)<div class="err">\{{ .error }}</div>@endif
  <form method="POST" action="/register" up-submit>
    {{ .csrfField }}
    <div class="field"><label>Email</label><input type="email" name="email" required></div>
    <div class="field"><label>Password</label><input type="password" name="password" required></div>
    <button class="btn primary" type="submit">Register</button>
  </form>
  <p class="muted" style="margin-top:12px;">Already registered? <a class="btn" href="/login" up-follow>Login</a></p>
</div>`

const breezeViewDashboard = `@layout('layouts/app')
<div class="card">
  <h2>Dashboard</h2>
  <p class="muted">You’re logged in.</p>
  <div class="muted" style="margin-top:12px;">User: \{{ if .user }}\{{ .user.GetID }}\{{ end }}</div>
</div>`

const breezeViewProfile = `@layout('layouts/app')
<div class="card">
  <h2>Profile</h2>
  <p class="muted">Account information.</p>
  <div style="margin-top:12px;" class="muted">This page is a scaffold. Add profile update actions here.</div>
</div>`

// Livewire stack: view paths under resources/views/pages/ (see Laravel Livewire v4 pages:: namespace).
const livewireAuthControllerTmpl = `package auth

import (
	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/auth"
	"github.com/CodeSyncr/nimbus/http"

	"{{.AppName}}/app/models"
)

// AuthController handles login, register, logout.
type AuthController struct {
	DB    *nimbus.DB
	Guard auth.Guard
}

func (c *AuthController) LoginForm(ctx *http.Context) error {
	return ctx.View("pages/auth/login", map[string]any{"title": "Login"})
}

func (c *AuthController) Login(ctx *http.Context) error {
	email := ctx.Request.FormValue("email")
	password := ctx.Request.FormValue("password")
	if email == "" || password == "" {
		return ctx.View("pages/auth/login", map[string]any{"title": "Login", "error": "Email and password required"})
	}
	var u models.User
	if err := c.DB.Where("email = ?", email).First(&u).Error; err != nil {
		return ctx.View("pages/auth/login", map[string]any{"title": "Login", "error": "Invalid credentials"})
	}
	if !u.CheckPassword(password) {
		return ctx.View("pages/auth/login", map[string]any{"title": "Login", "error": "Invalid credentials"})
	}
	if c.Guard != nil {
		_ = c.Guard.Login(ctx.Request.Context(), &u)
	}
	ctx.Redirect(http.StatusFound, "/dashboard")
	return nil
}

func (c *AuthController) RegisterForm(ctx *http.Context) error {
	return ctx.View("pages/auth/register", map[string]any{"title": "Register"})
}

func (c *AuthController) Register(ctx *http.Context) error {
	email := ctx.Request.FormValue("email")
	password := ctx.Request.FormValue("password")
	if email == "" || password == "" {
		return ctx.View("pages/auth/register", map[string]any{"title": "Register", "error": "Email and password required"})
	}
	u := models.User{Email: email}
	if err := u.SetPassword(password); err != nil {
		return ctx.View("pages/auth/register", map[string]any{"title": "Register", "error": "Failed to hash password"})
	}
	if err := c.DB.Create(&u).Error; err != nil {
		return ctx.View("pages/auth/register", map[string]any{"title": "Register", "error": "Email already exists"})
	}
	if c.Guard != nil {
		_ = c.Guard.Login(ctx.Request.Context(), &u)
	}
	ctx.Redirect(http.StatusFound, "/dashboard")
	return nil
}

func (c *AuthController) Logout(ctx *http.Context) error {
	if c.Guard != nil {
		_ = c.Guard.Logout(ctx.Request.Context())
	}
	ctx.Redirect(http.StatusFound, "/login")
	return nil
}
`

const livewireControllersTmpl = `package controllers

import (
	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/auth"
	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus-livewire/livewire"

	"{{.AppName}}/app/models"
)

func NewSessionGuard(db *nimbus.DB) *auth.SessionGuard {
	return auth.NewSessionGuardWithLoader(models.UserByID(db))
}

type Dashboard struct{}

func (d *Dashboard) Index(c *http.Context) error {
	u := auth.UserFromContext(c.Request.Context())
	counter, _ := livewire.Render("counter", nil)
	return c.View("pages/dashboard", map[string]any{
		"title": "Dashboard",
		"user":  u,
		"counter": counter,
	})
}

type Profile struct{}

func (p *Profile) Show(c *http.Context) error {
	u := auth.UserFromContext(c.Request.Context())
	return c.View("pages/profile", map[string]any{
		"title": "Profile",
		"user":  u,
	})
}
`

const livewireViewsLayout = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width,initial-scale=1"/>
  <title>{{ .title }}</title>
  <!-- Layout mirrors Laravel Livewire v4. Alpine is bundled with {{ livewireScripts }} (nimbus-livewire npm build); do not add a second Alpine CDN script. -->
  <style>
    :root{--bg:#0b0d10;--panel:#11141a;--muted:#7b8290;--text:#e9edf4;--accent:#00e5b0;--border:rgba(255,255,255,.08);}
    *{box-sizing:border-box} body{margin:0;font-family:ui-sans-serif,system-ui;background:var(--bg);color:var(--text)}
    a{color:inherit;text-decoration:none} .wrap{max-width:980px;margin:0 auto;padding:28px}
    .top{display:flex;align-items:center;justify-content:space-between;margin-bottom:18px;gap:12px;flex-wrap:wrap}
    .brand{font-weight:800;letter-spacing:.02em}
    .nav{display:flex;gap:10px;align-items:center;flex-wrap:wrap}
    .btn{display:inline-flex;align-items:center;gap:8px;border:1px solid var(--border);background:var(--panel);color:var(--text);padding:8px 12px;border-radius:10px}
    .btn.primary{border-color:rgba(0,229,176,.25);background:rgba(0,229,176,.08);color:var(--accent)}
    .card{background:var(--panel);border:1px solid var(--border);border-radius:14px;padding:18px}
    .field{display:flex;flex-direction:column;gap:6px;margin:10px 0}
    input{background:#0e1015;border:1px solid var(--border);border-radius:10px;padding:10px 12px;color:var(--text)}
    .muted{color:var(--muted);font-size:13px}
    .err{color:#ff4d6a;margin-top:10px}
  </style>
  {{ livewireStyles }}
</head>
<body>
  <div class="wrap">
    <div class="top">
      <div class="brand">{{ if .appName }}{{ .appName }}{{ else }}Nimbus{{ end }}</div>
      <div class="nav">
        <a class="btn" href="/dashboard">Dashboard</a>
        <a class="btn" href="/profile">Profile</a>
        <form method="POST" action="/logout" style="margin:0;">
          {{ .csrfField }}
          <button class="btn" type="submit">Logout</button>
        </form>
      </div>
    </div>
    {{ .embed }}
  </div>
  {{ livewireScripts }}
</body>
</html>`

const livewireViewLogin = `@layout('layouts/app')
<div class="card" x-data="{ sending: false }">
  <h2>Login</h2>
  <p class="muted">Sign in to your account.</p>
  @if(.error)<div class="err">\{{ .error }}</div>@endif
  <form method="POST" action="/login" x-on:submit="sending = true" x-bind:aria-busy="sending">
    {{ .csrfField }}
    <div class="field"><label>Email</label><input type="email" name="email" required autocomplete="username"></div>
    <div class="field"><label>Password</label><input type="password" name="password" required autocomplete="current-password"></div>
    <button class="btn primary" type="submit" x-bind:disabled="sending">Login</button>
  </form>
  <p class="muted" style="margin-top:12px;">No account? <a class="btn" href="/register">Register</a></p>
</div>`

const livewireViewRegister = `@layout('layouts/app')
<div class="card" x-data="{ sending: false }">
  <h2>Create account</h2>
  <p class="muted">Register to get started.</p>
  @if(.error)<div class="err">\{{ .error }}</div>@endif
  <form method="POST" action="/register" x-on:submit="sending = true" x-bind:aria-busy="sending">
    {{ .csrfField }}
    <div class="field"><label>Email</label><input type="email" name="email" required autocomplete="username"></div>
    <div class="field"><label>Password</label><input type="password" name="password" required autocomplete="new-password"></div>
    <button class="btn primary" type="submit" x-bind:disabled="sending">Register</button>
  </form>
  <p class="muted" style="margin-top:12px;">Already registered? <a class="btn" href="/login">Login</a></p>
</div>`

const livewireViewDashboard = `@layout('layouts/app')
<div class="card">
  <h2>Dashboard</h2>
  <p class="muted">You’re logged in.</p>
  <div class="muted" style="margin-top:12px;">User: \{{ if .user }}\{{ .user.GetID }}\{{ end }}</div>
</div>`

const livewireViewProfile = `@layout('layouts/app')
<div class="card">
  <h2>Profile</h2>
  <p class="muted">Account information.</p>
  <div style="margin-top:12px;" class="muted">This page is a scaffold. Add profile update actions here.</div>
</div>`

const livewireHomeStub = `@layout('layouts/app')
<div class="card">
  <h2>Welcome</h2>
  <p class="muted">Livewire starter: views live under <code style="color:var(--accent)">resources/views/pages/</code> like Laravel Livewire v4 <code>pages::</code> components.</p>
  <p class="muted" style="margin-top:10px;">Go to <a class="btn primary" href="/dashboard">Dashboard</a> or <a class="btn" href="/login">Login</a>.</p>
</div>`

const livewireViewDashboardWithComponent = `@layout('layouts/app')
<div style="display:grid; grid-template-columns: 1fr; gap: 14px;">
  <div class="card">
    <h2 style="margin-top:0;">Dashboard</h2>
    <p class="muted">You’re logged in.</p>
    <div class="muted" style="margin-top:12px;">User: {{ if .user }}{{ .user.GetID }}{{ end }}</div>
  </div>
  {{ raw .counter }}
</div>`

const breezeMigrationUsers = `package migrations

import (
	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/database/schema"
)

type CreateUsers struct{ schema.BaseSchema }

func (m *CreateUsers) TableName() string { return "users" }

func (m *CreateUsers) Up(db *nimbus.DB) error {
	return schema.New(db).CreateTable("users", func(t *schema.Table) {
		t.Increments("id")
		t.String("email", 255).Unique()
		t.String("password_hash", 255)
		t.Timestamps()
		t.SoftDeletes()
	})
}

func (m *CreateUsers) Down(db *nimbus.DB) error {
	return schema.New(db).DropTable("users")
}
`

const breezeMigrationsRegistry = `package migrations

import "github.com/CodeSyncr/nimbus/database"

func All() []database.Migration {
	createUsers := &CreateUsers{}
	return []database.Migration{
		{Name: "20260325000000_create_users", Up: createUsers.Up, Down: createUsers.Down},
	}
}
`

const jetstreamUserModelTmpl = `package models

import (
	"context"
	"fmt"
	"time"

	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/auth"
	"github.com/CodeSyncr/nimbus/database"
	"github.com/CodeSyncr/nimbus/hash"
)

// User implements auth.User for session-based authentication.
type User struct {
	database.Model
	Email        string
	PasswordHash string

	TwoFactorSecret        string     ` + "`json:\"two_factor_secret\"`" + `
	TwoFactorRecoveryCodes string     ` + "`json:\"two_factor_recovery_codes\"`" + `
	TwoFactorConfirmedAt   *time.Time ` + "`json:\"two_factor_confirmed_at\"`" + `
}

func (u *User) GetID() string { return fmt.Sprintf("%d", u.ID) }

func (u *User) SetPassword(plain string) error {
	h, err := hash.Make(plain)
	if err != nil {
		return err
	}
	u.PasswordHash = h
	return nil
}

func (u *User) CheckPassword(plain string) bool {
	return hash.Check(plain, u.PasswordHash)
}

// UserByID loads a user for auth.SessionGuard.
func UserByID(db *nimbus.DB) auth.UserLoaderFunc {
	return func(ctx context.Context, id string) (auth.User, error) {
		var u User
		if err := db.WithContext(ctx).First(&u, "id = ?", id).Error; err != nil {
			return nil, nil
		}
		return &u, nil
	}
}
`

const jetstreamMigrationTwoFactor = `package migrations

import (
	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/database/schema"
)

type AddTwoFactorToUsers struct{ schema.BaseSchema }

func (m *AddTwoFactorToUsers) TableName() string { return "users" }

func (m *AddTwoFactorToUsers) Up(db *nimbus.DB) error {
	return schema.New(db).AlterTable("users", func(t *schema.Table) {
		t.String("two_factor_secret", 255).Nullable()
		t.Text("two_factor_recovery_codes").Nullable()
		t.Timestamp("two_factor_confirmed_at").Nullable()
	})
}

func (m *AddTwoFactorToUsers) Down(db *nimbus.DB) error { return nil }
`

const jetstreamMigrationsRegistry = `package migrations

import "github.com/CodeSyncr/nimbus/database"

func All() []database.Migration {
	createUsers := &CreateUsers{}
	twoFactor := &AddTwoFactorToUsers{}
	return []database.Migration{
		{Name: "20260325000000_create_users", Up: createUsers.Up, Down: createUsers.Down},
		{Name: "20260325000001_add_two_factor_to_users", Up: twoFactor.Up, Down: twoFactor.Down},
	}
}
`

const jetstreamTwoFactorControllerTmpl = `package settings

import (
	"crypto/rand"
	"encoding/base64"

	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/auth"
	"github.com/CodeSyncr/nimbus/http"

	"{{.AppName}}/app/models"
)

type TwoFactor struct {
	DB *nimbus.DB
}

func (t *TwoFactor) Show(c *http.Context) error {
	u, _ := auth.UserFromContext(c.Request.Context()).(*models.User)
	return c.View("settings/two_factor", map[string]any{"title": "Two-factor", "user": u})
}

func (t *TwoFactor) Enable(c *http.Context) error {
	u, _ := auth.UserFromContext(c.Request.Context()).(*models.User)
	if u == nil {
		c.Redirect(http.StatusFound, "/login")
		return nil
	}
	if u.TwoFactorSecret == "" {
		b := make([]byte, 24)
		_, _ = rand.Read(b)
		u.TwoFactorSecret = base64.RawStdEncoding.EncodeToString(b)
		_ = t.DB.Save(u).Error
	}
	c.Redirect(http.StatusFound, "/settings/two-factor")
	return nil
}

func (t *TwoFactor) Disable(c *http.Context) error {
	u, _ := auth.UserFromContext(c.Request.Context()).(*models.User)
	if u == nil {
		c.Redirect(http.StatusFound, "/login")
		return nil
	}
	u.TwoFactorSecret = ""
	u.TwoFactorRecoveryCodes = ""
	u.TwoFactorConfirmedAt = nil
	_ = t.DB.Save(u).Error
	c.Redirect(http.StatusFound, "/settings/two-factor")
	return nil
}
`

const jetstreamViewTwoFactor = `@layout('layouts/app')
<div class="card">
  <h2>Two-factor authentication</h2>
  <p class="muted">Jetstream-style scaffold. Integrate a real TOTP flow later.</p>
  <div style="margin-top:12px;">
    @if(and .user .user.TwoFactorSecret)
      <div class="muted">Status: Enabled</div>
      <form method="POST" action="/settings/two-factor/disable" up-submit style="margin-top:10px;">
        {{ .csrfField }}
        <button class="btn" type="submit">Disable</button>
      </form>
    @else
      <div class="muted">Status: Disabled</div>
      <form method="POST" action="/settings/two-factor/enable" up-submit style="margin-top:10px;">
        {{ .csrfField }}
        <button class="btn primary" type="submit">Enable</button>
      </form>
    @endif
  </div>
</div>`

// applyStarterKit writes additional files + overrides stubs for Basic apps.
func applyStarterKit(dir, appName, starter string, teams bool) error {
	starter = strings.TrimSpace(strings.ToLower(starter))
	if starter == "" || starter == "none" {
		return nil
	}
	if starter != "breeze" && starter != "jetstream" && starter != "livewire" {
		return nil
	}

	lw := starter == "livewire"
	jet := starter == "jetstream"

	_ = os.MkdirAll(filepath.Join(dir, "app", "controllers", "auth"), 0755)
	_ = os.MkdirAll(filepath.Join(dir, "resources", "views", "layouts"), 0755)
	_ = os.MkdirAll(filepath.Join(dir, "app", "middleware"), 0755)
	// Remove default root layout; starter kits use resources/views/layouts/app.nimbus.
	_ = os.Remove(filepath.Join(dir, "resources", "views", "layout.nimbus"))

	// Overwrite kernel: Breeze/Jetstream use Unpoly; Livewire stack does not.
	var kb strings.Builder
	if lw {
		kernelLW := template.Must(template.New("livewire_kernel").Parse(livewireKernelStub))
		_ = kernelLW.Execute(&kb, map[string]string{"AppName": appName})
	} else {
		kernelT := template.Must(template.New("breeze_kernel").Parse(breezeKernelStub))
		_ = kernelT.Execute(&kb, map[string]string{"AppName": appName})
	}
	_ = os.WriteFile(filepath.Join(dir, "start", "kernel.go"), []byte(kb.String()), 0644)

	var rb strings.Builder
	routesT := template.Must(template.New("starter_routes").Parse(starterRoutesTmpl))
	_ = routesT.Execute(&rb, map[string]any{"AppName": appName, "Jetstream": jet})
	_ = os.WriteFile(filepath.Join(dir, "start", "routes.go"), []byte(rb.String()), 0644)

	// Controllers/models/migrations.
	var cb strings.Builder
	var bcb strings.Builder
	if lw {
		ctrlLW := template.Must(template.New("livewire_auth").Parse(livewireAuthControllerTmpl))
		_ = ctrlLW.Execute(&cb, map[string]string{"AppName": appName})
		baseLW := template.Must(template.New("livewire_controllers").Parse(livewireControllersTmpl))
		_ = baseLW.Execute(&bcb, map[string]string{"AppName": appName})
	} else {
		ctrlT := template.Must(template.New("breeze_auth_controller").Parse(breezeAuthControllerTmpl))
		_ = ctrlT.Execute(&cb, map[string]string{"AppName": appName})
		baseCtrlT := template.Must(template.New("breeze_controllers").Parse(breezeControllersTmpl))
		_ = baseCtrlT.Execute(&bcb, map[string]string{"AppName": appName})
	}
	_ = os.WriteFile(filepath.Join(dir, "app", "controllers", "auth", "auth_controller.go"), []byte(cb.String()), 0644)
	_ = os.WriteFile(filepath.Join(dir, "app", "controllers", "starter.go"), []byte(bcb.String()), 0644)

	_ = os.WriteFile(filepath.Join(dir, "app", "models", "user.go"), []byte(breezeUserModelTmpl), 0644)
	_ = os.WriteFile(filepath.Join(dir, "app", "middleware", "guest.go"), []byte(breezeGuestMiddlewareTmpl), 0644)

	_ = os.WriteFile(filepath.Join(dir, "database", "migrations", "20260325000000_create_users.go"), []byte(breezeMigrationUsers), 0644)
	_ = os.WriteFile(filepath.Join(dir, "database", "migrations", "registry.go"), []byte(breezeMigrationsRegistry), 0644)

	if lw {
		_ = os.MkdirAll(filepath.Join(dir, "resources", "views", "pages", "auth"), 0755)
		_ = os.WriteFile(filepath.Join(dir, "resources", "views", "layouts", "app.nimbus"), []byte(livewireViewsLayout), 0644)
		_ = os.WriteFile(filepath.Join(dir, "resources", "views", "pages", "auth", "login.nimbus"), []byte(livewireViewLogin), 0644)
		_ = os.WriteFile(filepath.Join(dir, "resources", "views", "pages", "auth", "register.nimbus"), []byte(livewireViewRegister), 0644)
		_ = os.WriteFile(filepath.Join(dir, "resources", "views", "pages", "dashboard.nimbus"), []byte(livewireViewDashboardWithComponent), 0644)
		_ = os.WriteFile(filepath.Join(dir, "resources", "views", "pages", "profile.nimbus"), []byte(livewireViewProfile), 0644)
		_ = os.WriteFile(filepath.Join(dir, "resources", "views", "home.nimbus"), []byte(livewireHomeStub), 0644)

		// Ensure the Livewire plugin is registered (routes + template funcs + script endpoint).
		var sb strings.Builder
		srvT := template.Must(template.New("bin_server_livewire").Parse(binServerLivewireTmpl))
		_ = srvT.Execute(&sb, map[string]string{"AppName": appName})
		_ = os.WriteFile(filepath.Join(dir, "bin", "server.go"), []byte(sb.String()), 0644)
	} else {
		_ = os.MkdirAll(filepath.Join(dir, "resources", "views", "auth"), 0755)
		_ = os.WriteFile(filepath.Join(dir, "resources", "views", "layouts", "app.nimbus"), []byte(breezeViewsLayout), 0644)
		_ = os.WriteFile(filepath.Join(dir, "resources", "views", "auth", "login.nimbus"), []byte(breezeViewLogin), 0644)
		_ = os.WriteFile(filepath.Join(dir, "resources", "views", "auth", "register.nimbus"), []byte(breezeViewRegister), 0644)
		_ = os.WriteFile(filepath.Join(dir, "resources", "views", "dashboard.nimbus"), []byte(breezeViewDashboard), 0644)
		_ = os.WriteFile(filepath.Join(dir, "resources", "views", "profile.nimbus"), []byte(breezeViewProfile), 0644)
		_ = os.WriteFile(filepath.Join(dir, "resources", "views", "home.nimbus"), []byte(`@layout('layouts/app')
<div class="card">
  <h2>Welcome</h2>
  <p class="muted">Breeze starter: Unpoly partial navigation. Go to <a class="btn primary" href="/dashboard" up-follow>Dashboard</a>.</p>
  <p class="muted" style="margin-top:10px;">Not logged in? <a class="btn" href="/login" up-follow>Login</a></p>
</div>`), 0644)
	}

	if starter == "jetstream" {
		_ = os.MkdirAll(filepath.Join(dir, "app", "controllers", "settings"), 0755)
		_ = os.MkdirAll(filepath.Join(dir, "resources", "views", "settings"), 0755)

		// Override user model to include Jetstream fields.
		_ = os.WriteFile(filepath.Join(dir, "app", "models", "user.go"), []byte(jetstreamUserModelTmpl), 0644)

		// Add 2FA migration + registry.
		_ = os.WriteFile(filepath.Join(dir, "database", "migrations", "20260325000001_add_two_factor_to_users.go"), []byte(jetstreamMigrationTwoFactor), 0644)
		_ = os.WriteFile(filepath.Join(dir, "database", "migrations", "registry.go"), []byte(jetstreamMigrationsRegistry), 0644)

		// Add settings controller + view.
		tfT := template.Must(template.New("jetstream_two_factor").Parse(jetstreamTwoFactorControllerTmpl))
		var tfb strings.Builder
		_ = tfT.Execute(&tfb, map[string]string{"AppName": appName})
		_ = os.WriteFile(filepath.Join(dir, "app", "controllers", "settings", "two_factor.go"), []byte(tfb.String()), 0644)
		_ = os.WriteFile(filepath.Join(dir, "resources", "views", "settings", "two_factor.nimbus"), []byte(jetstreamViewTwoFactor), 0644)

		// Routes add 2FA when starter=jetstream (starterRoutesTmpl).

		_ = teams
	}
	if lw {
		_ = patchGoModForNimbusLivewire(dir)
	}
	return nil
}

// patchGoModForNimbusLivewire adds the standalone Livewire module next to Nimbus (same layout as `nimbus create`).
func patchGoModForNimbusLivewire(dir string) error {
	path := filepath.Join(dir, "go.mod")
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := string(b)
	if strings.Contains(s, "nimbus-livewire") {
		return nil
	}
	s = strings.Replace(s, "\tgithub.com/joho/godotenv v1.5.1\n", "\tgithub.com/joho/godotenv v1.5.1\n\tgithub.com/CodeSyncr/nimbus-livewire v0.1.0\n", 1)
	s = strings.Replace(s, "replace github.com/CodeSyncr/nimbus => ../nimbus\n", "replace github.com/CodeSyncr/nimbus => ../nimbus\nreplace github.com/CodeSyncr/nimbus-livewire => ../nimbus-livewire\n", 1)
	return os.WriteFile(path, []byte(s), 0644)
}

// ── config/supabase.go ────────────────────────────────────────────
const supabaseConfigFile = `/*
|--------------------------------------------------------------------------
| Supabase Configuration
|--------------------------------------------------------------------------
|
| Supabase project credentials and database connection settings.
| These values are loaded from environment variables. You can find
| them in your Supabase Dashboard → Settings → API.
|
*/

package config

var Supabase SupabaseConfig

type SupabaseConfig struct {
	// URL is the Supabase project URL (e.g. https://xyz.supabase.co).
	URL string
	// AnonKey is the public API key for client-side access.
	AnonKey string
	// ServiceRoleKey is the server-side key with full access (bypasses RLS).
	ServiceRoleKey string
	// JWTSecret is the JWT secret for verifying access tokens.
	JWTSecret string
	// DatabaseURL is the direct Postgres connection string.
	DatabaseURL string
}

func loadSupabase() {
	Supabase = SupabaseConfig{
		URL:            env("SUPABASE_URL", ""),
		AnonKey:        env("SUPABASE_ANON_KEY", ""),
		ServiceRoleKey: env("SUPABASE_SERVICE_ROLE_KEY", ""),
		JWTSecret:      env("SUPABASE_JWT_SECRET", ""),
		DatabaseURL:    env("SUPABASE_DB_URL", ""),
	}
}
`
