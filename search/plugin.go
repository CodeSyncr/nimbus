package search

import (
	"os"

	"github.com/CodeSyncr/nimbus"
)

// Ensure Plugin satisfies nimbus.Plugin and capability interfaces.
var (
	_ nimbus.Plugin    = (*Plugin)(nil)
	_ nimbus.HasConfig = (*Plugin)(nil)
)

// Plugin wraps the search engine as a Nimbus plugin, making it installable
// via `nimbus plugin install scout` and selectable during `nimbus new`.
type Plugin struct {
	nimbus.BasePlugin
	engine Engine
}

// New creates a new Scout search plugin. Pass nil to auto-detect engine
// from the SEARCH_DRIVER env var (defaults to "postgres").
func NewPlugin(engine Engine) *Plugin {
	return &Plugin{
		BasePlugin: nimbus.BasePlugin{
			PluginName:    "scout",
			PluginVersion: "1.0.0",
		},
		engine: engine,
	}
}

// Register binds the search engine into the container and sets it as default.
func (p *Plugin) Register(app *nimbus.App) error {
	if p.engine != nil {
		Register("default", p.engine)
	}
	app.Container.Singleton("search.engine", func() Engine {
		return Default()
	})
	return nil
}

// Boot is a no-op for Scout.
func (p *Plugin) Boot(app *nimbus.App) error {
	return nil
}

// DefaultConfig returns default scout configuration.
func (p *Plugin) DefaultConfig() map[string]any {
	driver := os.Getenv("SEARCH_DRIVER")
	if driver == "" {
		driver = "postgres"
	}
	return map[string]any{
		"driver": driver,
	}
}

// Engine returns the underlying search engine.
func (p *Plugin) Engine() Engine {
	return p.engine
}
