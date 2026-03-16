package nimbus

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/CodeSyncr/nimbus/cli"
	"github.com/CodeSyncr/nimbus/config"
	"github.com/CodeSyncr/nimbus/container"
	"github.com/CodeSyncr/nimbus/events"
	"github.com/CodeSyncr/nimbus/health"
	"github.com/CodeSyncr/nimbus/router"
	"github.com/CodeSyncr/nimbus/schedule"
)

// Provider is the service provider interface (AdonisJS/Laravel style).
// Register runs first (bind services); Boot runs after all providers are registered.
type Provider interface {
	Register(app *App) error
	Boot(app *App) error
}

// App is the core Nimbus application (AdonisJS-style).
type App struct {
	Config          *config.Config
	Router          *router.Router
	Server          *http.Server
	Container       *container.Container
	Events          *events.Dispatcher
	Scheduler       *schedule.Scheduler
	Health          *health.Checker
	providers       []Provider
	plugins         []Plugin
	pluginIndex     map[string]Plugin
	namedMiddleware map[string]router.Middleware
	pluginConfigs   map[string]map[string]any

	bootHooks     []func(*App)
	startHooks    []func(*App)
	shutdownHooks []func(*App)
}

// New creates a new Nimbus application with default config.
func New() *App {
	cfg := config.Load()
	r := router.New()
	app := &App{
		Config:          cfg,
		Router:          r,
		Container:       container.New(),
		Events:          events.New(),
		Scheduler:       schedule.New(),
		Health:          health.New(),
		Server:          &http.Server{Addr: ":" + cfg.App.Port, Handler: r},
		pluginIndex:     make(map[string]Plugin),
		namedMiddleware: make(map[string]router.Middleware),
		pluginConfigs:   make(map[string]map[string]any),
	}
	return app
}

// ---------------------------------------------------------------------------
// Providers
// ---------------------------------------------------------------------------

// Register adds a service provider. Call before Run.
func (a *App) Register(p Provider) {
	a.providers = append(a.providers, p)
}

// ---------------------------------------------------------------------------
// Plugins
// ---------------------------------------------------------------------------

// Use registers one or more plugins with the application.
// Call in bin/server.go before app.Run().
//
//	app.Use(
//	    &auth.Plugin{},
//	    &redis.Plugin{},
//	)
func (a *App) Use(plugins ...Plugin) {
	for _, p := range plugins {
		a.plugins = append(a.plugins, p)
		a.pluginIndex[p.Name()] = p
	}
}

// Plugin returns a registered plugin by name, or nil if not found.
func (a *App) Plugin(name string) Plugin {
	return a.pluginIndex[name]
}

// Plugins returns all registered plugins in registration order.
func (a *App) Plugins() []Plugin {
	return a.plugins
}

// NamedMiddleware returns the merged map of named middleware from all
// plugins. Use in start/kernel.go or start/routes.go.
func (a *App) NamedMiddleware() map[string]router.Middleware {
	return a.namedMiddleware
}

// PluginConfig returns the merged default config for a plugin, or nil.
func (a *App) PluginConfig(name string) map[string]any {
	return a.pluginConfigs[name]
}

// ---------------------------------------------------------------------------
// Lifecycle Hooks
// ---------------------------------------------------------------------------

// OnBoot registers a callback that runs after providers/plugins have been
// booted and plugin routes/middleware have been applied, but before the
// server starts listening.
func (a *App) OnBoot(fn func(*App)) {
	if fn == nil {
		return
	}
	a.bootHooks = append(a.bootHooks, fn)
}

// OnStart registers a callback that runs right before the HTTP server begins
// serving requests (after Boot and listen/port selection).
func (a *App) OnStart(fn func(*App)) {
	if fn == nil {
		return
	}
	a.startHooks = append(a.startHooks, fn)
}

// OnShutdown registers a callback that runs during graceful shutdown, before
// plugin HasShutdown hooks are executed.
func (a *App) OnShutdown(fn func(*App)) {
	if fn == nil {
		return
	}
	a.shutdownHooks = append(a.shutdownHooks, fn)
}

// ---------------------------------------------------------------------------
// Boot
// ---------------------------------------------------------------------------

