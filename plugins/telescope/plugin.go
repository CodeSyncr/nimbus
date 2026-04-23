/*
|--------------------------------------------------------------------------
| Telescope Plugin for Nimbus
|--------------------------------------------------------------------------
|
| Telescope provides insight into your local Nimbus development environment:
| requests, exceptions, log entries, database queries, and more.
|
| Inspired by Laravel Telescope: https://laravel.com/docs/telescope
|
| Usage:
|
|   app.Use(telescope.New())
|
|   // Access the dashboard at /telescope
|
*/

package telescope

import (
	"os"
	"strings"
	"sync"
	"time"

	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/database"
	"github.com/CodeSyncr/nimbus/events"
	"github.com/CodeSyncr/nimbus/logger"
	"github.com/CodeSyncr/nimbus/schedule"
	"github.com/CodeSyncr/nimbus/view"
)

var (
	_ nimbus.Plugin        = (*Plugin)(nil)
	_ nimbus.HasMiddleware = (*Plugin)(nil)
	_ nimbus.HasRoutes     = (*Plugin)(nil)
	_ nimbus.HasConfig     = (*Plugin)(nil)
	_ nimbus.HasViews      = (*Plugin)(nil)
)

// Plugin integrates Telescope debugging into Nimbus.
type Plugin struct {
	nimbus.BasePlugin
	store    *Store
	basePath string // e.g. "/telescope"; no trailing slash
	requestWatcherOnce sync.Once
}

// New creates a new Telescope plugin instance.
func New() *Plugin {
	return &Plugin{
		BasePlugin: nimbus.BasePlugin{
			PluginName:    "telescope",
			PluginVersion: "1.0.0",
		},
		store: NewStore(100),
	}
}

var registerIntegrationsOnce sync.Once

// Register binds the Telescope store to the container and registers plugin views.
func (p *Plugin) Register(app *nimbus.App) error {
	p.initBasePath()
	view.RegisterPluginViews("telescope", p.ViewsFS())
	setGlobalStore(p.store)
	app.Container.Singleton("telescope.store", func() *Store { return p.store })

	// Mail is often configured during provider / app boot; wrap after everything loads.
	app.OnBoot(func(_ *nimbus.App) {
		wrapMailDriverForTelescope()
	})

	registerIntegrationsOnce.Do(func() {
		registerErrorHook()
		RegisterQueueObserver()
		view.OnRendered = func(name string, dur time.Duration, data any) {
			RecordViewRender(name, dur, data)
		}
		events.AfterDispatch(func(name string, payload any) {
			RecordEventFromDispatch(name, payload)
		})
		schedule.OnTaskRun(func(name, expr, status string, d time.Duration, out string) {
			RecordScheduleRun(name, expr, status, d, out)
		})
	})
	return nil
}

// Boot performs post-registration setup (e.g. register query watcher with database).
func (p *Plugin) Boot(app *nimbus.App) error {
	p.configureWatchers()
	p.maybeEnablePersistence()
	p.RegisterQueryWatcher()
	_ = logger.TeeCore(NewTelescopeZapCore(zapInfoLevel()))
	p.registerIntegrations()

	// Auto-wire request recording so the Requests panel always populates.
	// In production, keep behavior consistent with route registration gate.
	if os.Getenv("APP_ENV") != "production" || os.Getenv("TELESCOPE_ENABLED") == "true" {
		p.requestWatcherOnce.Do(func() {
			if app != nil && app.Router != nil {
				app.Router.Use(p.RequestWatcher())
			}
		})
	}
	return nil
}

func (p *Plugin) configureWatchers() {
	raw := strings.TrimSpace(os.Getenv("TELESCOPE_WATCHERS"))
	if raw == "" {
		return // all enabled
	}
	parts := strings.Split(raw, ",")
	var types []EntryType
	for _, s := range parts {
		s = strings.TrimSpace(strings.ToLower(s))
		if s == "" {
			continue
		}
		switch s {
		case "requests", "request":
			types = append(types, EntryRequest)
		case "commands", "command":
			types = append(types, EntryCommand)
		case "schedule", "scheduler":
			types = append(types, EntrySchedule)
		case "jobs", "job":
			types = append(types, EntryJob)
		case "batches", "batch":
			types = append(types, EntryBatch)
		case "cache":
			types = append(types, EntryCache)
		case "dumps", "dump":
			types = append(types, EntryDump)
		case "events", "event":
			types = append(types, EntryEvent)
		case "exceptions", "exception":
			types = append(types, EntryException)
		case "gates", "gate":
			types = append(types, EntryGate)
		case "http_client", "http-client", "httpclient":
			types = append(types, EntryHTTPClient)
		case "logs", "log":
			types = append(types, EntryLog)
		case "mail":
			types = append(types, EntryMail)
		case "models", "model":
			types = append(types, EntryModel)
		case "notifications", "notification":
			types = append(types, EntryNotification)
		case "queries", "query":
			types = append(types, EntryQuery)
		case "redis":
			types = append(types, EntryRedis)
		case "views", "view":
			types = append(types, EntryView)
		}
	}
	if len(types) > 0 {
		p.store.EnableOnly(types...)
	}
}

func (p *Plugin) maybeEnablePersistence() {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("TELESCOPE_STORAGE")))
	if mode == "" {
		return
	}
	if mode != "db" && mode != "database" && mode != "gorm" {
		return
	}
	db := database.Get()
	if db == nil {
		return
	}
	b := newDBPersist(db)
	if err := b.migrate(); err != nil {
		return
	}
	p.store.SetPersistBackend(b)
	// Populate the in-memory ring from DB so restarts retain history.
	p.store.LoadLatestFromBackend(0)

	// Optional pruning: TELESCOPE_PRUNE_DAYS=7
	days := intEnv("TELESCOPE_PRUNE_DAYS", 0)
	if days > 0 {
		p.store.PruneBefore(time.Now().Add(-time.Duration(days) * 24 * time.Hour))
	}
}

func (p *Plugin) initBasePath() {
	path := strings.TrimSpace(os.Getenv("TELESCOPE_PATH"))
	if path == "" {
		path = "/telescope"
	}
	p.basePath = strings.TrimSuffix(path, "/")
}

// DefaultConfig returns the default configuration.
func (p *Plugin) DefaultConfig() map[string]any {
	return map[string]any{
		"enabled":     os.Getenv("APP_ENV") == "development" || os.Getenv("APP_ENV") == "",
		"path":        "/telescope",
		"max_entries": 100,
	}
}
