/*
|--------------------------------------------------------------------------
| Transmit Plugin for Nimbus
|--------------------------------------------------------------------------
|
| Server-Sent Events (SSE) for real-time server-to-client push.
| Inspired by AdonisJS Transmit: https://docs.adonisjs.com/guides/digging-deeper/server-sent-events
|
|   transmit.Broadcast("notifications", map[string]any{"msg": "Hello"})
|   transmit.BroadcastExcept("chats/1", data, senderUID)
|   transmit.Authorize("users/:id", func(ctx, params) bool { return ctx.Auth.UserID == params["id"] })
|
| Routes: GET __transmit/events, POST __transmit/subscribe, POST __transmit/unsubscribe
|
| Production: Disable compression for text/event-stream in your reverse proxy.
|   Nginx: exclude text/event-stream from gzip_types
|   Traefik: excludedcontenttypes=text/event-stream
|
| Multi-instance: Set TRANSMIT_TRANSPORT=redis (with REDIS_URL) to sync across instances.
|   Or pass Config.Transport: transmit.NewRedisTransport(transmit.RedisTransportConfig{})
|
*/

package transmit

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

var _ nimbus.Plugin = (*Plugin)(nil)
var _ nimbus.HasRoutes = (*Plugin)(nil)
var _ nimbus.HasShutdown = (*Plugin)(nil)

var (
	globalStore     *Store
	globalTransport Transport
	globalMu        sync.RWMutex
)

func setGlobalStore(s *Store) {
	globalMu.Lock()
	defer globalMu.Unlock()
	globalStore = s
}

func setGlobalTransport(t Transport) {
	globalMu.Lock()
	defer globalMu.Unlock()
	globalTransport = t
}

// GetStore returns the global store (for Broadcast).
func GetStore() *Store {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return globalStore
}

func getGlobalTransport() Transport {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return globalTransport
}

// Config holds Transmit configuration.
type Config struct {
	Path         string              // route prefix, default __transmit
	PingInterval string              // e.g. "30s", "1m", empty to disable
	Middleware   []router.Middleware // optional middleware for all transmit routes (e.g. auth)
	Transport    Transport           // optional; Redis for multi-instance sync
}

// Plugin integrates SSE into Nimbus.
type Plugin struct {
	nimbus.BasePlugin
	store        *Store
	path         string
	pingInterval time.Duration
	middleware   []router.Middleware
	transport    Transport
}

// New creates a new Transmit plugin.
func New(cfg *Config) *Plugin {
	path := "__transmit"
	pingInterval := time.Duration(0)
	var middleware []router.Middleware
	var transport Transport
	if cfg != nil {
		if cfg.Path != "" {
			path = strings.TrimSuffix(cfg.Path, "/")
		}
		if cfg.PingInterval != "" {
			if d, err := time.ParseDuration(cfg.PingInterval); err == nil {
				pingInterval = d
			}
		}
		middleware = cfg.Middleware
		transport = cfg.Transport
	}
	if p := os.Getenv("TRANSMIT_PATH"); p != "" {
		path = strings.TrimSuffix(p, "/")
	}
	if s := os.Getenv("TRANSMIT_PING_INTERVAL"); s != "" {
		if d, err := time.ParseDuration(s); err == nil {
			pingInterval = d
		}
	}
	if transport == nil && os.Getenv("TRANSMIT_TRANSPORT") == "redis" {
		if rt, err := NewRedisTransport(RedisTransportConfig{}); err == nil {
			transport = rt
		}
	}
	return &Plugin{
		BasePlugin: nimbus.BasePlugin{
			PluginName:    "transmit",
			PluginVersion: "1.0.0",
		},
		store:        NewStore(),
		path:         path,
		pingInterval: pingInterval,
		middleware:   middleware,
		transport:    transport,
	}
}

// Register sets the global store and transport.
func (p *Plugin) Register(app *nimbus.App) error {
	setGlobalStore(p.store)
	setGlobalTransport(p.transport)
	return nil
}

