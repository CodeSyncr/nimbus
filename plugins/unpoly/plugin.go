/*
|--------------------------------------------------------------------------
| Unpoly Plugin for Nimbus
|--------------------------------------------------------------------------
|
| This plugin integrates the Unpoly server protocol into Nimbus.
| Unpoly (https://unpoly.com) is a framework for server-rendered
| HTML applications that enables fast page updates, modals/layers,
| form validation, and smooth navigation without full page reloads.
|
| The plugin provides:
|   - Middleware that sets required response headers (X-Up-Location,
|     X-Up-Method, Vary) for every Unpoly request.
|   - Context helper functions to read Unpoly request headers and
|     set response headers for events, layers, cache control, etc.
|
| Usage:
|
|   // bin/server.go
|   import "github.com/CodeSyncr/nimbus/plugins/unpoly"
|
|   app.Use(unpoly.New())
|
*/

package unpoly

import "github.com/CodeSyncr/nimbus"

var (
	_ nimbus.Plugin        = (*Plugin)(nil)
	_ nimbus.HasMiddleware = (*Plugin)(nil)
	_ nimbus.HasConfig     = (*Plugin)(nil)
)

// Plugin integrates the Unpoly server protocol with Nimbus.
type Plugin struct {
	nimbus.BasePlugin
}

// New creates a new Unpoly plugin instance.
func New() *Plugin {
	return &Plugin{
		BasePlugin: nimbus.BasePlugin{
			PluginName:    "unpoly",
			PluginVersion: "1.0.0",
		},
	}
}
