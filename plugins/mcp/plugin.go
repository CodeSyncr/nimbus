/*
|--------------------------------------------------------------------------
| MCP Plugin for Nimbus
|--------------------------------------------------------------------------
|
| This plugin provides Model Context Protocol (MCP) support for Nimbus,
| inspired by Laravel's MCP package. It enables AI clients to interact
| with your application through tools, resources, and prompts.
|
| Features:
|   - Web servers: Expose MCP over HTTP (Streamable HTTP transport)
|   - Tools: Actions AI clients can invoke
|   - Resources: Data AI clients can read
|   - Prompts: Reusable prompt templates
|
| Usage:
|
|   // bin/server.go
|   app.Use(mcp.New())
|
|   // start/routes.go or similar
|   mcp.Web("/mcp/weather", myWeatherServer)
|
| Configuration (config/mcp.go or .env):
|   MCP_PREFIX=/mcp
|
*/

package mcp

import (
	"strings"

	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/router"
)

var (
	_ nimbus.Plugin    = (*Plugin)(nil)
	_ nimbus.HasRoutes = (*Plugin)(nil)
)

// Plugin integrates MCP with Nimbus.
type Plugin struct {
	nimbus.BasePlugin
	servers []*webServerRegistration
}

// webServerRegistration holds path and server for mounting.
type webServerRegistration struct {
	path   string
	server *Server
}

// New creates a new MCP plugin instance.
func New() *Plugin {
	return &Plugin{
		BasePlugin: nimbus.BasePlugin{
			PluginName:    "mcp",
			PluginVersion: "1.0.0",
		},
		servers: nil,
	}
}

// Register binds MCP services to the container.
func (p *Plugin) Register(app *nimbus.App) error {
	return nil
}

// Boot performs post-registration setup.
func (p *Plugin) Boot(app *nimbus.App) error {
	return nil
}

// Web registers an MCP server to be served at the given path over HTTP.
// Call before app.Boot(). The path should not end with a slash.
//
// Example:
//
//	mcpPlugin := mcp.New()
//	mcpPlugin.Web("/mcp/weather", weatherServer)
//	app.Use(mcpPlugin)
func (p *Plugin) Web(path string, srv *Server) {
	path = "/" + strings.Trim(path, "/")
	p.servers = append(p.servers, &webServerRegistration{path: path, server: srv})
}

// RegisterRoutes mounts all registered MCP servers onto the router.
func (p *Plugin) RegisterRoutes(r *router.Router) {
	for _, reg := range p.servers {
		r.Mount(reg.path, reg.server.Handler())
	}
}
