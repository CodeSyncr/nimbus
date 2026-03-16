/*
|--------------------------------------------------------------------------
| Feature Flags — Nimbus Plugin
|--------------------------------------------------------------------------
|
| Integrates feature flags with the Nimbus plugin system.
| Provides API routes and middleware for flag-based access control.
|
*/

package flags

import (
	"encoding/json"
	"net/http"

	"github.com/CodeSyncr/nimbus"
	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

var (
	_ nimbus.Plugin        = (*FlagPlugin)(nil)
	_ nimbus.HasRoutes     = (*FlagPlugin)(nil)
	_ nimbus.HasConfig     = (*FlagPlugin)(nil)
	_ nimbus.HasMiddleware = (*FlagPlugin)(nil)
)

// FlagPlugin integrates feature flags with Nimbus.
type FlagPlugin struct {
	nimbus.BasePlugin
	Manager *Manager
}

// NewPlugin creates a new feature flag plugin.
func NewPlugin(store Store) *FlagPlugin {
	return &FlagPlugin{
		BasePlugin: nimbus.BasePlugin{
			PluginName:    "flags",
			PluginVersion: "1.0.0",
		},
		Manager: New(store),
	}
}

func (p *FlagPlugin) Register(app *nimbus.App) error {
	app.Container.Singleton("flags.manager", func() *Manager { return p.Manager })
	return nil
}

func (p *FlagPlugin) Boot(app *nimbus.App) error {
	p.Manager.LoadFromEnv()
	return nil
}

func (p *FlagPlugin) DefaultConfig() map[string]any {
	return map[string]any{
		"store":    "memory",
		"env_load": true,
	}
}

// Middleware returns named middleware for flag-based access control.
func (p *FlagPlugin) Middleware() map[string]router.Middleware {
	return map[string]router.Middleware{
		"feature": RequireFlag(p.Manager),
	}
}

// RequireFlag returns middleware that returns 404 if the flag is not active.
// Usage: router.Use(flags.RequireFlag(manager)("flag-name"))
// Or via named middleware: "feature:flag-name"
func RequireFlag(m *Manager) router.Middleware {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *nhttp.Context) error {
			// Extract flag name from middleware parameter
			flagName := c.Request.URL.Query().Get("_feature")
			if flagName == "" {
				return next(c)
			}
			if !m.Active(flagName, nil) {
				return c.JSON(http.StatusNotFound, map[string]string{"error": "Not found"})
			}
			return next(c)
		}
	}
}

// FlagGate returns middleware that gates a route behind a feature flag.
func FlagGate(m *Manager, flagName string) router.Middleware {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *nhttp.Context) error {
			if !m.Active(flagName, nil) {
				return c.JSON(http.StatusNotFound, map[string]string{"error": "Not found"})
			}
			return next(c)
		}
	}
}

// RegisterRoutes mounts the flags API.
func (p *FlagPlugin) RegisterRoutes(r *router.Router) {
	grp := r.Group("/_flags")
	grp.Get("/", p.listFlags)
	grp.Post("/:name/enable", p.enableFlag)
	grp.Post("/:name/disable", p.disableFlag)
	grp.Post("/:name/rollout", p.setRollout)
	grp.Get("/:name/check", p.checkFlag)
}

func (p *FlagPlugin) listFlags(c *nhttp.Context) error {
	return c.JSON(http.StatusOK, p.Manager.All())
}

func (p *FlagPlugin) enableFlag(c *nhttp.Context) error {
	name := c.Param("name")
	if err := p.Manager.Enable(name); err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "enabled", "flag": name})
}

func (p *FlagPlugin) disableFlag(c *nhttp.Context) error {
	name := c.Param("name")
	if err := p.Manager.Disable(name); err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "disabled", "flag": name})
}

func (p *FlagPlugin) setRollout(c *nhttp.Context) error {
	name := c.Param("name")
	var body struct {
		Percent int `json:"percent"`
	}
	if err := json.NewDecoder(c.Request.Body).Decode(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	if err := p.Manager.SetRollout(name, body.Percent); err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]any{
		"flag":    name,
		"percent": body.Percent,
	})
}

func (p *FlagPlugin) checkFlag(c *nhttp.Context) error {
	name := c.Param("name")
	userID := c.Request.URL.Query().Get("user_id")
	var user *UserContext
	if userID != "" {
		groups := c.Request.URL.Query().Get("groups")
		var groupList []string
		if groups != "" {
			for _, g := range json.RawMessage(groups) {
				_ = g
			}
			_ = json.Unmarshal([]byte(groups), &groupList)
		}
		user = &UserContext{ID: userID, Groups: groupList}
	}
	active := p.Manager.Active(name, user)
	result := map[string]any{"flag": name, "active": active}
	if userID != "" {
		result["user_id"] = userID
		variant := p.Manager.Variant(name, userID)
		if variant != "" {
			result["variant"] = variant
		}
	}
	return c.JSON(http.StatusOK, result)
}
