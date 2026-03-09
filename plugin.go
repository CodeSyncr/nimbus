package nimbus

import (
	"io/fs"

	"github.com/CodeSyncr/nimbus/database"
	"github.com/CodeSyncr/nimbus/router"
)

// ---------------------------------------------------------------------------
// Plugin interface
// ---------------------------------------------------------------------------

// Plugin is the base contract every Nimbus plugin must satisfy.
// A plugin has a name, a version, and two lifecycle hooks that mirror
// the Provider pattern (Register → Boot).
//
// Plugins may optionally implement one or more capability interfaces
// (HasRoutes, HasMiddleware, HasConfig, HasMigrations, HasViews,
// HasShutdown) to hook into the framework at well-defined points.
type Plugin interface {
	// Name returns the plugin's unique identifier (e.g. "auth", "redis").
	Name() string

	// Version returns the semantic version string (e.g. "1.0.0").
	Version() string

	// Register is called first for all plugins. Bind services into
	// app.Container here. Do not resolve other services yet.
	Register(app *App) error

	// Boot is called after every plugin (and provider) has registered.
	// Safe to resolve container bindings and perform initialisation
	// that depends on other services.
	Boot(app *App) error
}

// ---------------------------------------------------------------------------
// Capability interfaces (optional — implement only what you need)
// ---------------------------------------------------------------------------

// HasRoutes allows a plugin to mount its own HTTP routes onto the
// application router during boot.
type HasRoutes interface {
	RegisterRoutes(r *router.Router)
}

// HasMiddleware allows a plugin to expose named middleware that can be
// assigned to routes or groups in start/kernel.go or start/routes.go.
type HasMiddleware interface {
	Middleware() map[string]router.Middleware
}

// HasConfig allows a plugin to declare default configuration values.
// The map is keyed by config name and merged into the application's
// configuration at boot time.
type HasConfig interface {
	DefaultConfig() map[string]any
}

// HasMigrations allows a plugin to provide database migrations that
// are collected and can be run alongside application migrations.
type HasMigrations interface {
	Migrations() []database.Migration
}

// HasViews allows a plugin to supply an embedded filesystem of .nimbus
// templates that are layered into the view engine.
type HasViews interface {
	ViewsFS() fs.FS
}

// HasShutdown allows a plugin to run cleanup logic when the
// application is shutting down (e.g. closing connections, flushing
// buffers).
type HasShutdown interface {
	Shutdown() error
}

// ---------------------------------------------------------------------------
// BasePlugin — embed to get default implementations
// ---------------------------------------------------------------------------

// BasePlugin provides a no-op implementation of the Plugin interface.
// Embed it in your plugin struct so you only need to override the
// methods you care about.
//
//	type MyPlugin struct {
//	    nimbus.BasePlugin
//	}
//
//	func init() {
//	    _ = nimbus.Plugin(&MyPlugin{}) // compile-time check
//	}
type BasePlugin struct {
	PluginName    string
	PluginVersion string
}

func (b *BasePlugin) Name() string              { return b.PluginName }
func (b *BasePlugin) Version() string           { return b.PluginVersion }
func (b *BasePlugin) Register(_ *App) error     { return nil }
func (b *BasePlugin) Boot(_ *App) error         { return nil }
