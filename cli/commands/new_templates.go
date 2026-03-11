package commands

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
const binServerTmpl = `package bin

import (
	"context"
	"fmt"
	"os"

	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/cache"
	"github.com/CodeSyncr/nimbus/database"
	"github.com/CodeSyncr/nimbus/queue"
	"github.com/CodeSyncr/nimbus/view"
	"gorm.io/gorm"

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

	start.RegisterMiddleware(app)
	start.RegisterRoutes(app)

	return app
}

func bootCache() {
	cache.Boot(nil)
}

func bootDatabase(app *nimbus.App) {
	db, err := database.Connect(config.Database.Driver, config.Database.DSN)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Database connection failed: %%v\n", err)
		os.Exit(1)
	}

	app.Container.Singleton("db", func() *gorm.DB {
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
		fmt.Fprintf(os.Stderr, "Database create failed: %%v\n", err)
		os.Exit(1)
	}
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
		fmt.Fprintf(os.Stderr, "Database create failed: %%v\n", err)
		os.Exit(1)
	}
	fmt.Println("Database created (or already exists).")
}
`

// ── start/kernel.go ────────────────────────────────────────────
const kernelStub = `package start

import (
	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/middleware"
	"github.com/CodeSyncr/nimbus/router"
)

func RegisterMiddleware(app *nimbus.App) {
	app.Router.Use(
		middleware.Logger(),
		middleware.Recover(),
	)
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
		"version": "0.1.0",
		"env":     "development",
		"tagline": "AdonisJS-style framework for Go",
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
		fmt.Fprintf(os.Stderr, "Database create failed: %%v\n", err)
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
		"version": "0.1.0",
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
    <link href="https://fonts.googleapis.com/css2?family=Bricolage+Grotesque:opsz,wght@12..96,300;12..96,400;12..96,500;12..96,700;12..96,800&family=DM+Mono:wght@300;400;500&display=swap" rel="stylesheet">
    <style>
*, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }

    :root {
      --bg:         #06070f;
      --bg-card:    rgba(255,255,255,0.03);
      --border:     rgba(255,255,255,0.07);
      --border-h:   rgba(255,255,255,0.14);
      --amber:      #f59e0b;
      --amber-dim:  #d97706;
      --cyan:       #22d3ee;
      --slate:      #94a3b8;
      --text:       #e2e8f0;
      --text-dim:   #64748b;
      --glow-a:     rgba(245,158,11,0.18);
      --glow-c:     rgba(34,211,238,0.12);
    }

    html { scroll-behavior: smooth; }

    body {
      background: var(--bg);
      color: var(--text);
      font-family: 'Bricolage Grotesque', system-ui, sans-serif;
      min-height: 100vh;
      overflow-x: hidden;
    }

    /* ─── AURORA BACKGROUND ─── */
    .aurora {
      position: fixed; inset: 0; z-index: 0; pointer-events: none;
      overflow: hidden;
    }
    .aurora::before,
    .aurora::after {
      content: '';
      position: absolute;
      border-radius: 50%;
      filter: blur(120px);
      opacity: .55;
    }
    .aurora::before {
      width: 900px; height: 600px;
      top: -200px; left: -200px;
      background: radial-gradient(ellipse, rgba(245,158,11,0.22) 0%, transparent 70%);
      animation: drift1 22s ease-in-out infinite alternate;
    }
    .aurora::after {
      width: 700px; height: 500px;
      bottom: -150px; right: -150px;
      background: radial-gradient(ellipse, rgba(34,211,238,0.18) 0%, transparent 70%);
      animation: drift2 18s ease-in-out infinite alternate;
    }
    @keyframes drift1 { from { transform: translate(0,0) scale(1); } to { transform: translate(120px, 80px) scale(1.1); } }
    @keyframes drift2 { from { transform: translate(0,0) scale(1); } to { transform: translate(-80px,-60px) scale(1.12); } }

    /* Subtle grid overlay */
    .grid-overlay {
      position: fixed; inset: 0; z-index: 0; pointer-events: none;
      background-image:
        linear-gradient(rgba(255,255,255,0.015) 1px, transparent 1px),
        linear-gradient(90deg, rgba(255,255,255,0.015) 1px, transparent 1px);
      background-size: 64px 64px;
    }

    /* ─── LAYOUT ─── */
    .wrapper {
      position: relative; z-index: 1;
      max-width: 960px;
      margin: 0 auto;
      padding: 0 24px;
    }

    /* ─── NAV ─── */
    nav {
      position: relative; z-index: 10;
      display: flex; align-items: center; justify-content: space-between;
      padding: 22px 0;
      border-bottom: 1px solid var(--border);
    }
    .nav-logo {
      display: flex; align-items: center; gap: 10px;
      font-weight: 700; font-size: 1.05rem; letter-spacing: -0.02em;
      text-decoration: none; color: var(--text);
    }
    .nav-logo svg { color: var(--amber); }
    .nav-badge {
      font-size: 0.68rem; font-weight: 500; font-family: 'DM Mono', monospace;
      background: rgba(245,158,11,0.12);
      color: var(--amber);
      border: 1px solid rgba(245,158,11,0.25);
      border-radius: 100px;
      padding: 2px 9px;
    }
    .nav-links { display: flex; align-items: center; gap: 6px; }
    .nav-link {
      display: inline-flex; align-items: center; gap-7px;
      padding: 6px 14px;
      border-radius: 8px;
      font-size: 0.82rem; font-weight: 500;
      color: var(--slate);
      text-decoration: none;
      border: 1px solid transparent;
      transition: all .2s;
    }
    .nav-link:hover { color: var(--text); background: var(--bg-card); border-color: var(--border); }
    .nav-link.primary {
      background: var(--amber);
      color: #0d0e17;
      border-color: var(--amber);
      font-weight: 600;
    }
    .nav-link.primary:hover { background: #fbbf24; border-color: #fbbf24; }

    /* ─── HERO ─── */
    .hero {
      padding: 88px 0 72px;
      text-align: center;
    }
    .status-pill {
      display: inline-flex; align-items: center; gap: 8px;
      background: rgba(34,211,238,0.08);
      border: 1px solid rgba(34,211,238,0.2);
      border-radius: 100px;
      padding: 5px 14px 5px 10px;
      font-size: 0.78rem; font-weight: 500;
      color: var(--cyan);
      margin-bottom: 36px;
      letter-spacing: 0.01em;
    }
    .pulse-dot {
      position: relative; display: flex;
      width: 8px; height: 8px;
    }
    .pulse-dot span {
      position: absolute; inset: 0;
      border-radius: 50%; background: var(--cyan);
    }
    .pulse-dot span:first-child {
      animation: pulse-ring 1.8s ease-out infinite;
      opacity: 0;
    }
    @keyframes pulse-ring {
      0%   { transform: scale(1); opacity: .7; }
      100% { transform: scale(2.4); opacity: 0; }
    }

    .hero-title {
      font-size: clamp(2.8rem, 6vw, 4.8rem);
      font-weight: 800;
      line-height: 1.05;
      letter-spacing: -0.04em;
      color: #fff;
      margin-bottom: 20px;
    }
    .hero-title .accent {
      background: linear-gradient(135deg, var(--amber) 0%, #fb923c 60%, #f43f5e 100%);
      -webkit-background-clip: text; background-clip: text;
      -webkit-text-fill-color: transparent;
    }
    .hero-sub {
      font-size: 1.15rem; font-weight: 400;
      color: var(--slate); max-width: 520px;
      margin: 0 auto 42px;
      line-height: 1.65;
    }
    .hero-ctas {
      display: flex; flex-wrap: wrap; gap: 12px; justify-content: center;
      margin-bottom: 56px;
    }
    .btn {
      display: inline-flex; align-items: center; gap: 8px;
      padding: 11px 22px; border-radius: 10px;
      font-size: 0.88rem; font-weight: 600;
      text-decoration: none; transition: all .2s; border: 1px solid transparent;
      font-family: 'Bricolage Grotesque', sans-serif;
    }
    .btn-primary {
      background: var(--amber);
      color: #0d0e17;
      box-shadow: 0 0 30px rgba(245,158,11,0.3), 0 4px 14px rgba(245,158,11,0.2);
    }
    .btn-primary:hover {
      background: #fbbf24;
      box-shadow: 0 0 44px rgba(245,158,11,0.45), 0 4px 20px rgba(245,158,11,0.3);
      transform: translateY(-1px);
    }
    .btn-ghost {
      background: var(--bg-card);
      color: var(--text);
      border-color: var(--border);
      backdrop-filter: blur(8px);
    }
    .btn-ghost:hover { border-color: var(--border-h); background: rgba(255,255,255,0.05); transform: translateY(-1px); }

    /* Command snippet */
    .cmd-block {
      display: inline-flex; align-items: center; gap: 12px;
      background: rgba(255,255,255,0.04);
      border: 1px solid var(--border);
      border-radius: 10px;
      padding: 12px 20px;
      font-family: 'DM Mono', monospace;
      font-size: 0.83rem;
      color: var(--slate);
    }
    .cmd-block .prompt { color: var(--amber); user-select: none; }
    .cmd-block .cmd-text { color: #e2e8f0; }
    .cmd-copy {
      cursor: pointer; padding: 4px 6px; border-radius: 5px;
      background: none; border: none; color: var(--text-dim);
      transition: all .15s; font-size: 1rem; line-height: 1;
    }
    .cmd-copy:hover { color: var(--text); background: rgba(255,255,255,0.06); }
    .cmd-copy.copied { color: #4ade80; }

    /* ─── SECTION DIVIDER ─── */
    .section-label {
      text-align: center;
      font-size: 0.72rem; font-weight: 600; letter-spacing: 0.14em;
      text-transform: uppercase;
      color: var(--text-dim);
      margin-bottom: 36px;
    }

    /* ─── FEATURE CARDS ─── */
    .features-grid {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
      gap: 14px;
      margin-bottom: 72px;
    }
    .card {
      background: var(--bg-card);
      border: 1px solid var(--border);
      border-radius: 14px;
      padding: 26px 24px;
      transition: all .25s;
      position: relative;
      overflow: hidden;
    }
    .card::before {
      content: '';
      position: absolute; inset: 0;
      opacity: 0;
      transition: opacity .25s;
      border-radius: 14px;
    }
    .card:hover { border-color: var(--border-h); transform: translateY(-2px); }
    .card:hover::before { opacity: 1; }
    .card.amber::before { background: radial-gradient(ellipse at top left, rgba(245,158,11,0.07) 0%, transparent 60%); }
    .card.cyan::before  { background: radial-gradient(ellipse at top left, rgba(34,211,238,0.06) 0%, transparent 60%); }
    .card.rose::before  { background: radial-gradient(ellipse at top left, rgba(244,63,94,0.06) 0%, transparent 60%); }
    .card.violet::before{ background: radial-gradient(ellipse at top left, rgba(167,139,250,0.06) 0%, transparent 60%); }
    .card.green::before { background: radial-gradient(ellipse at top left, rgba(74,222,128,0.06) 0%, transparent 60%); }
    .card.sky::before   { background: radial-gradient(ellipse at top left, rgba(56,189,248,0.06) 0%, transparent 60%); }

    .card-icon {
      width: 38px; height: 38px;
      border-radius: 9px;
      display: flex; align-items: center; justify-content: center;
      margin-bottom: 16px;
      font-size: 1.1rem;
    }
    .card-icon.amber { background: rgba(245,158,11,0.12); color: var(--amber); }
    .card-icon.cyan  { background: rgba(34,211,238,0.1);  color: var(--cyan); }
    .card-icon.rose  { background: rgba(244,63,94,0.1);   color: #f43f5e; }
    .card-icon.violet{ background: rgba(167,139,250,0.1); color: #a78bfa; }
    .card-icon.green { background: rgba(74,222,128,0.1);  color: #4ade80; }
    .card-icon.sky   { background: rgba(56,189,248,0.1);  color: #38bdf8; }

    .card h3 {
      font-size: 0.95rem; font-weight: 700;
      color: var(--text); margin-bottom: 7px;
      letter-spacing: -0.02em;
    }
    .card p {
      font-size: 0.83rem; line-height: 1.65;
      color: var(--text-dim);
    }

    /* ─── CODE BLOCK ─── */
    .code-section { margin-bottom: 72px; }
    .code-window {
      background: rgba(255,255,255,0.02);
      border: 1px solid var(--border);
      border-radius: 14px;
      overflow: hidden;
    }
    .code-titlebar {
      display: flex; align-items: center; justify-content: space-between;
      padding: 12px 18px;
      border-bottom: 1px solid var(--border);
      background: rgba(255,255,255,0.02);
    }
    .code-dots { display: flex; gap: 6px; }
    .code-dot {
      width: 11px; height: 11px; border-radius: 50%;
    }
    .code-dot:nth-child(1) { background: #ff5f57; }
    .code-dot:nth-child(2) { background: #ffbd2e; }
    .code-dot:nth-child(3) { background: #28c840; }
    .code-filename {
      font-family: 'DM Mono', monospace;
      font-size: 0.74rem;
      color: var(--text-dim);
    }
    pre {
      padding: 24px;
      overflow-x: auto;
      font-family: 'DM Mono', monospace;
      font-size: 0.82rem;
      line-height: 1.75;
      tab-size: 2;
    }
    .kw   { color: #c084fc; }
    .fn   { color: #60a5fa; }
    .str  { color: #86efac; }
    .cmt  { color: #475569; font-style: italic; }
    .num  { color: var(--amber); }
    .type { color: var(--cyan); }
    .op   { color: #94a3b8; }

    /* ─── STATS ROW ─── */
    .stats-row {
      display: grid;
      grid-template-columns: repeat(3, 1fr);
      gap: 1px;
      background: var(--border);
      border: 1px solid var(--border);
      border-radius: 14px;
      overflow: hidden;
      margin-bottom: 72px;
    }
    .stat {
      background: var(--bg);
      padding: 28px 24px;
      text-align: center;
    }
    .stat-val {
      font-size: 2rem; font-weight: 800;
      letter-spacing: -0.05em;
      background: linear-gradient(135deg, #fff 0%, #94a3b8 100%);
      -webkit-background-clip: text; background-clip: text;
      -webkit-text-fill-color: transparent;
    }
    .stat-label {
      font-size: 0.78rem; color: var(--text-dim);
      margin-top: 4px; font-weight: 400;
    }

    /* ─── FOOTER ─── */
    footer {
      border-top: 1px solid var(--border);
      padding: 32px 0;
    }
    .footer-inner {
      display: flex; align-items: center; justify-content: space-between;
      flex-wrap: wrap; gap: 14px;
    }
    .footer-text { font-size: 0.8rem; color: var(--text-dim); }
    .footer-text a { color: var(--amber); text-decoration: none; transition: color .15s; }
    .footer-text a:hover { color: #fbbf24; }
    .footer-links { display: flex; gap: 20px; }
    .footer-links a {
      font-size: 0.8rem; color: var(--text-dim);
      text-decoration: none; transition: color .15s;
    }
    .footer-links a:hover { color: var(--text); }

    /* ─── ANIMATIONS ─── */
    .fade-up {
      opacity: 0;
      transform: translateY(18px);
      animation: fadeUp .55s cubic-bezier(.22,1,.36,1) forwards;
    }
    @keyframes fadeUp {
      to { opacity: 1; transform: translateY(0); }
    }
    .fade-up:nth-child(1) { animation-delay: .05s; }
    .fade-up:nth-child(2) { animation-delay: .12s; }
    .fade-up:nth-child(3) { animation-delay: .19s; }
    .fade-up:nth-child(4) { animation-delay: .26s; }
    .fade-up:nth-child(5) { animation-delay: .33s; }
    .fade-up:nth-child(6) { animation-delay: .40s; }

    .stagger-hero > * {
      opacity: 0;
      transform: translateY(14px);
      animation: fadeUp .5s cubic-bezier(.22,1,.36,1) forwards;
    }
    .stagger-hero > *:nth-child(1) { animation-delay: .1s; }
    .stagger-hero > *:nth-child(2) { animation-delay: .2s; }
    .stagger-hero > *:nth-child(3) { animation-delay: .3s; }
    .stagger-hero > *:nth-child(4) { animation-delay: .4s; }
    .stagger-hero > *:nth-child(5) { animation-delay: .5s; }

    /* Responsive */
    @media (max-width: 600px) {
      .nav-links .btn-ghost { display: none; }
      .stats-row { grid-template-columns: 1fr; }
      .hero-title { font-size: 2.4rem; }
      .footer-inner { flex-direction: column; align-items: flex-start; }
    }
    </style>
    {{ if .viteDev }}
    <script type="module" src="http://localhost:5173/@vite/client"></script>
    <script type="module" src="http://localhost:5173/inertia/app.tsx"></script>
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

// ── Inertia kit: resources/inertia/pages/home/index.tsx ────────────
const inertiaPageHomeReact = `export default function Index({ appName, version, env, tagline }: { appName: string, version: string, env: string, tagline: string }) {
  const copyCommand = (e: any) => {
    navigator.clipboard.writeText('nimbus new my-app --kit=web');
    const btn = e.currentTarget;
    btn.classList.add('copied');
    btn.innerHTML = '✓';
    setTimeout(() => {
      btn.classList.remove('copied');
      btn.innerHTML = '⎘';
    }, 1800);
  };

  return (
    <>
<div className="aurora"></div>
  <div className="grid-overlay"></div>

  <div className="wrapper">

    {/*  NAV  */}
    <nav>
      <a href="/" className="nav-logo">
        <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
          <path d="M17.5 19H9a7 7 0 1 1 6.71-9h1.79a4.5 4.5 0 1 1 0 9Z"/>
        </svg>
        {appName}
        <span className="nav-badge">v{version}</span>
      </a>
      <div className="nav-links">
        <a href="/health" className="nav-link">Health</a>
        <a href="https://github.com/CodeSyncr/nimbus" target="_blank" rel="noopener" className="nav-link primary">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor"><path d="M12 2C6.477 2 2 6.484 2 12.017c0 4.425 2.865 8.18 6.839 9.504.5.092.682-.217.682-.483 0-.237-.008-.868-.013-1.703-2.782.605-3.369-1.343-3.369-1.343-.454-1.158-1.11-1.466-1.11-1.466-.908-.62.069-.608.069-.608 1.003.07 1.531 1.032 1.531 1.032.892 1.53 2.341 1.088 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.113-4.555-4.951 0-1.093.39-1.988 1.029-2.688-.103-.253-.446-1.272.098-2.65 0 0 .84-.27 2.75 1.026A9.564 9.564 0 0 1 12 6.844a9.59 9.59 0 0 1 2.504.337c1.909-1.296 2.747-1.027 2.747-1.027.546 1.379.202 2.398.1 2.651.64.7 1.028 1.595 1.028 2.688 0 3.848-2.339 4.695-4.566 4.943.359.309.678.92.678 1.855 0 1.338-.012 2.419-.012 2.747 0 .268.18.58.688.482A10.02 10.02 0 0 0 22 12.017C22 6.484 17.522 2 12 2Z"/></svg>
          GitHub
        </a>
      </div>
    </nav>

    {/*  HERO  */}
    <section className="hero">
      <div className="stagger-hero">
        <div>
          <div className="status-pill">
            <span className="pulse-dot"><span></span><span></span></span>
            Application running · {env} environment
          </div>
        </div>
        <h1 className="hero-title">
          Build fast.<br/>
          Deploy with <span className="accent">{appName}</span>.
        </h1>
        <p className="hero-sub">{tagline}</p>
        <div className="hero-ctas">
          <a href="https://github.com/CodeSyncr/nimbus" target="_blank" rel="noopener" className="btn btn-primary">
            <svg width="15" height="15" viewBox="0 0 24 24" fill="currentColor"><path d="M12 2C6.477 2 2 6.484 2 12.017c0 4.425 2.865 8.18 6.839 9.504.5.092.682-.217.682-.483 0-.237-.008-.868-.013-1.703-2.782.605-3.369-1.343-3.369-1.343-.454-1.158-1.11-1.466-1.11-1.466-.908-.62.069-.608.069-.608 1.003.07 1.531 1.032 1.531 1.032.892 1.53 2.341 1.088 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.113-4.555-4.951 0-1.093.39-1.988 1.029-2.688-.103-.253-.446-1.272.098-2.65 0 0 .84-.27 2.75 1.026A9.564 9.564 0 0 1 12 6.844a9.59 9.59 0 0 1 2.504.337c1.909-1.296 2.747-1.027 2.747-1.027.546 1.379.202 2.398.1 2.651.64.7 1.028 1.595 1.028 2.688 0 3.848-2.339 4.695-4.566 4.943.359.309.678.92.678 1.855 0 1.338-.012 2.419-.012 2.747 0 .268.18.58.688.482A10.02 10.02 0 0 0 22 12.017C22 6.484 17.522 2 12 2Z"/></svg>
            View on GitHub
          </a>
          <a href="https://github.com/CodeSyncr/nimbus/tree/main/docs" target="_blank" rel="noopener" className="btn btn-ghost">
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14,2 14,8 20,8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/><polyline points="10,9 9,9 8,9"/></svg>
            Documentation
          </a>
          <a href="/health" className="btn btn-ghost">
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><polyline points="22,12 18,12 15,21 9,3 6,12 2,12"/></svg>
            Health check
          </a>
        </div>
        <div>
          <div className="cmd-block">
            <span className="prompt">$</span>
            <span className="cmd-text">nimbus new my-app --kit=web</span>
            <button className="cmd-copy" title="Copy to clipboard" onClick={copyCommand}>⎘</button>
          </div>
        </div>
      </div>
    </section>

    {/*  STATS  */}
    <div className="stats-row">
      <div className="stat">
        <div className="stat-val">~0ms</div>
        <div className="stat-label">Cold start overhead</div>
      </div>
      <div className="stat">
        <div className="stat-val">Go</div>
        <div className="stat-label">Powered by Go runtime</div>
      </div>
      <div className="stat">
        <div className="stat-val">AdonisJS</div>
        <div className="stat-label">Inspired by</div>
      </div>
    </div>

    {/*  FEATURES  */}
    <p className="section-label">What's included</p>
    <div className="features-grid">
      <div className="card amber fade-up">
        <div className="card-icon amber">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><path d="M12 2L2 7l10 5 10-5-10-5z"/><path d="M2 17l10 5 10-5"/><path d="M2 12l10 5 10-5"/></svg>
        </div>
        <h3>MVC Architecture</h3>
        <p>Clean separation of concerns with Models, Views, and Controllers — batteries included, opinionated by default.</p>
      </div>
      <div className="card cyan fade-up">
        <div className="card-icon cyan">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z"/></svg>
        </div>
        <h3>Blazing Fast Router</h3>
        <p>Radix-tree based HTTP router with named params, groups, middleware, and resource routing out of the box.</p>
      </div>
      <div className="card rose fade-up">
        <div className="card-icon rose">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M9 21V9"/></svg>
        </div>
        <h3>ORM & Migrations</h3>
        <p>Expressive query builder with relationship support. Version-controlled schema migrations that just work.</p>
      </div>
      <div className="card violet fade-up">
        <div className="card-icon violet">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg>
        </div>
        <h3>Auth Scaffolding</h3>
        <p>Session, token, and OAuth-based auth with guards and middleware — generated in seconds with the CLI.</p>
      </div>
      <div className="card green fade-up">
        <div className="card-icon green">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg>
        </div>
        <h3>Nimbus Templates</h3>
        <p>Server-side HTML templating with layouts, partials, and live hot-reload. Edit and see changes instantly.</p>
      </div>
      <div className="card sky fade-up">
        <div className="card-icon sky">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.07 4.93a10 10 0 0 1 0 14.14M4.93 4.93a10 10 0 0 0 0 14.14"/></svg>
        </div>
        <h3>Event System</h3>
        <p>Typed event emitter and listener system. Decouple your business logic with first-class async event handling.</p>
      </div>
    </div>

    {/*  CODE EXAMPLE  */}
    <div className="code-section">
      <p className="section-label">Looks like this</p>
      <div className="code-window">
        <div className="code-titlebar">
          <div className="code-dots">
            <div className="code-dot"></div>
            <div className="code-dot"></div>
            <div className="code-dot"></div>
          </div>
          <span className="code-filename">app/controllers/users_controller.go</span>
          <span></span>
        </div>
        <pre><span className="kw">package</span> controllers

<span className="kw">import</span> (
  <span className="str">"github.com/CodeSyncr/nimbus/http"</span>
  <span className="str">"your-app/app/models"</span>
)

<span className="cmt">// Index returns a paginated list of users</span>
<span className="kw">func</span> (<span className="type">UsersController</span>) <span className="fn">Index</span>(ctx <span className="op">*</span><span className="type">http.Context</span>) <span className="kw">error</span> {
  users, err <span className="op">:=</span> models.<span className="type">User</span>.<span className="fn">Query</span>().
    <span className="fn">Where</span>(<span className="str">"active"</span>, <span className="num">true</span>).
    <span className="fn">OrderBy</span>(<span className="str">"created_at"</span>, <span className="str">"desc"</span>).
    <span className="fn">Paginate</span>(ctx.<span className="fn">QueryInt</span>(<span className="str">"page"</span>, <span className="num">1</span>), <span className="num">20</span>)

  <span className="kw">if</span> err <span className="op">!=</span> <span className="num">nil</span> {
    <span className="kw">return</span> ctx.<span className="fn">InternalServerError</span>(err)
  }

  <span className="kw">return</span> ctx.<span className="fn">View</span>(<span className="str">"users/index"</span>, <span className="type">ViewData</span>{
    <span className="str">"users"</span>: users,
    <span className="str">"meta"</span>:  users.<span className="fn">Meta</span>(),
  })
}</pre>
      </div>
    </div>

    {/*  FOOTER  */}
    <footer>
      <div className="footer-inner">
        <p className="footer-text">
          Built with <a href="https://github.com/CodeSyncr/nimbus" target="_blank" rel="noopener">Nimbus</a>
          · AdonisJS-style framework for Go
        </p>
        <div className="footer-links">
          <a href="https://github.com/CodeSyncr/nimbus" target="_blank" rel="noopener">GitHub</a>
          <a href="/health">Health</a>
          <a href="https://github.com/CodeSyncr/nimbus/issues" target="_blank" rel="noopener">Issues</a>
        </div>
      </div>
    </footer>

  </div>{/* /.wrapper */}
    </>
  );
}
`

// ── Inertia kit: inertia/app.js (Vue) ────────────────────────────────
const inertiaAppVue = `import { createApp } from 'vue'
import { createInertiaApp } from '@inertiajs/vue3'

