// Package admin is a resource-based CRUD admin panel for Nimbus, modeled on
// Laravel Nova / Filament. You describe your models as Resources (fields +
// display rules) and the panel auto-generates list, create, edit, and delete
// screens with a modern Tailwind UI.
//
//	panel := admin.New(db, admin.Config{
//	    BrandName:  "Acme Admin",
//	    Middleware: []router.Middleware{auth.RequireAuth(guard)}, // gate the panel
//	})
//	panel.AddResource(admin.Resource{
//	    Model:  &models.Post{},
//	    Fields: []admin.Field{
//	        admin.Text("Title").AsSortable(),
//	        admin.Textarea("Body"),
//	        admin.Boolean("Published"),
//	    },
//	})
//	app.Use(panel)
//
// With no Fields, the panel infers them from the struct. The panel operates on
// your existing tables — it declares no migrations of its own.
//
// Layout:
//
//	admin.go     – plugin + panel + Config    resource.go – Resource, Field, reflection
//	routes.go    – CRUD handlers              views/      – layout, list, form, dashboard
package admin

import (
	"embed"
	"io/fs"

	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/lucid"
	"github.com/CodeSyncr/nimbus/router"
	"github.com/CodeSyncr/nimbus/view"
)

//go:embed views/*.nimbus
var viewsFS embed.FS

var (
	_ nimbus.Plugin    = (*Plugin)(nil)
	_ nimbus.HasRoutes = (*Plugin)(nil)
	_ nimbus.HasViews  = (*Plugin)(nil)
	_ nimbus.HasConfig = (*Plugin)(nil)
)

// Config tunes the admin panel.
type Config struct {
	// RoutePrefix is where the panel mounts. Default "/admin".
	RoutePrefix string
	// BrandName shown in the sidebar header. Default "Nimbus Admin".
	BrandName string
	// Middleware gates every panel route (e.g. an auth guard). Strongly
	// recommended — without it the panel is public.
	Middleware []router.Middleware
}

// Panel holds the registered resources and the database handle.
type Panel struct {
	db        *lucid.DB
	cfg       Config
	resources map[string]*Resource
	order     []string
}

// Plugin wires the admin panel into Nimbus.
type Plugin struct {
	nimbus.BasePlugin
	panel *Panel
}

// New builds an admin panel over the given database.
func New(db *lucid.DB, cfg Config) *Plugin {
	if cfg.RoutePrefix == "" {
		cfg.RoutePrefix = "/admin"
	}
	if cfg.BrandName == "" {
		cfg.BrandName = "Nimbus Admin"
	}
	return &Plugin{
		BasePlugin: nimbus.BasePlugin{PluginName: "admin", PluginVersion: "1.0.0"},
		panel: &Panel{
			db:        db,
			cfg:       cfg,
			resources: map[string]*Resource{},
		},
	}
}

// AddResource registers a resource with the panel. Chainable.
func (p *Plugin) AddResource(r Resource) *Plugin {
	r.normalize()
	if _, exists := p.panel.resources[r.Slug]; !exists {
		p.panel.order = append(p.panel.order, r.Slug)
	}
	p.panel.resources[r.Slug] = &r
	return p
}

// Panel exposes the underlying panel (for advanced use / testing).
func (p *Plugin) Panel() *Panel { return p.panel }

func (p *Plugin) Register(app *nimbus.App) error {
	view.RegisterPluginViews("admin", p.ViewsFS())
	app.Container.Singleton("admin.panel", func() *Panel { return p.panel })
	return nil
}

func (p *Plugin) Boot(app *nimbus.App) error { return nil }

// ViewsFS returns the embedded admin templates for the view engine.
func (p *Plugin) ViewsFS() fs.FS {
	f, _ := fs.Sub(viewsFS, "views")
	return f
}

func (p *Plugin) DefaultConfig() map[string]any {
	return map[string]any{
		"route_prefix": p.panel.cfg.RoutePrefix,
		"brand":        p.panel.cfg.BrandName,
		"resources":    p.panel.order,
	}
}

// nav returns the sidebar entries.
func (pn *Panel) nav() []map[string]string {
	out := make([]map[string]string, 0, len(pn.order))
	for _, slug := range pn.order {
		r := pn.resources[slug]
		out = append(out, map[string]string{"label": r.Label, "slug": r.Slug})
	}
	return out
}
