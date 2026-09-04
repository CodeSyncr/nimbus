package nimbus

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	stdhttp "net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/CodeSyncr/nimbus/cli"
	"github.com/CodeSyncr/nimbus/config"
	"github.com/CodeSyncr/nimbus/container"
	"github.com/CodeSyncr/nimbus/errors"
	"github.com/CodeSyncr/nimbus/events"
	"github.com/CodeSyncr/nimbus/health"
	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/internal/startupview"
	"github.com/CodeSyncr/nimbus/locale"
	"github.com/CodeSyncr/nimbus/openapi"
	"github.com/CodeSyncr/nimbus/router"
	"github.com/CodeSyncr/nimbus/schedule"
)

// Provider is the service provider interface (AdonisJS/Laravel style).
// Register runs first (bind services); Boot runs after all providers are registered.
type Provider interface {
	Register(app *App) error
	Boot(app *App) error
}

// HasStart can be optionally implemented by a Provider or Plugin to execute logic
// right before the HTTP server begins serving requests (skipped in ModeWarmup).
type HasStart interface {
	Start(app *App) error
}

// AppMode represents the operational mode of the application (AdonisJS-inspired).
type AppMode string

const (
	ModeRun    AppMode = "run"    // Default: full server execution
	ModeWarmup AppMode = "warmup" // Assembly & inspection only (no server, workers, or listeners)
	ModeTest   AppMode = "test"   // Testing environment
	ModeCli    AppMode = "cli"    // CLI command execution
)

// AppState represents the lifecycle stage of the application.
type AppState string

const (
	StateInitiated   AppState = "initiated"
	StateBooting     AppState = "booting"
	StateBooted      AppState = "booted"
	StateWarming     AppState = "warming"
	StateWarmed      AppState = "warmed"
	StateStarting    AppState = "starting"
	StateReady       AppState = "ready"
	StateTerminating AppState = "terminating"
	StateTerminated  AppState = "terminated"
)

// Option is a functional option for configuring a Nimbus App.
type Option func(*App)

// WithMode sets the initial operational mode of the App.
func WithMode(m AppMode) Option {
	return func(a *App) {
		a.mode = m
	}
}

// WithConfig overrides the configuration used by the App.
func WithConfig(cfg *config.Config) Option {
	return func(a *App) {
		a.Config = cfg
	}
}

// WithPort overrides the port that the App listens on.
func WithPort(port string) Option {
	return func(a *App) {
		if a.Config != nil {
			a.Config.App.Port = port
		}
		if a.Server != nil {
			a.Server.Addr = ":" + port
		}
	}
}

// App is the core Nimbus application (AdonisJS-style).
type App struct {
	Config          *config.Config
	Router          *router.Router
	Server          *stdhttp.Server
	Container       *container.Container
	Events          *events.Dispatcher
	Scheduler       *schedule.Scheduler
	Health          *health.Checker
	providers       []Provider
	plugins         []Plugin
	pluginIndex     map[string]Plugin
	namedMiddleware map[string]router.Middleware
	pluginConfigs   map[string]map[string]any

	mu         sync.RWMutex
	mode       AppMode
	state      AppState
	isBooted   bool
	isWarmedUp bool

	bootHooks     []func(*App)
	warmHooks     []func(*App)
	startHooks    []func(*App)
	shutdownHooks []func(*App)
}

// New creates a new Nimbus application with default config and optional functional options.
func New(opts ...Option) *App {
	cfg := config.Load()
	locale.BootFromEnv()
	r := router.New()
	r.Fallback(errors.NotFoundHandler())
	app := &App{
		Config:          cfg,
		Router:          r,
		Container:       container.New(),
		Events:          events.New(),
		Scheduler:       schedule.New(),
		Health:          health.New(),
		pluginIndex:     make(map[string]Plugin),
		namedMiddleware: make(map[string]router.Middleware),
		pluginConfigs:   make(map[string]map[string]any),
		mode:            ModeRun,
		state:           StateInitiated,
	}
	app.Router.Container = app.Container
	app.Server = &stdhttp.Server{
		Addr:    ":" + cfg.App.Port,
		Handler: r,
		// Production-safe timeouts guard against slow/malicious clients
		// (Slowloris). Tunable via SERVER_*_TIMEOUT env vars; see config.AppConfig.
		ReadTimeout:       cfg.App.ReadTimeout,
		ReadHeaderTimeout: cfg.App.ReadHeaderTimeout,
		WriteTimeout:      cfg.App.WriteTimeout,
		IdleTimeout:       cfg.App.IdleTimeout,
		MaxHeaderBytes:    cfg.App.MaxHeaderBytes,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(app)
		}
	}
	app.registerDefaultHealthRoutes()
	return app
}