// Boot runs the full initialisation sequence:
//
//  1. Provider Register (all)
//  2. Plugin Register (all) — bind services
//  3. Plugin DefaultConfig collected
//  4. Provider Boot (all)
//  5. Plugin Boot (all)
//  6. Plugin capabilities applied (routes, middleware, views)
func (a *App) Boot() error {
	// Pass 1 — Provider.Register
	for _, p := range a.providers {
		if err := p.Register(a); err != nil {
			return fmt.Errorf("provider register: %w", err)
		}
	}
	a.Events.Dispatch(events.ProviderRegister, nil)

	// Pass 2 — Plugin.Register + HasBindings
	for _, p := range a.plugins {
		if err := p.Register(a); err != nil {
			return fmt.Errorf("plugin %s register: %w", p.Name(), err)
		}
		if hb, ok := p.(HasBindings); ok {
			hb.Bindings(a.Container)
		}
	}
	a.Events.Dispatch(events.PluginRegister, nil)

	// Pass 3 — Collect plugin default configs
	for _, p := range a.plugins {
		if hc, ok := p.(HasConfig); ok {
			a.pluginConfigs[p.Name()] = hc.DefaultConfig()
		}
	}

	// Pass 4 — Provider.Boot
	for _, p := range a.providers {
		if err := p.Boot(a); err != nil {
			return fmt.Errorf("provider boot: %w", err)
		}
	}
	a.Events.Dispatch(events.ProviderBoot, nil)

	// Pass 5 — Plugin.Boot
	for _, p := range a.plugins {
		if err := p.Boot(a); err != nil {
			return fmt.Errorf("plugin %s boot: %w", p.Name(), err)
		}
	}
	a.Events.Dispatch(events.PluginBoot, nil)

	// Pass 6 — Apply plugin capabilities
	for _, p := range a.plugins {
		// Routes
		if hr, ok := p.(HasRoutes); ok {
			hr.RegisterRoutes(a.Router)
		}
		// Named middleware
		if hm, ok := p.(HasMiddleware); ok {
			for name, mw := range hm.Middleware() {
				a.namedMiddleware[name] = mw
			}
		}
	}
	a.Events.Dispatch(events.RouteRegistered, nil)
	a.Events.Dispatch(events.MiddlewareRegistered, nil)

	// Pass 6b — Remaining capabilities (commands, schedule, events, health)
	for _, p := range a.plugins {
		// CLI commands
		if hcmd, ok := p.(HasCommands); ok {
			for _, cmd := range hcmd.Commands() {
				cli.RegisterCommand(cmd)
			}
		}
		// Scheduled tasks
		if hs, ok := p.(HasSchedule); ok {
			hs.Schedule(a.Scheduler)
		}
		// Event listeners
		if he, ok := p.(HasEvents); ok {
			for event, listeners := range he.Listeners() {
				for _, ln := range listeners {
					a.Events.Listen(event, ln)
				}
			}
		}
		// Health checks
		if hh, ok := p.(HasHealthChecks); ok {
			for name, check := range hh.HealthChecks() {
				a.Health.Add(name, check)
			}
		}
	}

	// Pass 7 — App-level boot hooks
	for _, fn := range a.bootHooks {
		fn(a)
	}

	a.Events.Dispatch(events.AppBooted, nil)
	return nil
}

