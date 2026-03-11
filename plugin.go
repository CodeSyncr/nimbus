package nimbus

import (
	"io/fs"

	"github.com/CodeSyncr/nimbus/cli"
	"github.com/CodeSyncr/nimbus/container"
	"github.com/CodeSyncr/nimbus/database"
	"github.com/CodeSyncr/nimbus/events"
	"github.com/CodeSyncr/nimbus/health"
	"github.com/CodeSyncr/nimbus/router"
	"github.com/CodeSyncr/nimbus/schedule"
)

// ---------------------------------------------------------------------------
// Plugin interface
// ---------------------------------------------------------------------------

// Plugin is the base contract every Nimbus plugin must satisfy.
// A plugin has a name, a version, and two lifecycle hooks that mirror
// the Provider pattern (Register → Boot).
//
// Plugins may optionally implement one or more capability interfaces
// to hook into the framework at well-defined points. This design allows
// integrating anything — Stripe, Sentry, MongoDB, WhatsApp, etc.
type Plugin interface {
	// Name returns the plugin's unique identifier (e.g. "stripe", "sentry").
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

// HasBindings allows a plugin to declare container bindings that are
// automatically registered during the Register phase. Use this to bind
// SDK clients, API wrappers, or any service the app can resolve.
//
//	func (p *StripePlugin) Bindings(c *container.Container) {
//	    c.Singleton("stripe", func() (*stripe.Client, error) {
//	        return stripe.New(os.Getenv("STRIPE_KEY"))
//	    })
//	}
type HasBindings interface {
	Bindings(c *container.Container)
}

// HasCommands allows a plugin to register CLI commands (Artisan-style).
// Commands are added to the Nimbus CLI and available via `nimbus <cmd>`.
//
//	func (p *SentryPlugin) Commands() []cli.Command {
//	    return []cli.Command{&SentryTestCommand{}}
//	}
type HasCommands interface {
	Commands() []cli.Command
}

// HasSchedule allows a plugin to register periodic background tasks.
// The scheduler runs automatically when the app starts.
//
//	func (p *TelemetryPlugin) Schedule(s *schedule.Scheduler) {
//	    s.Every(5*time.Minute, "telemetry-flush", p.flush)
//	}
type HasSchedule interface {
	Schedule(s *schedule.Scheduler)
}

// HasEvents allows a plugin to declare event listeners that are
// registered on the application's event dispatcher during boot.
//
//	func (p *AuditPlugin) Listeners() map[string][]events.Listener {
//	    return map[string][]events.Listener{
//	        "user.created": {p.onUserCreated},
//	        "order.placed": {p.onOrderPlaced},
//	    }
//	}
type HasEvents interface {
	Listeners() map[string][]events.Listener
}

// HasHealthChecks allows a plugin to report health status. Registered
// checks are added to the application's health checker.
//
//	func (p *RedisPlugin) HealthChecks() map[string]health.Check {
//	    return map[string]health.Check{
//	        "redis": func(ctx context.Context) error {
//	            return p.client.Ping(ctx).Err()
//	        },
//	    }
//	}
type HasHealthChecks interface {
	HealthChecks() map[string]health.Check
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
type BasePlugin struct {
	PluginName    string
	PluginVersion string
}

func (b *BasePlugin) Name() string          { return b.PluginName }
func (b *BasePlugin) Version() string       { return b.PluginVersion }
func (b *BasePlugin) Register(_ *App) error { return nil }
func (b *BasePlugin) Boot(_ *App) error     { return nil }