func (a *App) registerDefaultHealthRoutes() {
	// Liveness: process is up.
	a.Router.Get("/livez", func(c *nhttp.Context) error {
		return c.JSON(stdhttp.StatusOK, map[string]string{"status": "ok"})
	})

	// Readiness: dependencies registered in app.Health are healthy.
	a.Router.Get("/readyz", func(c *nhttp.Context) error {
		result := a.Health.Run(c.Ctx())
		code := stdhttp.StatusOK
		if result.Status != "ok" {
			code = stdhttp.StatusServiceUnavailable
		}
		return c.JSON(code, result)
	})

	// Keep legacy /health as readiness-compatible endpoint.
	a.Router.Get("/health", func(c *nhttp.Context) error {
		result := a.Health.Run(c.Ctx())
		code := stdhttp.StatusOK
		if result.Status != "ok" {
			code = stdhttp.StatusServiceUnavailable
		}
		return c.JSON(code, result)
	})
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
// Mode & State Accessors
// ---------------------------------------------------------------------------

// GetMode returns the current application operational mode. Defaults to ModeRun.
func (a *App) GetMode() AppMode {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.mode == "" {
		return ModeRun
	}
	return a.mode
}

// Mode returns the current application operational mode (shorthand for GetMode).
func (a *App) Mode() AppMode {
	return a.GetMode()
}

// SetMode configures the application operational mode (e.g. ModeRun, ModeWarmup, ModeTest, ModeCli).
func (a *App) SetMode(m AppMode) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mode = m
}

// State returns the current lifecycle state of the application.
func (a *App) State() AppState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.state == "" {
		return StateInitiated
	}
	return a.state
}

// IsWarmedUp returns true if the application has completed its warmup phase.
func (a *App) IsWarmedUp() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.isWarmedUp
}

// IsBooted returns true if the application has completed its boot phase.
func (a *App) IsBooted() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.isBooted
}

// IsWarmup returns true if the application mode is ModeWarmup.
func (a *App) IsWarmup() bool {
	return a.GetMode() == ModeWarmup
}

// IsRun returns true if the application mode is ModeRun.
func (a *App) IsRun() bool {
	return a.GetMode() == ModeRun
}

// IsTest returns true if the application mode is ModeTest.
func (a *App) IsTest() bool {
	return a.GetMode() == ModeTest
}

// IsCli returns true if the application mode is ModeCli.
func (a *App) IsCli() bool {
	return a.GetMode() == ModeCli
}

// ServeHTTP implements net/http.Handler directly on *App, dispatching
// requests to the underlying router. This allows testing with httptest or
// embedding the Nimbus App directly as an http.Handler.
func (a *App) ServeHTTP(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	a.Router.ServeHTTP(w, r)
}

// ---------------------------------------------------------------------------
// Lifecycle Hooks
// ---------------------------------------------------------------------------

// OnBoot registers a callback that runs after providers/plugins have been
// booted and plugin routes/middleware have been applied, but before warmup
// completes and before the server starts listening.
func (a *App) OnBoot(fn func(*App)) {
	if fn == nil {
		return
	}
	a.bootHooks = append(a.bootHooks, fn)
}

// OnWarmup registers a callback that runs during the WarmUp phase, after
// Boot has completed but before the HTTP server starts listening.
func (a *App) OnWarmup(fn func(*App)) {
	if fn == nil {
		return
	}
	a.warmHooks = append(a.warmHooks, fn)
}

// OnStart registers a callback that runs right before the HTTP server begins
// serving requests (after Boot/WarmUp and listen/port selection).
func (a *App) OnStart(fn func(*App)) {
	if fn == nil {
		return
	}
	a.startHooks = append(a.startHooks, fn)
}

