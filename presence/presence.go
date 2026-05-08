/*
|--------------------------------------------------------------------------
| Nimbus Realtime Presence
|--------------------------------------------------------------------------
|
| Built-in presence channels with user tracking, typing indicators,
| and collaborative state. Powers real-time features with zero
| external dependencies.
|
| Usage:
|
|   // Setup
|   hub := presence.NewHub(presence.Config{
|       AuthFunc: func(r *http.Request, channel string) (*presence.User, error) {
|           // authenticate and return user
|           return &presence.User{ID: "123", Name: "Yash"}, nil
|       },
|   })
|   app.Use(hub.Plugin())
|
|   // Client-side (JavaScript)
|   const ws = new WebSocket("ws://localhost:3333/_presence?channel=room-1")
|   ws.send(JSON.stringify({type: "typing", data: {typing: true}}))
|   ws.onmessage = (e) => console.log(JSON.parse(e.data))
|
|   // Events received:
|   // {type: "presence:join", user: {id: "123", name: "Yash"}}
|   // {type: "presence:leave", user: {id: "123", name: "Yash"}}
|   // {type: "presence:typing", user: {id: "123"}, data: {typing: true}}
|   // {type: "presence:state", users: [{id: "123", name: "Yash"}]}
|   // {type: "message", data: "hello"}
|
*/

package presence

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/CodeSyncr/nimbus"
	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

// ---------------------------------------------------------------------------
// Core Types
// ---------------------------------------------------------------------------

// User represents a connected user in a presence channel.
type User struct {
	ID       string            `json:"id"`
	Name     string            `json:"name,omitempty"`
	Avatar   string            `json:"avatar,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Event is a message sent over the presence channel.
type Event struct {
	Type    string `json:"type"`
	Channel string `json:"channel,omitempty"`
	User    *User  `json:"user,omitempty"`
	Users   []User `json:"users,omitempty"`
	Data    any    `json:"data,omitempty"`
}

// Config configures the presence hub.
type Config struct {
	// AuthFunc authenticates a user for a specific channel.
	// Return nil user to reject the connection.
	AuthFunc func(r *http.Request, channel string) (*User, error)

	// PingInterval controls how often to send ping frames (default: 30s).
	PingInterval time.Duration

	// WriteTimeout for websocket writes (default: 10s).
	WriteTimeout time.Duration

	// MaxMessageSize in bytes (default: 4096).
	MaxMessageSize int64

	// Path for the websocket endpoint (default: "/_presence").
	Path string

	// AllowedOrigins controls accepted websocket Origin hosts.
	// When empty, same-origin requests are allowed by default.
	AllowedOrigins []string
}

// ---------------------------------------------------------------------------
// Nimbus Plugin
// ---------------------------------------------------------------------------

var (
	_ nimbus.Plugin    = (*PresencePlugin)(nil)
	_ nimbus.HasRoutes = (*PresencePlugin)(nil)
)

// PresencePlugin integrates presence channels with Nimbus.
type PresencePlugin struct {
	nimbus.BasePlugin
	Hub *Hub
}

// NewPlugin creates a presence plugin.
func NewPlugin(cfg Config) *PresencePlugin {
	return &PresencePlugin{
		BasePlugin: nimbus.BasePlugin{
			PluginName:    "presence",
			PluginVersion: "1.0.0",
		},
		Hub: NewHub(cfg),
	}
}

func (p *PresencePlugin) Register(app *nimbus.App) error {
	app.Container.Singleton("presence.hub", func() *Hub { return p.Hub })
	return nil
}

func (p *PresencePlugin) Boot(app *nimbus.App) error {
	return nil
}

// RegisterRoutes mounts presence endpoints.
func (p *PresencePlugin) RegisterRoutes(r *router.Router) {
	path := p.Hub.config.Path
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	// WebSocket endpoint
	r.Get(path, func(c *nhttp.Context) error {
		p.Hub.HandleWebSocket(c.Response, c.Request)
		return nil
	})

	// REST API for presence state
	r.Get(path+"/channels", func(c *nhttp.Context) error {
		channels := p.Hub.Channels()
		result := make([]map[string]any, 0, len(channels))
		for _, name := range channels {
			result = append(result, map[string]any{
				"name":  name,
				"users": p.Hub.UserCount(name),
			})
		}
		return c.JSON(200, result)
	})

	r.Get(path+"/channels/:name/users", func(c *nhttp.Context) error {
		name := c.Param("name")
		users := p.Hub.UsersIn(name)
		if users == nil {
			return c.JSON(404, map[string]string{"error": "channel not found"})
		}
		return c.JSON(200, users)
	})

	// Server-side broadcast
	r.Post(path+"/channels/:name/broadcast", func(c *nhttp.Context) error {
		name := c.Param("name")
		var event Event
		if err := json.NewDecoder(c.Request.Body).Decode(&event); err != nil {
			return c.JSON(400, map[string]string{"error": "invalid body"})
		}
		event.Channel = name
		p.Hub.Broadcast(name, event)
		return c.JSON(200, map[string]string{"status": "sent"})
	})
}
