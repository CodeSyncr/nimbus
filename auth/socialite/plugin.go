package socialite

import (
	"os"

	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/router"
)

// Ensure SocialitePlugin satisfies nimbus.Plugin and capability interfaces.
var (
	_ nimbus.Plugin    = (*SocialitePlugin)(nil)
	_ nimbus.HasRoutes = (*SocialitePlugin)(nil)
	_ nimbus.HasConfig = (*SocialitePlugin)(nil)
)

// SocialitePlugin wraps Socialite as a Nimbus plugin, making it
// installable via `nimbus plugin install socialite` and selectable
// during `nimbus new`.
type SocialitePlugin struct {
	nimbus.BasePlugin
	Manager  *Socialite
	Callback CallbackFunc
}

// NewPlugin creates a new Socialite plugin. Pass a callback to handle
// the authenticated social user.
func NewPlugin(cfg Config, callback CallbackFunc) *SocialitePlugin {
	return &SocialitePlugin{
		BasePlugin: nimbus.BasePlugin{
			PluginName:    "socialite",
			PluginVersion: "1.0.0",
		},
		Manager:  New(cfg),
		Callback: callback,
	}
}

// Register binds the Socialite manager into the container.
func (p *SocialitePlugin) Register(app *nimbus.App) error {
	app.Container.Singleton("socialite", func() *Socialite { return p.Manager })
	return nil
}

// Boot is a no-op for Socialite.
func (p *SocialitePlugin) Boot(app *nimbus.App) error {
	return nil
}

// RegisterRoutes mounts the OAuth redirect and callback routes.
func (p *SocialitePlugin) RegisterRoutes(r *router.Router) {
	r.Get("/auth/{provider}", p.Manager.RedirectHandler())
	r.Get("/auth/{provider}/callback", p.Manager.CallbackHandler(p.Callback))
}

// DefaultConfig returns default Socialite configuration.
func (p *SocialitePlugin) DefaultConfig() map[string]any {
	return map[string]any{
		"github_client_id":     os.Getenv("GITHUB_CLIENT_ID"),
		"github_client_secret": os.Getenv("GITHUB_CLIENT_SECRET"),
		"google_client_id":     os.Getenv("GOOGLE_CLIENT_ID"),
		"google_client_secret": os.Getenv("GOOGLE_CLIENT_SECRET"),
	}
}
