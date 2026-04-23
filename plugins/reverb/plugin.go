/*
|--------------------------------------------------------------------------
| Reverb — WebSocket broadcasting (Laravel Reverb–style)
|--------------------------------------------------------------------------
|
| Optional plugin for channel-based WebSocket push. Complements Transmit (SSE).
|
|   app.Use(reverb.New(nil)) // defaults: path /reverb/ws, REDIS_URL for multi-node
|
|   reverb.Broadcast(ctx, "orders.1", map[string]any{"status": "shipped"})
|
| Browser: connect WebSocket to /reverb/ws, then send:
|   {"action":"subscribe","channel":"orders.1"}
|
| Server → client events: connected, subscribed, message, pong
|
| Env:
|   REVERB_PATH=/reverb/ws
|   REVERB_ALLOWED_ORIGINS=https://app.example.com,https://www.example.com
|   REVERB_REDIS_CHANNEL=nimbus:reverb:fanout  (optional)
|
*/

package reverb

import (
	"context"
	"os"
	"strings"
	"sync"

	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/redis"
	"github.com/CodeSyncr/nimbus/router"
)

var (
	_ nimbus.Plugin    = (*Plugin)(nil)
	_ nimbus.HasRoutes = (*Plugin)(nil)
	_ nimbus.HasShutdown = (*Plugin)(nil)

	globalMu sync.RWMutex
	globalHub *Hub
)

// Config configures the Reverb plugin.
type Config struct {
	// Path is the WebSocket upgrade route (GET). Default /reverb/ws
	Path string
	// RedisURL enables cross-instance fan-out (same as REDIS_URL if empty).
	RedisURL string
	// AllowedOrigins restricts WS Origin header; empty allows same-host only.
	AllowedOrigins []string
	// FanoutChannel overrides the Redis Pub/Sub channel name.
	FanoutChannel string
	// Gate authorizes the upgrade request. Nil = allow (use middleware on the route group for auth).
	Gate func(*http.Context) bool
}

// Plugin provides WebSocket broadcasting.
type Plugin struct {
	nimbus.BasePlugin
	cfg Config
	hub *Hub
}

// New creates a Reverb plugin with defaults from environment.
func New(cfg *Config) *Plugin {
	c := Config{
		Path:     "/reverb/ws",
		RedisURL: os.Getenv("REDIS_URL"),
	}
	if cfg != nil {
		if cfg.Path != "" {
			c.Path = cfg.Path
		}
		if cfg.RedisURL != "" {
			c.RedisURL = cfg.RedisURL
		}
		c.AllowedOrigins = cfg.AllowedOrigins
		c.FanoutChannel = cfg.FanoutChannel
		c.Gate = cfg.Gate
	}
	if p := os.Getenv("REVERB_PATH"); p != "" {
		c.Path = p
	}
	if o := os.Getenv("REVERB_ALLOWED_ORIGINS"); o != "" {
		for _, part := range strings.Split(o, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				c.AllowedOrigins = append(c.AllowedOrigins, part)
			}
		}
	}
	if ch := os.Getenv("REVERB_REDIS_CHANNEL"); ch != "" {
		c.FanoutChannel = ch
	}

	var rdb *redis.Client
	if c.RedisURL != "" {
		if opt, err := redis.ParseURL(c.RedisURL); err == nil {
			rdb = redis.NewClient(opt)
		}
	}
	h := newHub(rdb, c.AllowedOrigins, c.FanoutChannel)

	p := &Plugin{
		BasePlugin: nimbus.BasePlugin{
			PluginName:    "reverb",
			PluginVersion: "1.0.0",
		},
		cfg: c,
		hub: h,
	}
	return p
}

// Register exposes the hub for Broadcast.
func (p *Plugin) Register(app *nimbus.App) error {
	globalMu.Lock()
	globalHub = p.hub
	globalMu.Unlock()
	return nil
}

// Boot is reserved.
func (p *Plugin) Boot(app *nimbus.App) error { return nil }

// Shutdown closes Redis subscriptions.
func (p *Plugin) Shutdown() error {
	if p.hub != nil {
		return p.hub.shutdown()
	}
	return nil
}

// RegisterRoutes mounts the WebSocket endpoint.
func (p *Plugin) RegisterRoutes(r *router.Router) {
	path := strings.TrimSuffix(p.cfg.Path, "/")
	if path == "" {
		path = "/reverb/ws"
	}
	r.Get(path, func(c *http.Context) error {
		if p.cfg.Gate != nil && !p.cfg.Gate(c) {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "forbidden"})
		}
		p.hub.serveWS(c.Response, c.Request)
		return nil
	})
	// Simple health probe for the feature (not the WS upgrade itself).
	base := path
	if idx := strings.LastIndex(base, "/"); idx > 0 {
		base = base[:idx]
	}
	if base == "" {
		base = "/reverb"
	}
	r.Get(base+"/health", func(c *http.Context) error {
		return c.JSON(http.StatusOK, map[string]any{"reverb": true, "redis_fanout": p.hub.redis != nil})
	})
}

// GetHub returns the active hub (for advanced use). May be nil if the plugin is not registered.
func GetHub() *Hub {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return globalHub
}

// Broadcast sends to all subscribers on channel across instances when Redis is configured.
func Broadcast(ctx context.Context, channel string, data any) error {
	globalMu.RLock()
	h := globalHub
	globalMu.RUnlock()
	if h == nil {
		return nil
	}
	return h.Broadcast(ctx, channel, data)
}