createInertiaApp({
  resolve: (name) => {
    const pages = import.meta.glob('./pages/**/*.vue')
    return pages['./pages/' + name.replace(/\./g, '/') + '.vue']()
  },
  setup({ el, App, props, plugin }) {
    createApp(App).use(plugin).mount(el)
  },
})
`

// ── Inertia kit: resources/js/Pages/Home/Index.vue ───────────────
const inertiaPageHomeVue = `<template>
<div class="aurora"></div>
  <div class="grid-overlay"></div>

  <div class="wrapper">

    <!-- NAV -->
    <nav>
      <a href="/" class="nav-logo">
        <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M17.5 19H9a7 7 0 1 1 6.71-9h1.79a4.5 4.5 0 1 1 0 9Z"/>
        </svg>
        {{ appName }}
        <span class="nav-badge">v{{ version }}</span>
      </a>
      <div class="nav-links">
        <a href="/health" class="nav-link">Health</a>
        <a href="https://github.com/CodeSyncr/nimbus" target="_blank" rel="noopener" class="nav-link primary">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor"><path d="M12 2C6.477 2 2 6.484 2 12.017c0 4.425 2.865 8.18 6.839 9.504.5.092.682-.217.682-.483 0-.237-.008-.868-.013-1.703-2.782.605-3.369-1.343-3.369-1.343-.454-1.158-1.11-1.466-1.11-1.466-.908-.62.069-.608.069-.608 1.003.07 1.531 1.032 1.531 1.032.892 1.53 2.341 1.088 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.113-4.555-4.951 0-1.093.39-1.988 1.029-2.688-.103-.253-.446-1.272.098-2.65 0 0 .84-.27 2.75 1.026A9.564 9.564 0 0 1 12 6.844a9.59 9.59 0 0 1 2.504.337c1.909-1.296 2.747-1.027 2.747-1.027.546 1.379.202 2.398.1 2.651.64.7 1.028 1.595 1.028 2.688 0 3.848-2.339 4.695-4.566 4.943.359.309.678.92.678 1.855 0 1.338-.012 2.419-.012 2.747 0 .268.18.58.688.482A10.02 10.02 0 0 0 22 12.017C22 6.484 17.522 2 12 2Z"/></svg>
          GitHub
        </a>
      </div>
    </nav>

    <!-- HERO -->
    <section class="hero">
      <div class="stagger-hero">
        <div>
          <div class="status-pill">
            <span class="pulse-dot"><span></span><span></span></span>
            Application running · {{ env }} environment
          </div>
        </div>
        <h1 class="hero-title">
          Build fast.<br>
          Deploy with <span class="accent">{{ appName }}</span>.
        </h1>
        <p class="hero-sub">{{ tagline }}</p>
        <div class="hero-ctas">
          <a href="https://github.com/CodeSyncr/nimbus" target="_blank" rel="noopener" class="btn btn-primary">
            <svg width="15" height="15" viewBox="0 0 24 24" fill="currentColor"><path d="M12 2C6.477 2 2 6.484 2 12.017c0 4.425 2.865 8.18 6.839 9.504.5.092.682-.217.682-.483 0-.237-.008-.868-.013-1.703-2.782.605-3.369-1.343-3.369-1.343-.454-1.158-1.11-1.466-1.11-1.466-.908-.62.069-.608.069-.608 1.003.07 1.531 1.032 1.531 1.032.892 1.53 2.341 1.088 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.113-4.555-4.951 0-1.093.39-1.988 1.029-2.688-.103-.253-.446-1.272.098-2.65 0 0 .84-.27 2.75 1.026A9.564 9.564 0 0 1 12 6.844a9.59 9.59 0 0 1 2.504.337c1.909-1.296 2.747-1.027 2.747-1.027.546 1.379.202 2.398.1 2.651.64.7 1.028 1.595 1.028 2.688 0 3.848-2.339 4.695-4.566 4.943.359.309.678.92.678 1.855 0 1.338-.012 2.419-.012 2.747 0 .268.18.58.688.482A10.02 10.02 0 0 0 22 12.017C22 6.484 17.522 2 12 2Z"/></svg>
            View on GitHub
          </a>
          <a href="https://github.com/CodeSyncr/nimbus/tree/main/docs" target="_blank" rel="noopener" class="btn btn-ghost">
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14,2 14,8 20,8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/><polyline points="10,9 9,9 8,9"/></svg>
            Documentation
          </a>
          <a href="/health" class="btn btn-ghost">
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="22,12 18,12 15,21 9,3 6,12 2,12"/></svg>
            Health check
          </a>
        </div>
        <div>
          <div class="cmd-block">
            <span class="prompt">$</span>
            <span class="cmd-text">nimbus new my-app --kit=web</span>
            <button class="cmd-copy" title="Copy to clipboard" @click="copyCommand">⎘</button>
          </div>
        </div>
      </div>
    </section>

    <!-- STATS -->
    <div class="stats-row">
      <div class="stat">
        <div class="stat-val">~0ms</div>
        <div class="stat-label">Cold start overhead</div>
      </div>
      <div class="stat">
        <div class="stat-val">Go</div>
        <div class="stat-label">Powered by Go runtime</div>
      </div>
      <div class="stat">
        <div class="stat-val">AdonisJS</div>
        <div class="stat-label">Inspired by</div>
      </div>
    </div>

    <!-- FEATURES -->
    <p class="section-label">What's included</p>
    <div class="features-grid">
      <div class="card amber fade-up">
        <div class="card-icon amber">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2L2 7l10 5 10-5-10-5z"/><path d="M2 17l10 5 10-5"/><path d="M2 12l10 5 10-5"/></svg>
        </div>
        <h3>MVC Architecture</h3>
        <p>Clean separation of concerns with Models, Views, and Controllers — batteries included, opinionated by default.</p>
      </div>
      <div class="card cyan fade-up">
        <div class="card-icon cyan">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z"/></svg>
        </div>
        <h3>Blazing Fast Router</h3>
        <p>Radix-tree based HTTP router with named params, groups, middleware, and resource routing out of the box.</p>
      </div>
      <div class="card rose fade-up">
        <div class="card-icon rose">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M9 21V9"/></svg>
        </div>
        <h3>ORM & Migrations</h3>
        <p>Expressive query builder with relationship support. Version-controlled schema migrations that just work.</p>
      </div>
      <div class="card violet fade-up">
        <div class="card-icon violet">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg>
        </div>
        <h3>Auth Scaffolding</h3>
        <p>Session, token, and OAuth-based auth with guards and middleware — generated in seconds with the CLI.</p>
      </div>
      <div class="card green fade-up">
        <div class="card-icon green">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg>
        </div>
        <h3>Nimbus Templates</h3>
        <p>Server-side HTML templating with layouts, partials, and live hot-reload. Edit and see changes instantly.</p>
      </div>
      <div class="card sky fade-up">
        <div class="card-icon sky">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.07 4.93a10 10 0 0 1 0 14.14M4.93 4.93a10 10 0 0 0 0 14.14"/></svg>
        </div>
        <h3>Event System</h3>
        <p>Typed event emitter and listener system. Decouple your business logic with first-class async event handling.</p>
      </div>
    </div>

    <!-- CODE EXAMPLE -->
    <div class="code-section">
      <p class="section-label">Looks like this</p>
      <div class="code-window">
        <div class="code-titlebar">
          <div class="code-dots">
            <div class="code-dot"></div>
            <div class="code-dot"></div>
            <div class="code-dot"></div>
          </div>
          <span class="code-filename">app/controllers/users_controller.go</span>
          <span></span>
        </div>
        <pre><span class="kw">package</span> controllers

