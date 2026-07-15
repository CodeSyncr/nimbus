package supabase

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/container"
	"github.com/CodeSyncr/nimbus/database"
	"github.com/CodeSyncr/nimbus/health"
	"github.com/CodeSyncr/nimbus/router"
)

var (
	_ nimbus.Plugin          = (*Plugin)(nil)
	_ nimbus.HasBindings     = (*Plugin)(nil)
	_ nimbus.HasMiddleware   = (*Plugin)(nil)
	_ nimbus.HasHealthChecks = (*Plugin)(nil)
	_ nimbus.HasShutdown     = (*Plugin)(nil)
)

// Config holds Supabase plugin configuration.
type Config struct {
	// URL is the Supabase project URL (e.g. https://xyz.supabase.co).
	URL string
	// AnonKey is the public API key for client-side access.
	AnonKey string
	// ServiceRoleKey is the server-side key with full access (bypasses RLS).
	ServiceRoleKey string
	// JWTSecret is the JWT secret for verifying Supabase access tokens.
	// Found in Supabase Dashboard → Settings → API → JWT Secret.
	JWTSecret string
	// DatabaseURL is the direct Postgres connection string for lucid/GORM.
	// If set, the plugin auto-configures the database connection.
	DatabaseURL string
	// DatabaseDebug enables SQL logging.
	DatabaseDebug bool
	// DatabasePool configures connection pooling.
	DatabasePool database.PoolConfig
}

// ConfigFromEnv builds config from environment variables.
func ConfigFromEnv() Config {
	return Config{
		URL:            os.Getenv("SUPABASE_URL"),
		AnonKey:        os.Getenv("SUPABASE_ANON_KEY"),
		ServiceRoleKey: os.Getenv("SUPABASE_SERVICE_ROLE_KEY"),
		JWTSecret:      os.Getenv("SUPABASE_JWT_SECRET"),
		DatabaseURL:    os.Getenv("SUPABASE_DB_URL"),
		DatabaseDebug:  os.Getenv("APP_ENV") == "development",
	}
}

// Plugin integrates Supabase services (DB, Auth, Storage, Edge Functions,
// Realtime, PostgREST) into Nimbus.
type Plugin struct {
	nimbus.BasePlugin
	config Config
	client *Client
}

// New creates a Supabase plugin. Pass nil to use ConfigFromEnv.
func New(cfg *Config) *Plugin {
	var config Config
	if cfg != nil {
		config = *cfg
	} else {
		config = ConfigFromEnv()
	}
	return &Plugin{
		BasePlugin: nimbus.BasePlugin{
			PluginName:    "supabase",
			PluginVersion: "1.0.0",
		},
		config: config,
	}
}

func (p *Plugin) Register(app *nimbus.App) error {
	return nil
}

func (p *Plugin) Bindings(c *container.Container) {
	c.Singleton("supabase", func() *Client {
		return p.client
	})
	c.Singleton("supabase.auth", func() *AuthClient {
		if p.client == nil {
			return nil
		}
		return p.client.Auth
	})
	c.Singleton("supabase.storage", func() *StorageClient {
		if p.client == nil {
			return nil
		}
		return p.client.Storage
	})
}

// Middleware exposes named middleware for route assignment.
//
//	protected := app.Router.Group("/api", app.Middleware("supabase.auth"))
func (p *Plugin) Middleware() map[string]router.Middleware {
	return map[string]router.Middleware{
		"supabase.auth":          AuthMiddleware(),
		"supabase.auth.optional": OptionalAuthMiddleware(),
	}
}

// HealthChecks reports Supabase connectivity status.
func (p *Plugin) HealthChecks() map[string]health.Check {
	return map[string]health.Check{
		"supabase": func(ctx context.Context) error {
			if p.client == nil {
				return fmt.Errorf("supabase client not initialized")
			}
			// Ping the Supabase REST API health endpoint.
			resp, err := p.client.do("GET", p.client.url+"/rest/v1/", nil, nil)
			if err != nil {
				return fmt.Errorf("supabase health: %w", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode >= 500 {
				return fmt.Errorf("supabase health: status %d", resp.StatusCode)
			}
			return nil
		},
	}
}

func (p *Plugin) Boot(app *nimbus.App) error {
	if p.config.URL == "" {
		return nil
	}

	// Validate the project URL scheme/host to avoid SSRF and plaintext key
	// transport when the URL comes from less-trusted configuration.
	if err := validateSupabaseURL(p.config.URL); err != nil {
		return fmt.Errorf("supabase: %w", err)
	}

	// Surface a misconfiguration that would otherwise silently degrade auth:
	// without a JWT secret, AuthMiddleware rejects every request and
	// OptionalAuthMiddleware treats every caller as anonymous.
	if p.config.JWTSecret == "" {
		log.Println("[supabase] WARNING: SUPABASE_JWT_SECRET is not set — " +
			"token verification is disabled; protected routes will reject all requests")
	}

	p.client = NewClient(p.config)
	SetGlobal(p.client)

	// Auto-configure database if a Supabase DB URL is provided.
	if p.config.DatabaseURL != "" {
		db, err := database.ConnectWithConfig(database.ConnectConfig{
			Driver:     "postgres",
			DSN:        p.config.DatabaseURL,
			Debug:      p.config.DatabaseDebug,
			PoolConfig: p.config.DatabasePool,
		})
		if err != nil {
			return fmt.Errorf("supabase: database connect: %w", err)
		}
		// Make the Supabase Postgres connection the global Nimbus DB.
		nimbus.SetDB(db)
		database.AddConnection("default", db)
		database.AddConnection("supabase", db)
		app.Container.Instance("db", db)
	}

	return nil
}

// validateSupabaseURL enforces an https:// project URL (http:// is permitted
// only for localhost during development), guarding against SSRF and plaintext
// transport of API keys when the URL is sourced from config.
func validateSupabaseURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("invalid URL %q: %w", raw, err)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("URL %q has no host", raw)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if isLocalHost(host) {
			return nil
		}
		return fmt.Errorf("insecure http:// URL is only allowed for localhost, got %q", raw)
	default:
		return fmt.Errorf("unsupported URL scheme %q (expected https)", u.Scheme)
	}
}

func isLocalHost(host string) bool {
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

func (p *Plugin) Shutdown() error {
	c := GetClient()
	if c != nil && c.Realtime != nil {
		c.Realtime.Close()
	}
	return nil
}

// ── Health Check HTTP Handler (optional mount) ──────────────────

// HealthHandler returns an HTTP handler that reports Supabase health.
// Mount it manually if you want a dedicated endpoint:
//
//	app.Router.Get("/supabase/health", supabase.HealthHandler())
func HealthHandler() func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		c := GetClient()
		if c == nil {
			w.WriteHeader(503)
			json.NewEncoder(w).Encode(map[string]string{
				"status": "unavailable", "error": "client not initialized",
			})
			return
		}
		resp, err := c.do("GET", c.url+"/rest/v1/", nil, nil)
		if err != nil {
			// Log the detailed error server-side; return a generic status so we
			// don't leak upstream host/network details to unauthenticated callers.
			log.Printf("[supabase] health check failed: %v", err)
			w.WriteHeader(503)
			json.NewEncoder(w).Encode(map[string]string{
				"status": "unavailable",
			})
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 500 {
			w.WriteHeader(503)
			json.NewEncoder(w).Encode(map[string]any{
				"status": "degraded", "upstream_status": resp.StatusCode,
			})
			return
		}
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
	}
}
