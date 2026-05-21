package supabase

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/container"
	"github.com/CodeSyncr/nimbus/database"
	"github.com/CodeSyncr/nimbus/health"
	"github.com/CodeSyncr/nimbus/router"
)

var (
	_ nimbus.Plugin         = (*Plugin)(nil)
	_ nimbus.HasBindings    = (*Plugin)(nil)
	_ nimbus.HasMiddleware  = (*Plugin)(nil)
	_ nimbus.HasHealthChecks = (*Plugin)(nil)
	_ nimbus.HasShutdown    = (*Plugin)(nil)
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
		c := GetClient()
		if c == nil {
			w.WriteHeader(503)
			fmt.Fprint(w, `{"status":"unavailable","error":"client not initialized"}`)
			return
		}
		resp, err := c.do("GET", c.url+"/rest/v1/", nil, nil)
		if err != nil {
			w.WriteHeader(503)
			fmt.Fprintf(w, `{"status":"unavailable","error":"%s"}`, err.Error())
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 500 {
			w.WriteHeader(503)
			fmt.Fprintf(w, `{"status":"degraded","upstream_status":%d}`, resp.StatusCode)
			return
		}
		w.WriteHeader(200)
		fmt.Fprint(w, `{"status":"healthy"}`)
	}
}