<span class="kw">import</span> (
  <span class="str">"github.com/CodeSyncr/nimbus/http"</span>
  <span class="str">"your-app/app/models"</span>
)

<span class="cmt">// Index returns a paginated list of users</span>
<span class="kw">func</span> (<span class="type">UsersController</span>) <span class="fn">Index</span>(ctx <span class="op">*</span><span class="type">http.Context</span>) <span class="kw">error</span> {
  users, err <span class="op">:=</span> models.<span class="type">User</span>.<span class="fn">Query</span>().
    <span class="fn">Where</span>(<span class="str">"active"</span>, <span class="num">true</span>).
    <span class="fn">OrderBy</span>(<span class="str">"created_at"</span>, <span class="str">"desc"</span>).
    <span class="fn">Paginate</span>(ctx.<span class="fn">QueryInt</span>(<span class="str">"page"</span>, <span class="num">1</span>), <span class="num">20</span>)

  <span class="kw">if</span> err <span class="op">!=</span> <span class="num">nil</span> {
    <span class="kw">return</span> ctx.<span class="fn">InternalServerError</span>(err)
  }

  <span class="kw">return</span> ctx.<span class="fn">View</span>(<span class="str">"users/index"</span>, <span class="type">ViewData</span>{
    <span class="str">"users"</span>: users,
    <span class="str">"meta"</span>:  users.<span class="fn">Meta</span>(),
  })
}</pre>
      </div>
    </div>

    <!-- FOOTER -->
    <footer>
      <div class="footer-inner">
        <p class="footer-text">
          Built with <a href="https://github.com/CodeSyncr/nimbus" target="_blank" rel="noopener">Nimbus</a>
          · AdonisJS-style framework for Go
        </p>
        <div class="footer-links">
          <a href="https://github.com/CodeSyncr/nimbus" target="_blank" rel="noopener">GitHub</a>
          <a href="/health">Health</a>
          <a href="https://github.com/CodeSyncr/nimbus/issues" target="_blank" rel="noopener">Issues</a>
        </div>
      </div>
    </footer>

  </div><!-- /.wrapper -->
