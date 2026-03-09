package nimbus

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/CodeSyncr/nimbus/config"
	"github.com/CodeSyncr/nimbus/container"
	"github.com/CodeSyncr/nimbus/router"
)

// Provider is the service provider interface (AdonisJS/Laravel style).
// Register runs first (bind services); Boot runs after all providers are registered.
type Provider interface {
	Register(app *App) error
	Boot(app *App) error
}

// App is the core Nimbus application (AdonisJS-style).
type App struct {
	Config    *config.Config
	Router    *router.Router
	Server    *http.Server
	Container *container.Container

	providers       []Provider
	plugins         []Plugin
	pluginIndex     map[string]Plugin
	namedMiddleware map[string]router.Middleware
	pluginConfigs   map[string]map[string]any
}

// New creates a new Nimbus application with default config.
func New() *App {
	cfg := config.Load()
	r := router.New()
	app := &App{
		Config:          cfg,
		Router:          r,
		Container:       container.New(),
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

	// Pass 2 — Plugin.Register
	for _, p := range a.plugins {
		if err := p.Register(a); err != nil {
			return fmt.Errorf("plugin %s register: %w", p.Name(), err)
		}
	}

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

	// Pass 5 — Plugin.Boot
	for _, p := range a.plugins {
		if err := p.Boot(a); err != nil {
			return fmt.Errorf("plugin %s boot: %w", p.Name(), err)
		}
	}

	// Pass 6 — Apply plugin capabilities
	for _, p := range a.plugins {
		if hr, ok := p.(HasRoutes); ok {
			hr.RegisterRoutes(a.Router)
		}
		if hm, ok := p.(HasMiddleware); ok {
			for name, mw := range hm.Middleware() {
				a.namedMiddleware[name] = mw
			}
		}
	}

	return nil
}

// Shutdown calls Shutdown on every plugin that implements HasShutdown.
func (a *App) Shutdown() error {
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
	if err := a.Boot(); err != nil {
		return err
	}
	ln, port, err := a.listen()
	if err != nil {
		return err
	}
	a.Config.App.Port = port
	a.printStartup("http", port)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- a.Server.Serve(ln)
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
	url := fmt.Sprintf("%s://localhost:%s", scheme, port)

	fmt.Printf("  \033[32m➜\033[0m  \033[1m%s\033[0m running at \033[36m%s\033[0m\n", name, url)
	fmt.Printf("  \033[2m   env: %s · %d plugin(s) loaded\033[0m\n\n", env, len(a.plugins))
}
