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

	"github.com/CodeSyncr/nimbus"
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
	store *Store
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

// Register binds the Telescope store to the container and registers plugin views.
func (p *Plugin) Register(app *nimbus.App) error {
	view.RegisterPluginViews("telescope", p.ViewsFS())
	setGlobalStore(p.store)
	app.Container.Singleton("telescope.store", func() *Store { return p.store })
	return nil
}

// Boot performs post-registration setup (e.g. register query watcher with database).
func (p *Plugin) Boot(app *nimbus.App) error {
	p.RegisterQueryWatcher()
	return nil
}

// DefaultConfig returns the default configuration.
func (p *Plugin) DefaultConfig() map[string]any {
	return map[string]any{
		"enabled":     os.Getenv("APP_ENV") == "development" || os.Getenv("APP_ENV") == "",
		"path":        "/telescope",
		"max_entries": 100,
	}
}