</template>

<script setup>
import { ref } from 'vue'

defineProps(['appName', 'version', 'env', 'tagline'])

function copyCommand(e) {
  navigator.clipboard.writeText('nimbus new my-app --kit=web')
  const btn = e.currentTarget
  btn.classList.add('copied')
  btn.innerHTML = '✓'
  setTimeout(() => {
    btn.classList.remove('copied')
    btn.innerHTML = '⎘'
  }, 1800)
}
</script>
`

// ── Inertia kit: inertia/app.js (Svelte) ─────────────────────────────
const inertiaAppSvelte = `import { createInertiaApp } from '@inertiajs/svelte'

createInertiaApp({
  resolve: (name) => {
    const pages = import.meta.glob('./pages/**/*.svelte')
    return pages['./pages/' + name.replace(/\./g, '/') + '.svelte']()
  },
})
`

// ── Inertia kit: resources/js/Pages/Home/Index.svelte ─────────────
const inertiaPageHomeSvelte = `<script>
  export let appName = ''
  export let version = ''
  export let env = ''
  export let tagline = ''

  function copyCommand(e) {
    navigator.clipboard.writeText('nimbus new my-app --kit=web');
    const btn = e.currentTarget;
    btn.classList.add('copied');
    btn.innerHTML = '✓';
    setTimeout(() => {
      btn.classList.remove('copied');
      btn.innerHTML = '⎘';
    }, 1800);
  }
