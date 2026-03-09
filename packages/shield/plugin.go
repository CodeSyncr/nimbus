package shield

import (
	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/router"
)

// Compile-time interface checks.
var (
	_ nimbus.Plugin        = (*Plugin)(nil)
	_ nimbus.HasMiddleware = (*Plugin)(nil)
	_ nimbus.HasConfig     = (*Plugin)(nil)
)

// Plugin exposes Shield as a Nimbus plugin so it can be registered
// with app.Use(shield.NewPlugin(cfg)).
//
// It provides two named middleware ("shield" and "csrf") that can be
// referenced in start/kernel.go or attached to specific route groups.
type Plugin struct {
	nimbus.BasePlugin
	cfg Config
}

// NewPlugin creates a Shield plugin with the given configuration.
// Pass shield.DefaultConfig() for a production-ready baseline.
func NewPlugin(cfg Config) *Plugin {
	return &Plugin{
		BasePlugin: nimbus.BasePlugin{
			PluginName:    "shield",
			PluginVersion: "1.0.0",
		},
		cfg: cfg,
	}
}

// Middleware exposes the security-header guard and CSRF guard as named
// middleware that the application can reference.
func (p *Plugin) Middleware() map[string]router.Middleware {
	return map[string]router.Middleware{
		"shield": Guard(p.cfg),
		"csrf":   CSRFGuard(p.cfg.CSRF),
	}
}

// DefaultConfig returns the plugin's default settings so the
// application can inspect or override them at boot time.
func (p *Plugin) DefaultConfig() map[string]any {
	return map[string]any{
		"enabled":              true,
		"contentTypeNosniff":   p.cfg.ContentTypeNosniff,
		"xssProtection":        p.cfg.XSSProtection,
		"frameGuard":           p.cfg.FrameGuard,
		"referrerPolicy":       p.cfg.ReferrerPolicy,
		"csrf.enabled":         p.cfg.CSRF.Enabled,
		"csrf.cookieName":      p.cfg.CSRF.CookieName,
		"csrf.headerName":      p.cfg.CSRF.HeaderName,
		"csrf.fieldName":       p.cfg.CSRF.FieldName,
		"hsts.enabled":         p.cfg.HSTS.Enabled,
		"hsts.maxAge":          int(p.cfg.HSTS.MaxAge.Seconds()),
		"hsts.includeSubdomains": p.cfg.HSTS.IncludeSubdomains,
	}
}
