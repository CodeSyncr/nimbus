package nimbus

import (
	"fmt"
	"net/http"

	"github.com/nimbus-framework/nimbus/config"
	"github.com/nimbus-framework/nimbus/container"
	"github.com/nimbus-framework/nimbus/router"
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
	providers []Provider
}

// New creates a new Nimbus application with default config.
func New() *App {
	cfg := config.Load()
	r := router.New()
	app := &App{
		Config:    cfg,
		Router:    r,
		Container: container.New(),
		providers: nil,
		Server: &http.Server{
			Addr:    ":" + cfg.App.Port,
			Handler: r,
		},
	}
	return app
}

// Register adds a service provider. Call before Run. Register and Boot are invoked in order.
func (a *App) Register(p Provider) {
	a.providers = append(a.providers, p)
}

// Boot runs all registered providers: first every Register, then every Boot.
func (a *App) Boot() error {
	for _, p := range a.providers {
		if err := p.Register(a); err != nil {
			return fmt.Errorf("provider register: %w", err)
		}
	}
	for _, p := range a.providers {
		if err := p.Boot(a); err != nil {
			return fmt.Errorf("provider boot: %w", err)
		}
	}
	return nil
}

// Run boots providers (if any) and starts the HTTP server (like AdonisJS server.ts).
func (a *App) Run() error {
	if err := a.Boot(); err != nil {
		return err
	}
	port := a.Config.App.Port
	fmt.Printf("Nimbus server listening on http://localhost:%s\n", port)
	return a.Server.ListenAndServe()
}

// RunTLS starts the HTTP server with TLS.
func (a *App) RunTLS(certFile, keyFile string) error {
	port := a.Config.App.Port
	fmt.Printf("Nimbus server (TLS) listening on https://localhost:%s\n", port)
	return a.Server.ListenAndServeTLS(certFile, keyFile)
}
