// Package passport is an OAuth2 authorization server for Nimbus, modeled on
// Laravel Passport. It lets your app issue OAuth2 tokens to third-party (and
// first-party) clients, supporting the authorization_code (with PKCE),
// client_credentials, and refresh_token grants, plus token introspection and
// revocation.
//
//	app.Use(passport.NewPlugin(db, passport.Config{}))
//
// Then protect resource routes with the access-token middleware:
//
//	srv := app.Container.MustMake("passport").(*passport.Server)
//	api := app.Router.Group("/api", passport.RequireAccessToken(srv))
//	api.Use(passport.RequireScope("read:profile"))
//
// Layout:
//
//	passport.go   – plugin + wiring      config.go   – Config
//	server.go     – OAuth2 core          routes.go   – authorize/token/introspect/revoke
//	middleware.go – resource server      models/     – clients, codes, tokens
//	views/        – consent screen
package passport

import (
	"embed"
	"io/fs"

	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/database"
	"github.com/CodeSyncr/nimbus/lucid"
	"github.com/CodeSyncr/nimbus/plugins/passport/models"
	"github.com/CodeSyncr/nimbus/view"
)

//go:embed views/*.nimbus
var viewsFS embed.FS

var (
	_ nimbus.Plugin        = (*Plugin)(nil)
	_ nimbus.HasRoutes     = (*Plugin)(nil)
	_ nimbus.HasMigrations = (*Plugin)(nil)
	_ nimbus.HasViews      = (*Plugin)(nil)
	_ nimbus.HasConfig     = (*Plugin)(nil)
)

// Plugin wires the OAuth2 server into Nimbus.
type Plugin struct {
	nimbus.BasePlugin
	server *Server
}

// NewPlugin builds the Passport plugin over the given database.
func NewPlugin(db *lucid.DB, cfg Config) *Plugin {
	return &Plugin{
		BasePlugin: nimbus.BasePlugin{PluginName: "passport", PluginVersion: "1.0.0"},
		server:     NewServer(db, cfg),
	}
}

// Server exposes the underlying OAuth2 server (client creation, validation).
func (p *Plugin) Server() *Server { return p.server }

func (p *Plugin) Register(app *nimbus.App) error {
	view.RegisterPluginViews("passport", p.ViewsFS())
	app.Container.Singleton("passport", func() *Server { return p.server })
	return nil
}

func (p *Plugin) Boot(app *nimbus.App) error { return nil }

// Migrations creates the OAuth tables (clients, codes, access/refresh tokens).
func (p *Plugin) Migrations() []database.Migration { return models.Migrations() }

// ViewsFS returns the embedded consent screen for the view engine.
func (p *Plugin) ViewsFS() fs.FS {
	f, _ := fs.Sub(viewsFS, "views")
	return f
}

func (p *Plugin) DefaultConfig() map[string]any {
	return map[string]any{
		"route_prefix":      p.server.cfg.RoutePrefix,
		"access_token_ttl":  p.server.cfg.AccessTokenTTL.String(),
		"refresh_token_ttl": p.server.cfg.RefreshTokenTTL.String(),
	}
}