// Shutdown calls Shutdown on every plugin that implements HasShutdown.
func (a *App) Shutdown() error {
	for i := len(a.shutdownHooks) - 1; i >= 0; i-- {
		a.shutdownHooks[i](a)
	}
	for i := len(a.plugins) - 1; i >= 0; i-- {
		if hs, ok := a.plugins[i].(HasShutdown); ok {
			if err := hs.Shutdown(); err != nil {
				return fmt.Errorf("plugin %s shutdown: %w", a.plugins[i].Name(), err)
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Run
// ---------------------------------------------------------------------------

// Run boots providers and plugins, then starts the HTTP server.
// If the configured port is busy, it automatically picks a free port.
// Listens for SIGINT/SIGTERM and gracefully shuts down to release the port.
func (a *App) Run() error {
	configureGOGCFromEnv()
	startPprofIfEnabled()
	if err := a.Boot(); err != nil {
		return err
	}
	ln, port, err := a.listen()
	if err != nil {
		return err
	}
	a.Config.App.Port = port
	a.printStartup("http", port)

	for _, fn := range a.startHooks {
		fn(a)
	}
	a.Events.Dispatch(events.AppStarted, port)

	// Start scheduler if tasks were registered.
	if a.Scheduler.Count() > 0 {
		a.Scheduler.Start(context.Background())
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- a.Server.Serve(ln)
	}()

	select {
	case sig := <-quit:
		a.Events.Dispatch(events.AppShutdown, sig)
		fmt.Printf("\n  \033[33m⚠\033[0m  Received %v, shutting down...\n", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := a.Server.Shutdown(ctx); err != nil {
			return fmt.Errorf("server shutdown: %w", err)
		}
		a.Scheduler.Stop()
		_ = a.Shutdown()
		return nil
	case err := <-serveErr:
		return err
	}
}

// RunTLS starts the HTTP server with TLS.
// If the configured port is busy, it automatically picks a free port.
// Listens for SIGINT/SIGTERM and gracefully shuts down to release the port.
func (a *App) RunTLS(certFile, keyFile string) error {
	if err := a.Boot(); err != nil {
		return err
	}
	ln, port, err := a.listen()
	if err != nil {
		return err
	}
	a.Config.App.Port = port
	a.printStartup("https", port)

	for _, fn := range a.startHooks {
		fn(a)
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- a.Server.ServeTLS(ln, certFile, keyFile)
	}()

	select {
	case sig := <-quit:
		fmt.Printf("\n  \033[33m⚠\033[0m  Received %v, shutting down...\n", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := a.Server.Shutdown(ctx); err != nil {
			return fmt.Errorf("server shutdown: %w", err)
		}
		_ = a.Shutdown()
		return nil
	case err := <-serveErr:
		return err
	}
}

// listen tries the configured port first. If it's already in use,
// it binds to ":0" and lets the OS assign a free port.
func (a *App) listen() (net.Listener, string, error) {
	addr := ":" + a.Config.App.Port
	ln, err := net.Listen("tcp", addr)
	if err == nil {
		return ln, a.Config.App.Port, nil
	}

	ln, err = net.Listen("tcp", ":0")
	if err != nil {
		return nil, "", fmt.Errorf("nimbus: unable to listen on %s or any free port: %w", addr, err)
	}
	freePort := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	fmt.Printf("  \033[33m⚠\033[0m  Port %s is busy, using :%s\n", a.Config.App.Port, freePort)
	a.Server.Addr = ":" + freePort
	return ln, freePort, nil
}

func (a *App) printStartup(scheme, port string) {
	env := a.Config.App.Env
	if env == "" {
		env = "development"
	}
	name := a.Config.App.Name
	if name == "" {
		name = "nimbus"
	}

	// When running under `nimbus serve`, emit a machine-readable marker
	// that the CLI's airFilter parses for beautiful display.
	if os.Getenv("NIMBUS_SERVE") == "1" {
		fmt.Fprintf(os.Stdout, "__NIMBUS_READY__|%s|%s|%s|%s|%d\n", scheme, port, name, env, len(a.plugins))
		return
	}

	// Direct run — human-readable output.
	url := fmt.Sprintf("%s://localhost:%s", scheme, port)
	fmt.Printf("\n  \033[32m✓\033[0m  \033[1m%s\033[0m is ready\n\n", name)
	fmt.Printf("  \033[32m➜\033[0m  Local: \033[1;36m%s\033[0m\n", url)
	fmt.Printf("  \033[2m     env: %s · %d plugin(s)\033[0m\n\n", env, len(a.plugins))
}

// configureGOGCFromEnv reads NIMBUS_GOGC and applies it via debug.SetGCPercent.
// Examples:
//
//	NIMBUS_GOGC=50   → aggressive GC
//	NIMBUS_GOGC=100  → default
//	NIMBUS_GOGC=200  → fewer GC cycles
//	NIMBUS_GOGC=off  → disable GC (not recommended in production)
func configureGOGCFromEnv() {
	val := strings.TrimSpace(os.Getenv("NIMBUS_GOGC"))
	if val == "" {
		return
	}
	if strings.EqualFold(val, "off") {
		debug.SetGCPercent(-1)
		log.Println("[nimbus] GC disabled via NIMBUS_GOGC=off (not recommended in production)")
		return
	}
	percent, err := strconv.Atoi(val)
	if err != nil {
		log.Printf("[nimbus] invalid NIMBUS_GOGC value %q (expected integer or \"off\")\n", val)
		return
	}
	debug.SetGCPercent(percent)
	log.Printf("[nimbus] GC percent set to %d via NIMBUS_GOGC\n", percent)
}

// startPprofIfEnabled starts a pprof HTTP server when NIMBUS_PPROF is set.
// By default it listens on :6060 and exposes /debug/pprof endpoints.
// You can override the address by setting NIMBUS_PPROF to a full address,
// e.g. NIMBUS_PPROF="127.0.0.1:6060".
func startPprofIfEnabled() {
	val := strings.TrimSpace(os.Getenv("NIMBUS_PPROF"))
	if val == "" || strings.EqualFold(val, "off") || val == "0" {
		return
	}
	addr := ":6060"
	if strings.Contains(val, ":") {
		addr = val
	}
	go func() {
		log.Printf("[nimbus] pprof server listening on %s (set NIMBUS_PPROF=off to disable)\n", addr)
		if err := http.ListenAndServe(addr, nil); err != nil && err != http.ErrServerClosed {
			log.Printf("[nimbus] pprof server error: %v\n", err)
		}
	}()
}