// shutdownTimeout is the grace period given to in-flight requests during a
// graceful shutdown. Configurable via SERVER_SHUTDOWN_TIMEOUT; falls back to
// 10s if unset (e.g. an App constructed without config.Load).
func (a *App) shutdownTimeout() time.Duration {
	if a.Config != nil && a.Config.App.ShutdownTimeout > 0 {
		return a.Config.App.ShutdownTimeout
	}
	return 10 * time.Second
}

// OnShutdown registers a callback that runs during graceful shutdown, before
// plugin and provider HasShutdown hooks are executed.
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
//  7. App-level boot hooks
func (a *App) Boot() error {
	a.mu.Lock()
	if a.isBooted {
		a.mu.Unlock()
		return nil
	}
	a.state = StateBooting
	a.mu.Unlock()

	// Pass 0 — Fail-closed config validation. Refuse to boot in production
	// with a missing/weak APP_KEY; warn (don't block) in development.
	if a.Config != nil {
		warnings, err := a.Config.Validate()
		if err != nil {
			return err
		}
		for _, w := range warnings {
			fmt.Fprintf(os.Stderr, "  \033[33m⚠\033[0m  config: %s\n", w)
		}
	}

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

	a.mu.Lock()
	a.isBooted = true
	a.state = StateBooted
	a.mu.Unlock()
	a.Events.Dispatch(events.AppBooted, nil)
	return nil
}

// ---------------------------------------------------------------------------
// WarmUp & Tooling
// ---------------------------------------------------------------------------

// WarmUp executes provider boot, plugin assembly, and warmup hooks without
// starting network listeners, queue consumers, or background schedulers.
// It allows full application inspection (e.g. reading routes for codegen,
// CLI commands, or running unit tests). Calling WarmUp on an already warmed
// app is a safe no-op.
func (a *App) WarmUp() error {
	a.mu.Lock()
	if a.isWarmedUp {
		a.mu.Unlock()
		return nil
	}
	a.mu.Unlock()

	if !a.IsBooted() {
		if err := a.Boot(); err != nil {
			return err
		}
	}

	a.mu.Lock()
	a.state = StateWarming
	a.mu.Unlock()

	for _, fn := range a.warmHooks {
		fn(a)
	}

	a.mu.Lock()
	a.isWarmedUp = true
	a.state = StateWarmed
	a.mu.Unlock()

	a.Events.Dispatch(events.AppWarmed, a)
	return nil
}

// DumpRoutes exports the application router manifest to the specified output
// directory (defaults to ".nimbus-client"). It automatically warms up the app.
func (a *App) DumpRoutes(outDir ...string) error {
	if !a.IsWarmedUp() {
		if err := a.WarmUp(); err != nil {
			return err
		}
	}
	target := ".nimbus-client"
	if len(outDir) > 0 && outDir[0] != "" {
		target = outDir[0]
	}
	return router.WriteManifest(a.Router, target)
}

// DumpOpenAPI generates and writes the OpenAPI 3.0 specification to the given
// output file or directory (defaults to "openapi.json"). It automatically warms up the app.
func (a *App) DumpOpenAPI(outPath ...string) error {
	if !a.IsWarmedUp() {
		if err := a.WarmUp(); err != nil {
			return err
		}
	}
	target := "openapi.json"
	if len(outPath) > 0 && outPath[0] != "" {
		target = outPath[0]
	}
	title := "Nimbus API"
	if a.Config != nil && a.Config.App.Name != "" {
		title = a.Config.App.Name
	}
	spec := openapi.NewGenerator(openapi.GeneratorConfig{
		Title:   title,
		Version: "1.0.0",
	}).Generate(a.Router.Routes())
	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return fmt.Errorf("nimbus: marshal openapi: %w", err)
	}
	if strings.HasSuffix(target, ".json") {
		dir := filepath.Dir(target)
		if dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return err
			}
		}
		return os.WriteFile(target, data, 0644)
	}
	if err := os.MkdirAll(target, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(target, "openapi.json"), data, 0644)
}

