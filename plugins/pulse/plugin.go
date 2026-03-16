package pulse

import (
	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/router"
)

// Ensure Pulse satisfies nimbus.Plugin and capability interfaces.
var (
	_ nimbus.Plugin        = (*PulsePlugin)(nil)
	_ nimbus.HasRoutes     = (*PulsePlugin)(nil)
	_ nimbus.HasMiddleware = (*PulsePlugin)(nil)
	_ nimbus.HasConfig     = (*PulsePlugin)(nil)
)

// PulsePlugin wraps Pulse as a Nimbus plugin, making it installable
// via `nimbus plugin install pulse` and selectable during `nimbus new`.
type PulsePlugin struct {
	nimbus.BasePlugin
	Pulse *Pulse
}

// NewPlugin creates a new Pulse plugin with default config.
func NewPlugin(cfg ...Config) *PulsePlugin {
	var c Config
	if len(cfg) > 0 {
		c = cfg[0]
	}
	return &PulsePlugin{
		BasePlugin: nimbus.BasePlugin{
			PluginName:    "pulse",
			PluginVersion: "1.0.0",
		},
		Pulse: New(c),
	}
}

// Register binds the Pulse instance into the container.
func (p *PulsePlugin) Register(app *nimbus.App) error {
	app.Container.Singleton("pulse", func() *Pulse { return p.Pulse })
	return nil
}

// Boot is a no-op for Pulse.
func (p *PulsePlugin) Boot(app *nimbus.App) error {
	return nil
}

// RegisterRoutes mounts the Pulse dashboard at /pulse.
func (p *PulsePlugin) RegisterRoutes(r *router.Router) {
	r.Get("/pulse", p.Pulse.DashboardHandler())
}

// Middleware returns named middleware for Pulse request recording.
func (p *PulsePlugin) Middleware() map[string]router.Middleware {
	return map[string]router.Middleware{
		"pulse": p.Pulse.Middleware(),
	}
}

// DefaultConfig returns default Pulse configuration.
func (p *PulsePlugin) DefaultConfig() map[string]any {
	return map[string]any{
		"max_entries":            10000,
		"slow_query_threshold":   "100ms",
		"slow_request_threshold": "500ms",
	}
}