// Boot starts the transport subscriber when configured.
func (p *Plugin) Boot(app *nimbus.App) error {
	if p.transport == nil {
		return nil
	}
	go func() {
		ctx := context.Background()
		_ = p.transport.Subscribe(ctx, func(channel string, payload any, excludeUIDs []string) {
			s := GetStore()
			if s != nil {
				s.DeliverToChannel(channel, payload, excludeUIDs...)
			}
		})
	}()
	return nil
}

// Shutdown closes the transport when configured.
func (p *Plugin) Shutdown() error {
	if p.transport != nil {
		return p.transport.Close()
	}
	return nil
}

// RegisterRoutes mounts SSE routes.
func (p *Plugin) RegisterRoutes(r *router.Router) {
	base := "/" + p.path
	if len(p.middleware) > 0 {
		grp := r.Group(base, p.middleware...)
		grp.Get("/events", p.eventsHandler)
		grp.Post("/subscribe", p.subscribeHandler)
		grp.Post("/unsubscribe", p.unsubscribeHandler)
	} else {
		r.Get(base+"/events", p.eventsHandler)
		r.Post(base+"/subscribe", p.subscribeHandler)
		r.Post(base+"/unsubscribe", p.unsubscribeHandler)
	}
}

func (p *Plugin) eventsHandler(c *http.Context) error {
	w := c.Response
	req := c.Request

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil
	}

	uid, client := p.store.Connect()
	emitConnect(uid)
	defer func() {
		p.store.Disconnect(uid)
		emitDisconnect(uid)
	}()

	// Send UID to client
	uidMsg := []byte("data: {\"uid\":\"" + uid + "\"}\n\n")
	w.Write(uidMsg)
	flusher.Flush()

	var pingCh <-chan time.Time
	if p.pingInterval > 0 {
		ticker := time.NewTicker(p.pingInterval)
		defer ticker.Stop()
		pingCh = ticker.C
	} else {
		never := make(chan time.Time)
		pingCh = never
	}

	for {
		select {
		case msg := <-client.Events:
			if _, err := w.Write(msg); err != nil {
				return nil
			}
			flusher.Flush()
		case <-client.Done:
			return nil
		case <-req.Context().Done():
			return nil
		case <-pingCh:
			if _, err := w.Write([]byte(": ping\n\n")); err != nil {
				return nil
			}
			flusher.Flush()
		}
	}
}

func (p *Plugin) subscribeHandler(c *http.Context) error {
	var body struct {
		Channel string `json:"channel"`
		UID     string `json:"uid"`
	}
	if err := json.NewDecoder(c.Request.Body).Decode(&body); err != nil {
		c.Status(400)
		return c.JSON(400, map[string]string{"error": "invalid json"})
	}
	if body.Channel == "" || body.UID == "" {
		c.Status(400)
		return c.JSON(400, map[string]string{"error": "channel and uid required"})
	}
	if !CheckChannel(c, body.Channel) {
		c.Status(403)
		return c.JSON(403, map[string]string{"error": "unauthorized"})
	}
	p.store.Subscribe(body.Channel, body.UID)
	emitSubscribe(body.UID, body.Channel)
	return c.JSON(200, map[string]string{"status": "subscribed"})
}

func (p *Plugin) unsubscribeHandler(c *http.Context) error {
	var body struct {
		Channel string `json:"channel"`
		UID     string `json:"uid"`
	}
	if err := json.NewDecoder(c.Request.Body).Decode(&body); err != nil {
		c.Status(400)
		return c.JSON(400, map[string]string{"error": "invalid json"})
	}
	if body.Channel == "" || body.UID == "" {
		c.Status(400)
		return c.JSON(400, map[string]string{"error": "channel and uid required"})
	}
	p.store.Unsubscribe(body.Channel, body.UID)
	emitUnsubscribe(body.UID, body.Channel)
	return c.JSON(200, map[string]string{"status": "unsubscribed"})
}