// Shutdown calls Shutdown on every provider and plugin that implements HasShutdown.
// If the app is in ModeWarmup, provider and plugin shutdown hooks are skipped to avoid
// constructing lazy resources just to tear them down.
func (a *App) Shutdown() error {
	a.mu.Lock()
	a.state = StateTerminating
	a.mu.Unlock()

	for i := len(a.shutdownHooks) - 1; i >= 0; i-- {
		a.shutdownHooks[i](a)
	}
	if a.GetMode() != ModeWarmup {
		// Providers shutdown
		for i := len(a.providers) - 1; i >= 0; i-- {
			if hs, ok := a.providers[i].(HasShutdown); ok {
				if err := hs.Shutdown(); err != nil {
					return fmt.Errorf("provider shutdown: %w", err)
				}
			}
		}
		// Plugins shutdown
		for i := len(a.plugins) - 1; i >= 0; i-- {
			if hs, ok := a.plugins[i].(HasShutdown); ok {
				if err := hs.Shutdown(); err != nil {
					return fmt.Errorf("plugin %s shutdown: %w", a.plugins[i].Name(), err)
				}
			}
		}
	}

	a.mu.Lock()
	a.state = StateTerminated
	a.mu.Unlock()
	return nil
}

// ---------------------------------------------------------------------------
// Run
// ---------------------------------------------------------------------------

// Run boots providers and plugins, warms up the app, executes HasStart hooks,
// then starts the HTTP server.
// If the configured port is busy, it automatically picks a free port.
// Listens for SIGINT/SIGTERM and gracefully shuts down to release the port.
// Calling Run on an app configured with ModeWarmup returns an error.
//
// dumpRoutesIfRequested writes the route manifest and reports done=true when
// NIMBUS_DUMP_ROUTES is set, so the caller skips serving. The output directory
// defaults to ".nimbus-client" and can be overridden with NIMBUS_CLIENT_OUT.
//
//	NIMBUS_DUMP_ROUTES=1 go run .
//	nimbus gen:client
func (a *App) dumpRoutesIfRequested() (bool, error) {
	if os.Getenv("NIMBUS_DUMP_ROUTES") == "" {
		return false, nil
	}
	outDir := os.Getenv("NIMBUS_CLIENT_OUT")
	if outDir == "" {
		outDir = ".nimbus-client"
	}
	if err := a.DumpRoutes(outDir); err != nil {
		return true, fmt.Errorf("nimbus: dump routes: %w", err)
	}
	fmt.Printf("Wrote %s/%s (%d routes). Now run: nimbus gen:client\n",
		outDir, router.ManifestFileName, len(a.Router.Routes()))
	return true, nil
}

func (a *App) Run() error {
	if a.GetMode() == ModeWarmup {
		return fmt.Errorf("nimbus: cannot run application in %s mode", a.GetMode())
	}
	// Measured from here to the moment the listener is up, which is what
	// "Ready in" means to someone watching the terminal.
	bootStart := time.Now()
	configureGOGCFromEnv()
	startPprofIfEnabled()
	if !a.IsWarmedUp() {
		if err := a.WarmUp(); err != nil {
			return err
		}
	}
	// NIMBUS_DUMP_ROUTES=1 writes the route manifest (consumed by
	// `nimbus gen:client`) and exits without serving.
	if done, err := a.dumpRoutesIfRequested(); done || err != nil {
		return err
	}
	ln, port, err := a.listen()
	if err != nil {
		return err
	}
	a.Config.App.Port = port
	a.printStartup("http", port, time.Since(bootStart))

	a.mu.Lock()
	a.state = StateStarting
	a.mu.Unlock()

	// Provider HasStart hooks
	for _, p := range a.providers {
		if hs, ok := p.(HasStart); ok {
			if err := hs.Start(a); err != nil {
				return fmt.Errorf("provider start: %w", err)
			}
		}
	}

	// Plugin HasStart hooks
	for _, p := range a.plugins {
		if hs, ok := p.(HasStart); ok {
			if err := hs.Start(a); err != nil {
				return fmt.Errorf("plugin %s start: %w", p.Name(), err)
			}
		}
	}

	for _, fn := range a.startHooks {
		fn(a)
	}
	a.Events.Dispatch(events.AppStarted, port)

	// Start scheduler if tasks were registered.
	schedulerCtx, schedulerCancel := context.WithCancel(context.Background())
	defer schedulerCancel()
	if a.Scheduler.Count() > 0 {
		a.Scheduler.Start(schedulerCtx)
	}

	a.mu.Lock()
	a.state = StateReady
	a.mu.Unlock()
	a.Events.Dispatch(events.AppReady, port)

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
		ctx, cancel := context.WithTimeout(context.Background(), a.shutdownTimeout())
		defer cancel()
		if err := a.Server.Shutdown(ctx); err != nil {
			return fmt.Errorf("server shutdown: %w", err)
		}
		a.Scheduler.Stop()
		_ = a.Shutdown()
		return nil
	case err := <-serveErr:
		a.Scheduler.Stop()
		return err
	}
}