</script>

<div class="aurora"></div>
  <div class="grid-overlay"></div>

  <div class="wrapper">

    <!-- NAV -->
    <nav>
      <a href="/" class="nav-logo">
        <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M17.5 19H9a7 7 0 1 1 6.71-9h1.79a4.5 4.5 0 1 1 0 9Z"/>
        </svg>
        {appName}
        <span class="nav-badge">v{version}</span>
      </a>
      <div class="nav-links">
        <a href="/health" class="nav-link">Health</a>
        <a href="https://github.com/CodeSyncr/nimbus" target="_blank" rel="noopener" class="nav-link primary">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor"><path d="M12 2C6.477 2 2 6.484 2 12.017c0 4.425 2.865 8.18 6.839 9.504.5.092.682-.217.682-.483 0-.237-.008-.868-.013-1.703-2.782.605-3.369-1.343-3.369-1.343-.454-1.158-1.11-1.466-1.11-1.466-.908-.62.069-.608.069-.608 1.003.07 1.531 1.032 1.531 1.032.892 1.53 2.341 1.088 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.113-4.555-4.951 0-1.093.39-1.988 1.029-2.688-.103-.253-.446-1.272.098-2.65 0 0 .84-.27 2.75 1.026A9.564 9.564 0 0 1 12 6.844a9.59 9.59 0 0 1 2.504.337c1.909-1.296 2.747-1.027 2.747-1.027.546 1.379.202 2.398.1 2.651.64.7 1.028 1.595 1.028 2.688 0 3.848-2.339 4.695-4.566 4.943.359.309.678.92.678 1.855 0 1.338-.012 2.419-.012 2.747 0 .268.18.58.688.482A10.02 10.02 0 0 0 22 12.017C22 6.484 17.522 2 12 2Z"/></svg>
          GitHub
        </a>
      </div>
    </nav>

    <!-- HERO -->
    <section class="hero">
      <div class="stagger-hero">
        <div>
          <div class="status-pill">
            <span class="pulse-dot"><span></span><span></span></span>
            Application running · {env} environment
          </div>
        </div>
        <h1 class="hero-title">
          Build fast.<br>
          Deploy with <span class="accent">{appName}</span>.
        </h1>
        <p class="hero-sub">{tagline}</p>
        <div class="hero-ctas">
          <a href="https://github.com/CodeSyncr/nimbus" target="_blank" rel="noopener" class="btn btn-primary">
            <svg width="15" height="15" viewBox="0 0 24 24" fill="currentColor"><path d="M12 2C6.477 2 2 6.484 2 12.017c0 4.425 2.865 8.18 6.839 9.504.5.092.682-.217.682-.483 0-.237-.008-.868-.013-1.703-2.782.605-3.369-1.343-3.369-1.343-.454-1.158-1.11-1.466-1.11-1.466-.908-.62.069-.608.069-.608 1.003.07 1.531 1.032 1.531 1.032.892 1.53 2.341 1.088 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.113-4.555-4.951 0-1.093.39-1.988 1.029-2.688-.103-.253-.446-1.272.098-2.65 0 0 .84-.27 2.75 1.026A9.564 9.564 0 0 1 12 6.844a9.59 9.59 0 0 1 2.504.337c1.909-1.296 2.747-1.027 2.747-1.027.546 1.379.202 2.398.1 2.651.64.7 1.028 1.595 1.028 2.688 0 3.848-2.339 4.695-4.566 4.943.359.309.678.92.678 1.855 0 1.338-.012 2.419-.012 2.747 0 .268.18.58.688.482A10.02 10.02 0 0 0 22 12.017C22 6.484 17.522 2 12 2Z"/></svg>
            View on GitHub
          </a>
          <a href="https://github.com/CodeSyncr/nimbus/tree/main/docs" target="_blank" rel="noopener" class="btn btn-ghost">
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14,2 14,8 20,8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/><polyline points="10,9 9,9 8,9"/></svg>
            Documentation
          </a>
          <a href="/health" class="btn btn-ghost">
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="22,12 18,12 15,21 9,3 6,12 2,12"/></svg>
            Health check
          </a>
        </div>
        <div>
          <div class="cmd-block">
            <span class="prompt">$</span>
            <span class="cmd-text">nimbus new my-app --kit=web</span>
            <button class="cmd-copy" title="Copy to clipboard" on:click={copyCommand}>⎘</button>
          </div>
        </div>
      </div>
    </section>

    <!-- STATS -->
    <div class="stats-row">
      <div class="stat">
        <div class="stat-val">~0ms</div>
        <div class="stat-label">Cold start overhead</div>
      </div>
      <div class="stat">
        <div class="stat-val">Go</div>
        <div class="stat-label">Powered by Go runtime</div>
      </div>
      <div class="stat">
        <div class="stat-val">AdonisJS</div>
        <div class="stat-label">Inspired by</div>
      </div>
    </div>

    <!-- FEATURES -->
    <p class="section-label">What's included</p>
    <div class="features-grid">
      <div class="card amber fade-up">
        <div class="card-icon amber">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2L2 7l10 5 10-5-10-5z"/><path d="M2 17l10 5 10-5"/><path d="M2 12l10 5 10-5"/></svg>
        </div>
        <h3>MVC Architecture</h3>
        <p>Clean separation of concerns with Models, Views, and Controllers — batteries included, opinionated by default.</p>
      </div>
      <div class="card cyan fade-up">
        <div class="card-icon cyan">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z"/></svg>
        </div>
        <h3>Blazing Fast Router</h3>
        <p>Radix-tree based HTTP router with named params, groups, middleware, and resource routing out of the box.</p>
      </div>
      <div class="card rose fade-up">
        <div class="card-icon rose">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M9 21V9"/></svg>
        </div>
        <h3>ORM & Migrations</h3>
        <p>Expressive query builder with relationship support. Version-controlled schema migrations that just work.</p>
      </div>
      <div class="card violet fade-up">
        <div class="card-icon violet">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg>
        </div>
        <h3>Auth Scaffolding</h3>
        <p>Session, token, and OAuth-based auth with guards and middleware — generated in seconds with the CLI.</p>
      </div>
      <div class="card green fade-up">
        <div class="card-icon green">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg>
        </div>
        <h3>Nimbus Templates</h3>
        <p>Server-side HTML templating with layouts, partials, and live hot-reload. Edit and see changes instantly.</p>
      </div>
      <div class="card sky fade-up">
        <div class="card-icon sky">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.07 4.93a10 10 0 0 1 0 14.14M4.93 4.93a10 10 0 0 0 0 14.14"/></svg>
        </div>
        <h3>Event System</h3>
        <p>Typed event emitter and listener system. Decouple your business logic with first-class async event handling.</p>
      </div>
    </div>

    <!-- CODE EXAMPLE -->
    <div class="code-section">
      <p class="section-label">Looks like this</p>
      <div class="code-window">
        <div class="code-titlebar">
          <div class="code-dots">
            <div class="code-dot"></div>
            <div class="code-dot"></div>
            <div class="code-dot"></div>
          </div>
          <span class="code-filename">app/controllers/users_controller.go</span>
          <span></span>
        </div>
        <pre><span class="kw">package</span> controllers

<span class="kw">import</span> (
  <span class="str">"github.com/CodeSyncr/nimbus/http"</span>
  <span class="str">"your-app/app/models"</span>
)

