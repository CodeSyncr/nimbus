/*
|--------------------------------------------------------------------------
| Inertia.js Plugin for Nimbus
|--------------------------------------------------------------------------
|
| This plugin integrates Inertia.js with Nimbus, enabling you to build
| single-page apps using Vue, React, or Svelte without building an API.
|
| Inertia works by intercepting requests and returning JSON page data
| instead of full HTML when the X-Inertia header is present.
|
| Usage:
|
|   // bin/server.go
|   app.Use(inertia.New(inertia.Config{
|       URL:          "http://localhost:3000",
|       RootTemplate: "resources/views/app.html",
|       Version:      "1",
|   }))
|
|   // In a handler
|   return inertia.Render(c, "Home/Index", map[string]any{"users": users})
|
| Requires: github.com/petaki/inertia-go
|
|   go get github.com/petaki/inertia-go
|
*/

package inertia

import (
	"github.com/CodeSyncr/nimbus"
)

var (
	_ nimbus.Plugin = (*Plugin)(nil)
)

// Plugin integrates Inertia.js with Nimbus.
type Plugin struct {
	nimbus.BasePlugin
	config  Config
	manager Manager
}

// Manager is the Inertia adapter interface. Implementations wrap
// github.com/petaki/inertia-go or similar adapters.
type Manager interface {
	// Middleware returns http middleware that wraps the given handler.
	Middleware(next interface{}) interface{}
	// Render writes an Inertia page response to the response writer.
	Render(w interface{}, r interface{}, component string, props map[string]any) error
}

// New creates a new Inertia plugin with the given config.
func New(cfg Config) *Plugin {
	return &Plugin{
		BasePlugin: nimbus.BasePlugin{
			PluginName:    "inertia",
			PluginVersion: "1.0.0",
		},
		config: cfg,
	}
}

// Register binds the Inertia manager to the container (no-op for now).
func (p *Plugin) Register(app *nimbus.App) error {
	return nil
}

// Boot initializes the Inertia manager and wraps the server handler.
func (p *Plugin) Boot(app *nimbus.App) error {
	mgr, err := p.createManager()
	if err != nil {
		return err
	}
	p.manager = mgr
	setManager(mgr)
	app.Server.Handler = p.wrapHandler(app.Server.Handler)
	return nil
}
