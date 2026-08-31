/*
|--------------------------------------------------------------------------
| Captcha Plugin for Nimbus (Nimbus Cloud CapSolver Alternative)
|--------------------------------------------------------------------------
|
| This plugin provides dual capabilities:
|   1. Automated Captcha Solver API client for Nimbus Cloud (Turnstile,
|      reCAPTCHA v2/v3/Enterprise, hCaptcha, GeeTest, AWS WAF, OCR).
|   2. Server-side request verifier and middleware to defend Nimbus routes
|      from automated bots.
|
| Usage:
|
|   // bin/server.go
|   app.Use(captcha.New())
|
|   // Automated Solving (in scrapers / background jobs)
|   solution, err := captcha.Solve(ctx, captcha.TaskPayload{
|       Type:       captcha.TaskTypeTurnstileProxyless,
|       WebsiteURL: "https://example.com/login",
|       WebsiteKey: "0x4AAAAAA...",
|   })
|
|   // Protect HTTP routes with middleware
|   app.Router.Post("/register", captcha.Protect(), RegisterHandler)
|
| Configuration (.env):
|   NIMBUS_CLOUD_API_KEY=nc_live_...
|   NIMBUS_CAPTCHA_MOCK=false
|   TURNSTILE_SECRET_KEY=0x4AAAAAA...
|
*/

package captcha

import (
	"os"
	"strconv"
	"time"

	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/container"
	"github.com/CodeSyncr/nimbus/router"
)

var (
	_ nimbus.Plugin        = (*Plugin)(nil)
	_ nimbus.HasConfig     = (*Plugin)(nil)
	_ nimbus.HasBindings   = (*Plugin)(nil)
	_ nimbus.HasMiddleware = (*Plugin)(nil)
)

// Plugin integrates Nimbus Cloud Captcha solver & verifier into Nimbus apps.
type Plugin struct {
	nimbus.BasePlugin
	customConfig *Config
	manager      *Manager
}

// New creates a new Captcha plugin instance. Options may optionally pass custom Config.
func New(cfg ...*Config) *Plugin {
	var c *Config
	if len(cfg) > 0 && cfg[0] != nil {
		c = cfg[0]
	}

	return &Plugin{
		BasePlugin: nimbus.BasePlugin{
			PluginName:    "captcha",
			PluginVersion: "1.0.0",
		},
		customConfig: c,
	}
}

// Register initializes configuration, builds Manager, and registers IoC bindings.
func (p *Plugin) Register(app *nimbus.App) error {
	cfg := p.loadConfig(app)

	mgr, err := NewManager(cfg)
	if err != nil {
		return err
	}

	p.manager = mgr
	setManager(mgr)

	Bindings(app.Container, mgr)
	return nil
}

// Boot satisfies the nimbus.Plugin interface.
func (p *Plugin) Boot(app *nimbus.App) error {
	return nil
}

// Bindings binds Services into Nimbus Container.
func (p *Plugin) Bindings(c *container.Container) {
	if p.manager != nil {
		Bindings(c, p.manager)
	}
}

// Middleware exposes named middleware for application router.
func (p *Plugin) Middleware() map[string]router.Middleware {
	return map[string]router.Middleware{
		"captcha": protectMiddleware(),
	}
}

func protectMiddleware() router.Middleware {
	return Protect()
}

// DefaultConfig returns default configuration parameters.
func (p *Plugin) DefaultConfig() map[string]any {
	return map[string]any{
		"endpoint":         "https://api.nimbuscloud.io/v1/captcha",
		"default_provider": "turnstile",
		"mock":             false,
		"timeout":          60,
		"polling_interval": 1,
		"max_retries":      60,
	}
}

func (p *Plugin) loadConfig(app *nimbus.App) *Config {
	if p.customConfig != nil {
		return p.customConfig
	}

	cfg := DefaultConfig()

	if pluginCfg := app.PluginConfig("captcha"); pluginCfg != nil {
		if v, ok := pluginCfg["endpoint"].(string); ok && v != "" {
			cfg.Endpoint = v
		}
		if v, ok := pluginCfg["default_provider"].(string); ok && v != "" {
			cfg.DefaultProvider = v
		}
		if v, ok := pluginCfg["mock"].(bool); ok {
			cfg.MockMode = v
		}
		if v, ok := pluginCfg["timeout"].(int); ok && v > 0 {
			cfg.Timeout = time.Duration(v) * time.Second
		}
	}

	// Environment variable overrides
	if key := os.Getenv("NIMBUS_CLOUD_API_KEY"); key != "" {
		cfg.APIKey = key
	} else if key := os.Getenv("NIMBUS_CAPTCHA_KEY"); key != "" {
		cfg.APIKey = key
	} else if key := os.Getenv("CAPSOLVER_API_KEY"); key != "" {
		cfg.APIKey = key
	}

	if endpoint := os.Getenv("NIMBUS_CLOUD_ENDPOINT"); endpoint != "" {
		cfg.Endpoint = endpoint
	}

	if mockStr := os.Getenv("NIMBUS_CAPTCHA_MOCK"); mockStr != "" {
		if mockVal, err := strconv.ParseBool(mockStr); err == nil {
			cfg.MockMode = mockVal
		}
	}

	if cfg.ProviderSecretKeys == nil {
		cfg.ProviderSecretKeys = make(map[string]string)
	}

	if secret := os.Getenv("TURNSTILE_SECRET_KEY"); secret != "" {
		cfg.ProviderSecretKeys["turnstile"] = secret
	}
	if secret := os.Getenv("RECAPTCHA_SECRET_KEY"); secret != "" {
		cfg.ProviderSecretKeys["recaptcha"] = secret
	}
	if secret := os.Getenv("HCAPTCHA_SECRET_KEY"); secret != "" {
		cfg.ProviderSecretKeys["hcaptcha"] = secret
	}

	return cfg
}