// RunTLS starts the HTTP server with TLS.
// If the configured port is busy, it automatically picks a free port.
// Listens for SIGINT/SIGTERM and gracefully shuts down to release the port.
// Calling RunTLS on an app configured with ModeWarmup returns an error.
func (a *App) RunTLS(certFile, keyFile string) error {
	bootStart := time.Now()
	if a.GetMode() == ModeWarmup {
		return fmt.Errorf("nimbus: cannot run application in %s mode", a.GetMode())
	}
	if !a.IsWarmedUp() {
		if err := a.WarmUp(); err != nil {
			return err
		}
	}
	ln, port, err := a.listen()
	if err != nil {
		return err
	}
	a.Config.App.Port = port
	a.printStartup("https", port, time.Since(bootStart))

	a.mu.Lock()
	a.state = StateStarting
	a.mu.Unlock()

	// Provider HasStart hooks
	for _, p := range a.providers {
		if hs, ok := p.(HasStart); ok {
			if err := hs.Start(a); err != nil {
				return fmt.Errorf("provider start: %w", err)
			}
		}
	}

	// Plugin HasStart hooks
	for _, p := range a.plugins {
		if hs, ok := p.(HasStart); ok {
			if err := hs.Start(a); err != nil {
				return fmt.Errorf("plugin %s start: %w", p.Name(), err)
			}
		}
	}

	for _, fn := range a.startHooks {
		fn(a)
	}
	a.Events.Dispatch(events.AppStarted, port)

	schedulerCtx, schedulerCancel := context.WithCancel(context.Background())
	defer schedulerCancel()
	if a.Scheduler.Count() > 0 {
		a.Scheduler.Start(schedulerCtx)
	}

	a.mu.Lock()
	a.state = StateReady
	a.mu.Unlock()
	a.Events.Dispatch(events.AppReady, port)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- a.Server.ServeTLS(ln, certFile, keyFile)
	}()

	select {
	case sig := <-quit:
		a.Events.Dispatch(events.AppShutdown, sig)
		fmt.Printf("\n  \033[33m⚠\033[0m  Received %v, shutting down...\n", sig)
		ctx, cancel := context.WithTimeout(context.Background(), a.shutdownTimeout())
		defer cancel()
		if err := a.Server.Shutdown(ctx); err != nil {
			return fmt.Errorf("server shutdown: %w", err)
		}
		a.Scheduler.Stop()
		_ = a.Shutdown()
		return nil
	case err := <-serveErr:
		a.Scheduler.Stop()
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

func (a *App) printStartup(scheme, port string, booted time.Duration) {
	info := a.startupInfo(scheme, port, booted)

	// Under `nimbus serve` stdout is a pipe into Air, so the app can neither
	// colour the view nor tell the CLI when to stop its spinner. It hands the
	// whole report over as one marker line and the CLI draws it instead.
	if os.Getenv("NIMBUS_SERVE") == "1" {
		fmt.Fprintln(os.Stdout, info.Marker())
		return
	}

	fmt.Fprint(os.Stdout, startupview.Render(info))
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
		if err := stdhttp.ListenAndServe(addr, nil); err != nil && err != stdhttp.ErrServerClosed {
			log.Printf("[nimbus] pprof server error: %v\n", err)
		}
	}()
}