<span class="cmt">// Index returns a paginated list of users</span>
<span class="kw">func</span> (<span class="type">UsersController</span>) <span class="fn">Index</span>(ctx <span class="op">*</span><span class="type">http.Context</span>) <span class="kw">error</span> {
  users, err <span class="op">:=</span> models.<span class="type">User</span>.<span class="fn">Query</span>().
    <span class="fn">Where</span>(<span class="str">"active"</span>, <span class="num">true</span>).
    <span class="fn">OrderBy</span>(<span class="str">"created_at"</span>, <span class="str">"desc"</span>).
    <span class="fn">Paginate</span>(ctx.<span class="fn">QueryInt</span>(<span class="str">"page"</span>, <span class="num">1</span>), <span class="num">20</span>)

  <span class="kw">if</span> err <span class="op">!=</span> <span class="num">nil</span> {
    <span class="kw">return</span> ctx.<span class="fn">InternalServerError</span>(err)
  }

  <span class="kw">return</span> ctx.<span class="fn">View</span>(<span class="str">"users/index"</span>, <span class="type">ViewData</span>{
    <span class="str">"users"</span>: users,
    <span class="str">"meta"</span>:  users.<span class="fn">Meta</span>(),
  })
}</pre>
      </div>
    </div>

    <!-- FOOTER -->
    <footer>
      <div class="footer-inner">
        <p class="footer-text">
          Built with <a href="https://github.com/CodeSyncr/nimbus" target="_blank" rel="noopener">Nimbus</a>
          · AdonisJS-style framework for Go
        </p>
        <div class="footer-links">
          <a href="https://github.com/CodeSyncr/nimbus" target="_blank" rel="noopener">GitHub</a>
          <a href="/health">Health</a>
          <a href="https://github.com/CodeSyncr/nimbus/issues" target="_blank" rel="noopener">Issues</a>
        </div>
      </div>
    </footer>

  </div><!-- /.wrapper -->
`

// ── Inertia kit: package.json ──────────────────────────────────
func inertiaPackageJSON(kit string) string {
	deps := `"@inertiajs/react": "^1.0.0", "react": "^18.2.0", "react-dom": "^18.2.0"`
	return `{
  "name": "nimbus-inertia-app",
  "type": "module",
  "scripts": { "dev": "vite", "build": "vite build" },
  "dependencies": { ` + deps + ` }
}
`
}

func inertiaViteConfig(kit string) string {
	return `import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'public/build',
    manifest: true,
    rollupOptions: { input: { app: 'inertia/app.tsx' } }
  }
})
`
}

const inertiaTsconfig = `{ "compilerOptions": { "strict": true } }`
const inertiaTsconfigNode = `{ "compilerOptions": { "strict": true } }`
const inertiaTsconfigInertia = `{ "compilerOptions": { "strict": true } }`
const inertiaTypesTS = `export {}`

func inertiaIndexHTML(kit string) string {
	return `<html><body><div id="app"></div><script src="/inertia/app.tsx" type="module"></script></body></html>`
}

// ── config/config.go ───────────────────────────────────────────
const configLoader = `package config

import nimbusconfig "github.com/CodeSyncr/nimbus/config"

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
  <link href="https://fonts.googleapis.com/css2?family=Bricolage+Grotesque:opsz,wght@12..96,300;12..96,400;12..96,500;12..96,700;12..96,800&family=DM+Mono:wght@300;400;500&display=swap" rel="stylesheet">
  <style>
*, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }

    :root {
      --bg:         #06070f;
      --bg-card:    rgba(255,255,255,0.03);
      --border:     rgba(255,255,255,0.07);
      --border-h:   rgba(255,255,255,0.14);
      --amber:      #f59e0b;
      --amber-dim:  #d97706;
      --cyan:       #22d3ee;
      --slate:      #94a3b8;
      --text:       #e2e8f0;
      --text-dim:   #64748b;
      --glow-a:     rgba(245,158,11,0.18);
      --glow-c:     rgba(34,211,238,0.12);
    }

    html { scroll-behavior: smooth; }

    body {
      background: var(--bg);
      color: var(--text);
      font-family: 'Bricolage Grotesque', system-ui, sans-serif;
      min-height: 100vh;
      overflow-x: hidden;
    }

    /* ─── AURORA BACKGROUND ─── */
    .aurora {
      position: fixed; inset: 0; z-index: 0; pointer-events: none;
      overflow: hidden;
    }
    .aurora::before,
    .aurora::after {
      content: '';
      position: absolute;
      border-radius: 50%;
      filter: blur(120px);
      opacity: .55;
    }
    .aurora::before {
      width: 900px; height: 600px;
      top: -200px; left: -200px;
      background: radial-gradient(ellipse, rgba(245,158,11,0.22) 0%, transparent 70%);
      animation: drift1 22s ease-in-out infinite alternate;
    }
    .aurora::after {
      width: 700px; height: 500px;
      bottom: -150px; right: -150px;
      background: radial-gradient(ellipse, rgba(34,211,238,0.18) 0%, transparent 70%);
      animation: drift2 18s ease-in-out infinite alternate;
    }
    @keyframes drift1 { from { transform: translate(0,0) scale(1); } to { transform: translate(120px, 80px) scale(1.1); } }
    @keyframes drift2 { from { transform: translate(0,0) scale(1); } to { transform: translate(-80px,-60px) scale(1.12); } }

    /* Subtle grid overlay */
    .grid-overlay {
      position: fixed; inset: 0; z-index: 0; pointer-events: none;
      background-image:
        linear-gradient(rgba(255,255,255,0.015) 1px, transparent 1px),
        linear-gradient(90deg, rgba(255,255,255,0.015) 1px, transparent 1px);
      background-size: 64px 64px;
    }

    /* ─── LAYOUT ─── */
    .wrapper {
      position: relative; z-index: 1;
      max-width: 960px;
      margin: 0 auto;
      padding: 0 24px;
    }

    /* ─── NAV ─── */
    nav {
      position: relative; z-index: 10;
      display: flex; align-items: center; justify-content: space-between;
      padding: 22px 0;
      border-bottom: 1px solid var(--border);
    }
    .nav-logo {
      display: flex; align-items: center; gap: 10px;
      font-weight: 700; font-size: 1.05rem; letter-spacing: -0.02em;
      text-decoration: none; color: var(--text);
    }
    .nav-logo svg { color: var(--amber); }
    .nav-badge {
      font-size: 0.68rem; font-weight: 500; font-family: 'DM Mono', monospace;
      background: rgba(245,158,11,0.12);
      color: var(--amber);
      border: 1px solid rgba(245,158,11,0.25);
      border-radius: 100px;
      padding: 2px 9px;
    }
    .nav-links { display: flex; align-items: center; gap: 6px; }
    .nav-link {
      display: inline-flex; align-items: center; gap-7px;
      padding: 6px 14px;
      border-radius: 8px;
      font-size: 0.82rem; font-weight: 500;
      color: var(--slate);
      text-decoration: none;
      border: 1px solid transparent;
      transition: all .2s;
    }
    .nav-link:hover { color: var(--text); background: var(--bg-card); border-color: var(--border); }
    .nav-link.primary {
      background: var(--amber);
      color: #0d0e17;
      border-color: var(--amber);
      font-weight: 600;
    }
    .nav-link.primary:hover { background: #fbbf24; border-color: #fbbf24; }

    /* ─── HERO ─── */
    .hero {
      padding: 88px 0 72px;
      text-align: center;
    }
    .status-pill {
      display: inline-flex; align-items: center; gap: 8px;
      background: rgba(34,211,238,0.08);
      border: 1px solid rgba(34,211,238,0.2);
      border-radius: 100px;
      padding: 5px 14px 5px 10px;
      font-size: 0.78rem; font-weight: 500;
      color: var(--cyan);
      margin-bottom: 36px;
      letter-spacing: 0.01em;
    }
    .pulse-dot {
      position: relative; display: flex;
      width: 8px; height: 8px;
    }
    .pulse-dot span {
      position: absolute; inset: 0;
      border-radius: 50%; background: var(--cyan);
    }
    .pulse-dot span:first-child {
      animation: pulse-ring 1.8s ease-out infinite;
      opacity: 0;
    }
    @keyframes pulse-ring {
      0%   { transform: scale(1); opacity: .7; }
      100% { transform: scale(2.4); opacity: 0; }
    }

    .hero-title {
      font-size: clamp(2.8rem, 6vw, 4.8rem);
      font-weight: 800;
      line-height: 1.05;
      letter-spacing: -0.04em;
      color: #fff;
      margin-bottom: 20px;
    }
    .hero-title .accent {
      background: linear-gradient(135deg, var(--amber) 0%, #fb923c 60%, #f43f5e 100%);
      -webkit-background-clip: text; background-clip: text;
      -webkit-text-fill-color: transparent;
    }
    .hero-sub {
      font-size: 1.15rem; font-weight: 400;
      color: var(--slate); max-width: 520px;
      margin: 0 auto 42px;
      line-height: 1.65;
    }
    .hero-ctas {
      display: flex; flex-wrap: wrap; gap: 12px; justify-content: center;
      margin-bottom: 56px;
    }
    .btn {
      display: inline-flex; align-items: center; gap: 8px;
      padding: 11px 22px; border-radius: 10px;
      font-size: 0.88rem; font-weight: 600;
      text-decoration: none; transition: all .2s; border: 1px solid transparent;
      font-family: 'Bricolage Grotesque', sans-serif;
    }
    .btn-primary {
      background: var(--amber);
      color: #0d0e17;
      box-shadow: 0 0 30px rgba(245,158,11,0.3), 0 4px 14px rgba(245,158,11,0.2);
    }
    .btn-primary:hover {
      background: #fbbf24;
      box-shadow: 0 0 44px rgba(245,158,11,0.45), 0 4px 20px rgba(245,158,11,0.3);
      transform: translateY(-1px);
    }
    .btn-ghost {
      background: var(--bg-card);
      color: var(--text);
      border-color: var(--border);
      backdrop-filter: blur(8px);
    }
    .btn-ghost:hover { border-color: var(--border-h); background: rgba(255,255,255,0.05); transform: translateY(-1px); }

    /* Command snippet */
    .cmd-block {
      display: inline-flex; align-items: center; gap: 12px;
      background: rgba(255,255,255,0.04);
      border: 1px solid var(--border);
      border-radius: 10px;
      padding: 12px 20px;
      font-family: 'DM Mono', monospace;
      font-size: 0.83rem;
      color: var(--slate);
    }
    .cmd-block .prompt { color: var(--amber); user-select: none; }
    .cmd-block .cmd-text { color: #e2e8f0; }
    .cmd-copy {
      cursor: pointer; padding: 4px 6px; border-radius: 5px;
      background: none; border: none; color: var(--text-dim);
      transition: all .15s; font-size: 1rem; line-height: 1;
    }
    .cmd-copy:hover { color: var(--text); background: rgba(255,255,255,0.06); }
    .cmd-copy.copied { color: #4ade80; }

    /* ─── SECTION DIVIDER ─── */
    .section-label {
      text-align: center;
      font-size: 0.72rem; font-weight: 600; letter-spacing: 0.14em;
      text-transform: uppercase;
      color: var(--text-dim);
      margin-bottom: 36px;
    }

    /* ─── FEATURE CARDS ─── */
    .features-grid {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
      gap: 14px;
      margin-bottom: 72px;
    }
    .card {
      background: var(--bg-card);
      border: 1px solid var(--border);
      border-radius: 14px;
      padding: 26px 24px;
      transition: all .25s;
      position: relative;
      overflow: hidden;
    }
    .card::before {
      content: '';
      position: absolute; inset: 0;
      opacity: 0;
      transition: opacity .25s;
      border-radius: 14px;
    }
    .card:hover { border-color: var(--border-h); transform: translateY(-2px); }
    .card:hover::before { opacity: 1; }
    .card.amber::before { background: radial-gradient(ellipse at top left, rgba(245,158,11,0.07) 0%, transparent 60%); }
    .card.cyan::before  { background: radial-gradient(ellipse at top left, rgba(34,211,238,0.06) 0%, transparent 60%); }
    .card.rose::before  { background: radial-gradient(ellipse at top left, rgba(244,63,94,0.06) 0%, transparent 60%); }
    .card.violet::before{ background: radial-gradient(ellipse at top left, rgba(167,139,250,0.06) 0%, transparent 60%); }
    .card.green::before { background: radial-gradient(ellipse at top left, rgba(74,222,128,0.06) 0%, transparent 60%); }
    .card.sky::before   { background: radial-gradient(ellipse at top left, rgba(56,189,248,0.06) 0%, transparent 60%); }

    .card-icon {
      width: 38px; height: 38px;
      border-radius: 9px;
      display: flex; align-items: center; justify-content: center;
      margin-bottom: 16px;
      font-size: 1.1rem;
    }
    .card-icon.amber { background: rgba(245,158,11,0.12); color: var(--amber); }
    .card-icon.cyan  { background: rgba(34,211,238,0.1);  color: var(--cyan); }
    .card-icon.rose  { background: rgba(244,63,94,0.1);   color: #f43f5e; }
    .card-icon.violet{ background: rgba(167,139,250,0.1); color: #a78bfa; }
    .card-icon.green { background: rgba(74,222,128,0.1);  color: #4ade80; }
    .card-icon.sky   { background: rgba(56,189,248,0.1);  color: #38bdf8; }

    .card h3 {
      font-size: 0.95rem; font-weight: 700;
      color: var(--text); margin-bottom: 7px;
      letter-spacing: -0.02em;
    }
    .card p {
      font-size: 0.83rem; line-height: 1.65;
      color: var(--text-dim);
    }

    /* ─── CODE BLOCK ─── */
    .code-section { margin-bottom: 72px; }
    .code-window {
      background: rgba(255,255,255,0.02);
      border: 1px solid var(--border);
      border-radius: 14px;
      overflow: hidden;
    }
    .code-titlebar {
      display: flex; align-items: center; justify-content: space-between;
      padding: 12px 18px;
      border-bottom: 1px solid var(--border);
      background: rgba(255,255,255,0.02);
    }
    .code-dots { display: flex; gap: 6px; }
    .code-dot {
      width: 11px; height: 11px; border-radius: 50%;
    }
    .code-dot:nth-child(1) { background: #ff5f57; }
    .code-dot:nth-child(2) { background: #ffbd2e; }
    .code-dot:nth-child(3) { background: #28c840; }
    .code-filename {
      font-family: 'DM Mono', monospace;
      font-size: 0.74rem;
      color: var(--text-dim);
    }
    pre {
      padding: 24px;
      overflow-x: auto;
      font-family: 'DM Mono', monospace;
      font-size: 0.82rem;
      line-height: 1.75;
      tab-size: 2;
    }
    .kw   { color: #c084fc; }
    .fn   { color: #60a5fa; }
    .str  { color: #86efac; }
    .cmt  { color: #475569; font-style: italic; }
    .num  { color: var(--amber); }
    .type { color: var(--cyan); }
    .op   { color: #94a3b8; }

    /* ─── STATS ROW ─── */
    .stats-row {
      display: grid;
      grid-template-columns: repeat(3, 1fr);
      gap: 1px;
      background: var(--border);
      border: 1px solid var(--border);
      border-radius: 14px;
      overflow: hidden;
      margin-bottom: 72px;
    }
    .stat {
      background: var(--bg);
      padding: 28px 24px;
      text-align: center;
    }
    .stat-val {
      font-size: 2rem; font-weight: 800;
      letter-spacing: -0.05em;
      background: linear-gradient(135deg, #fff 0%, #94a3b8 100%);
      -webkit-background-clip: text; background-clip: text;
      -webkit-text-fill-color: transparent;
    }
    .stat-label {
      font-size: 0.78rem; color: var(--text-dim);
      margin-top: 4px; font-weight: 400;
    }

    /* ─── FOOTER ─── */
    footer {
      border-top: 1px solid var(--border);
      padding: 32px 0;
    }
    .footer-inner {
      display: flex; align-items: center; justify-content: space-between;
      flex-wrap: wrap; gap: 14px;
    }
    .footer-text { font-size: 0.8rem; color: var(--text-dim); }
    .footer-text a { color: var(--amber); text-decoration: none; transition: color .15s; }
    .footer-text a:hover { color: #fbbf24; }
    .footer-links { display: flex; gap: 20px; }
    .footer-links a {
      font-size: 0.8rem; color: var(--text-dim);
      text-decoration: none; transition: color .15s;
    }
    .footer-links a:hover { color: var(--text); }

    /* ─── ANIMATIONS ─── */
    .fade-up {
      opacity: 0;
      transform: translateY(18px);
      animation: fadeUp .55s cubic-bezier(.22,1,.36,1) forwards;
    }
    @keyframes fadeUp {
      to { opacity: 1; transform: translateY(0); }
    }
    .fade-up:nth-child(1) { animation-delay: .05s; }
    .fade-up:nth-child(2) { animation-delay: .12s; }
    .fade-up:nth-child(3) { animation-delay: .19s; }
    .fade-up:nth-child(4) { animation-delay: .26s; }
    .fade-up:nth-child(5) { animation-delay: .33s; }
    .fade-up:nth-child(6) { animation-delay: .40s; }

    .stagger-hero > * {
      opacity: 0;
      transform: translateY(14px);
      animation: fadeUp .5s cubic-bezier(.22,1,.36,1) forwards;
    }
    .stagger-hero > *:nth-child(1) { animation-delay: .1s; }
    .stagger-hero > *:nth-child(2) { animation-delay: .2s; }
    .stagger-hero > *:nth-child(3) { animation-delay: .3s; }
    .stagger-hero > *:nth-child(4) { animation-delay: .4s; }
    .stagger-hero > *:nth-child(5) { animation-delay: .5s; }

    /* Responsive */
    @media (max-width: 600px) {
      .nav-links .btn-ghost { display: none; }
      .stats-row { grid-template-columns: 1fr; }
      .hero-title { font-size: 2.4rem; }
      .footer-inner { flex-direction: column; align-items: flex-start; }
    }
  </style>
</head>
<body>

  <div class="aurora"></div>
  <div class="grid-overlay"></div>

  <div class="wrapper">

    <!-- NAV -->
    <nav>
      <a href="/" class="nav-logo">
        <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M17.5 19H9a7 7 0 1 1 6.71-9h1.79a4.5 4.5 0 1 1 0 9Z"/>
        </svg>
        {{ .appName }}
        <span class="nav-badge">v{{ .version }}</span>
      </a>
      <div class="nav-links">
        <a href="/health" class="nav-link">Health</a>
        <a href="https://github.com/CodeSyncr/nimbus" target="_blank" rel="noopener" class="nav-link primary">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor"><path d="M12 2C6.477 2 2 6.484 2 12.017c0 4.425 2.865 8.18 6.839 9.504.5.092.682-.217.682-.483 0-.237-.008-.868-.013-1.703-2.782.605-3.369-1.343-3.369-1.343-.454-1.158-1.11-1.466-1.11-1.466-.908-.62.069-.608.069-.608 1.003.07 1.531 1.032 1.531 1.032.892 1.53 2.341 1.088 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.113-4.555-4.951 0-1.093.39-1.988 1.029-2.688-.103-.253-.446-1.272.098-2.65 0 0 .84-.27 2.75 1.026A9.564 9.564 0 0 1 12 6.844a9.59 9.59 0 0 1 2.504.337c1.909-1.296 2.747-1.027 2.747-1.027.546 1.379.202 2.398.1 2.651.64.7 1.028 1.595 1.028 2.688 0 3.848-2.339 4.695-4.566 4.943.359.309.678.92.678 1.855 0 1.338-.012 2.419-.012 2.747 0 .268.18.58.688.482A10.02 10.02 0 0 0 22 12.017C22 6.484 17.522 2 12 2Z"/></svg>
          GitHub
        </a>
      </div>
    </nav>

    {{ .embed }}

    <!-- FOOTER -->
    <footer>
      <div class="footer-inner">
        <p class="footer-text">
          Built with <a href="https://github.com/CodeSyncr/nimbus" target="_blank" rel="noopener">Nimbus</a>
          · AdonisJS-style framework for Go
        </p>
        <div class="footer-links">
          <a href="https://github.com/CodeSyncr/nimbus" target="_blank" rel="noopener">GitHub</a>
          <a href="/health">Health</a>
          <a href="https://github.com/CodeSyncr/nimbus/issues" target="_blank" rel="noopener">Issues</a>
        </div>
      </div>
    </footer>

  </div><!-- /.wrapper -->

</body>
</html>
`

const homeViewTmpl = `@layout('layout')

<!-- HERO -->
    <section class="hero">
      <div class="stagger-hero">
        <div>
          <div class="status-pill">
            <span class="pulse-dot"><span></span><span></span></span>
            Application running · {{ .env }} environment
          </div>
        </div>
        <h1 class="hero-title">
          Build fast.<br>
          Deploy with <span class="accent">{{ .appName }}</span>.
        </h1>
        <p class="hero-sub">{{ .tagline }}</p>
        <div class="hero-ctas">
          <a href="https://github.com/CodeSyncr/nimbus" target="_blank" rel="noopener" class="btn btn-primary">
            <svg width="15" height="15" viewBox="0 0 24 24" fill="currentColor"><path d="M12 2C6.477 2 2 6.484 2 12.017c0 4.425 2.865 8.18 6.839 9.504.5.092.682-.217.682-.483 0-.237-.008-.868-.013-1.703-2.782.605-3.369-1.343-3.369-1.343-.454-1.158-1.11-1.466-1.11-1.466-.908-.62.069-.608.069-.608 1.003.07 1.531 1.032 1.531 1.032.892 1.53 2.341 1.088 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.113-4.555-4.951 0-1.093.39-1.988 1.029-2.688-.103-.253-.446-1.272.098-2.65 0 0 .84-.27 2.75 1.026A9.564 9.564 0 0 1 12 6.844a9.59 9.59 0 0 1 2.504.337c1.909-1.296 2.747-1.027 2.747-1.027.546 1.379.202 2.398.1 2.651.64.7 1.028 1.595 1.028 2.688 0 3.848-2.339 4.695-4.566 4.943.359.309.678.92.678 1.855 0 1.338-.012 2.419-.012 2.747 0 .268.18.58.688.482A10.02 10.02 0 0 0 22 12.017C22 6.484 17.522 2 12 2Z"/></svg>
            View on GitHub
          </a>
          <a href="https://github.com/CodeSyncr/nimbus/tree/main/docs" target="_blank" rel="noopener" class="btn btn-ghost">
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14,2 14,8 20,8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/><polyline points="10,9 9,9 8,9"/></svg>
            Documentation
          </a>
          <a href="/health" class="btn btn-ghost">
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="22,12 18,12 15,21 9,3 6,12 2,12"/></svg>
            Health check
          </a>
        </div>
        <div>
          <div class="cmd-block">
            <span class="prompt">$</span>
            <span class="cmd-text">nimbus new my-app --kit=web</span>
            <button class="cmd-copy" title="Copy to clipboard" onclick="
              navigator.clipboard.writeText('nimbus new my-app --kit=web');
              this.classList.add('copied');
              this.innerHTML = '✓';
              setTimeout(() => { this.classList.remove('copied'); this.innerHTML = '⎘'; }, 1800);
            ">⎘</button>
          </div>
        </div>
      </div>
    </section>

    <!-- STATS -->
    <div class="stats-row">
      <div class="stat">
        <div class="stat-val">~0ms</div>
        <div class="stat-label">Cold start overhead</div>
      </div>
      <div class="stat">
        <div class="stat-val">Go</div>
        <div class="stat-label">Powered by Go runtime</div>
      </div>
      <div class="stat">
        <div class="stat-val">AdonisJS</div>
        <div class="stat-label">Inspired by</div>
      </div>
    </div>

    <!-- FEATURES -->
    <p class="section-label">What's included</p>
    <div class="features-grid">
      <div class="card amber fade-up">
        <div class="card-icon amber">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2L2 7l10 5 10-5-10-5z"/><path d="M2 17l10 5 10-5"/><path d="M2 12l10 5 10-5"/></svg>
        </div>
        <h3>MVC Architecture</h3>
        <p>Clean separation of concerns with Models, Views, and Controllers — batteries included, opinionated by default.</p>
      </div>
      <div class="card cyan fade-up">
        <div class="card-icon cyan">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z"/></svg>
        </div>
        <h3>Blazing Fast Router</h3>
        <p>Radix-tree based HTTP router with named params, groups, middleware, and resource routing out of the box.</p>
      </div>
      <div class="card rose fade-up">
        <div class="card-icon rose">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M9 21V9"/></svg>
        </div>
        <h3>ORM & Migrations</h3>
        <p>Expressive query builder with relationship support. Version-controlled schema migrations that just work.</p>
      </div>
      <div class="card violet fade-up">
        <div class="card-icon violet">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg>
        </div>
        <h3>Auth Scaffolding</h3>
        <p>Session, token, and OAuth-based auth with guards and middleware — generated in seconds with the CLI.</p>
      </div>
      <div class="card green fade-up">
        <div class="card-icon green">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg>
        </div>
        <h3>Nimbus Templates</h3>
        <p>Server-side HTML templating with layouts, partials, and live hot-reload. Edit and see changes instantly.</p>
      </div>
      <div class="card sky fade-up">
        <div class="card-icon sky">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.07 4.93a10 10 0 0 1 0 14.14M4.93 4.93a10 10 0 0 0 0 14.14"/></svg>
        </div>
        <h3>Event System</h3>
        <p>Typed event emitter and listener system. Decouple your business logic with first-class async event handling.</p>
      </div>
    </div>

    <!-- CODE EXAMPLE -->
    <div class="code-section">
      <p class="section-label">Looks like this</p>
      <div class="code-window">
        <div class="code-titlebar">
          <div class="code-dots">
            <div class="code-dot"></div>
            <div class="code-dot"></div>
            <div class="code-dot"></div>
          </div>
          <span class="code-filename">app/controllers/users_controller.go</span>
          <span></span>
        </div>
        <pre><span class="kw">package</span> controllers

<span class="kw">import</span> (
  <span class="str">"github.com/CodeSyncr/nimbus/http"</span>
  <span class="str">"your-app/app/models"</span>
)

<span class="cmt">// Index returns a paginated list of users</span>
<span class="kw">func</span> (<span class="type">UsersController</span>) <span class="fn">Index</span>(ctx <span class="op">*</span><span class="type">http.Context</span>) <span class="kw">error</span> {
  users, err <span class="op">:=</span> models.<span class="type">User</span>.<span class="fn">Query</span>().
    <span class="fn">Where</span>(<span class="str">"active"</span>, <span class="num">true</span>).
    <span class="fn">OrderBy</span>(<span class="str">"created_at"</span>, <span class="str">"desc"</span>).
    <span class="fn">Paginate</span>(ctx.<span class="fn">QueryInt</span>(<span class="str">"page"</span>, <span class="num">1</span>), <span class="num">20</span>)

  <span class="kw">if</span> err <span class="op">!=</span> <span class="num">nil</span> {
    <span class="kw">return</span> ctx.<span class="fn">InternalServerError</span>(err)
  }

  <span class="kw">return</span> ctx.<span class="fn">View</span>(<span class="str">"users/index"</span>, <span class="type">ViewData</span>{
    <span class="str">"users"</span>: users,
    <span class="str">"meta"</span>:  users.<span class="fn">Meta</span>(),
  })
}</pre>
      </div>
    </div>
`
