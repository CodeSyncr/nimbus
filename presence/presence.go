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
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/CodeSyncr/nimbus"
	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
	"github.com/gorilla/websocket"
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

// Client represents a single websocket connection.
type Client struct {
	user    *User
	conn    *websocket.Conn
	channel string
	send    chan []byte
	hub     *Hub
	mu      sync.Mutex
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
}

// ---------------------------------------------------------------------------
// Hub
// ---------------------------------------------------------------------------

// Hub manages all presence channels and their connected clients.
type Hub struct {
	config   Config
	channels sync.Map // channel name -> *Channel
}

// Channel represents a single presence channel.
type Channel struct {
	name    string
	mu      sync.RWMutex
	clients map[string]*Client // user ID -> client
	hub     *Hub
}

// NewHub creates a new presence hub.
func NewHub(cfg Config) *Hub {
	if cfg.PingInterval <= 0 {
		cfg.PingInterval = 30 * time.Second
	}
	if cfg.WriteTimeout <= 0 {
		cfg.WriteTimeout = 10 * time.Second
	}
	if cfg.MaxMessageSize <= 0 {
		cfg.MaxMessageSize = 4096
	}
	if cfg.Path == "" {
		cfg.Path = "/_presence"
	}
	return &Hub{config: cfg}
}

// getOrCreateChannel returns or creates a channel.
func (h *Hub) getOrCreateChannel(name string) *Channel {
	val, loaded := h.channels.LoadOrStore(name, &Channel{
		name:    name,
		clients: make(map[string]*Client),
		hub:     h,
	})
	if !loaded {
		log.Printf("[presence] channel %q created", name)
	}
	return val.(*Channel)
}

// GetChannel returns a channel if it exists, nil otherwise.
func (h *Hub) GetChannel(name string) *Channel {
	val, ok := h.channels.Load(name)
	if !ok {
		return nil
	}
	return val.(*Channel)
}

// Channels returns all active channel names.
func (h *Hub) Channels() []string {
	var names []string
	h.channels.Range(func(key, _ any) bool {
		names = append(names, key.(string))
		return true
	})
	return names
}

// Broadcast sends a message to all clients in a channel.
func (h *Hub) Broadcast(channel string, event Event) {
	ch := h.GetChannel(channel)
	if ch == nil {
		return
	}
	ch.Broadcast(event)
}

// BroadcastExcept sends a message to all clients except the specified user.
func (h *Hub) BroadcastExcept(channel string, event Event, exceptUserID string) {
	ch := h.GetChannel(channel)
	if ch == nil {
		return
	}
	ch.BroadcastExcept(event, exceptUserID)
}

// UsersIn returns all users currently in a channel.
func (h *Hub) UsersIn(channel string) []User {
	ch := h.GetChannel(channel)
	if ch == nil {
		return nil
	}
	return ch.Users()
}

// UserCount returns the number of users in a channel.
func (h *Hub) UserCount(channel string) int {
	ch := h.GetChannel(channel)
	if ch == nil {
		return 0
	}
	return ch.Count()
}

// ---------------------------------------------------------------------------
// Channel Operations
// ---------------------------------------------------------------------------

// Join adds a client to the channel.
func (ch *Channel) Join(client *Client) {
	ch.mu.Lock()
	// Kick existing connection for same user
	if existing, ok := ch.clients[client.user.ID]; ok {
		close(existing.send)
	}
	ch.clients[client.user.ID] = client
	ch.mu.Unlock()

	// Send current presence state to the new client
	users := ch.Users()
	stateEvent := Event{
		Type:    "presence:state",
		Channel: ch.name,
		Users:   users,
	}
	data, _ := json.Marshal(stateEvent)
	select {
	case client.send <- data:
	default:
	}

	// Broadcast join to others
	joinEvent := Event{
		Type:    "presence:join",
		Channel: ch.name,
		User:    client.user,
	}
	ch.BroadcastExcept(joinEvent, client.user.ID)
}

// Leave removes a client from the channel.
func (ch *Channel) Leave(client *Client) {
	ch.mu.Lock()
	// Only remove if it's still the same connection
	if existing, ok := ch.clients[client.user.ID]; ok && existing == client {
		delete(ch.clients, client.user.ID)
	}
	remaining := len(ch.clients)
	ch.mu.Unlock()

	// Broadcast leave
	leaveEvent := Event{
		Type:    "presence:leave",
		Channel: ch.name,
		User:    client.user,
	}
	ch.Broadcast(leaveEvent)

	// Clean up empty channels
	if remaining == 0 {
		ch.hub.channels.Delete(ch.name)
		log.Printf("[presence] channel %q removed (empty)", ch.name)
	}
}

// Broadcast sends a message to all clients in the channel.
func (ch *Channel) Broadcast(event Event) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	ch.mu.RLock()
	defer ch.mu.RUnlock()
	for _, client := range ch.clients {
		select {
		case client.send <- data:
		default:
			// Buffer full, skip this client
		}
	}
}

// BroadcastExcept sends to all clients except one.
func (ch *Channel) BroadcastExcept(event Event, exceptUserID string) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	ch.mu.RLock()
	defer ch.mu.RUnlock()
	for userID, client := range ch.clients {
		if userID == exceptUserID {
			continue
		}
		select {
		case client.send <- data:
		default:
		}
	}
}

// Users returns all users in the channel.
func (ch *Channel) Users() []User {
	ch.mu.RLock()
	defer ch.mu.RUnlock()
	users := make([]User, 0, len(ch.clients))
	for _, client := range ch.clients {
		users = append(users, *client.user)
	}
	return users
}

// Count returns the number of connected users.
func (ch *Channel) Count() int {
	ch.mu.RLock()
	defer ch.mu.RUnlock()
	return len(ch.clients)
}

// ---------------------------------------------------------------------------
// WebSocket Handler
// ---------------------------------------------------------------------------

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// HandleWebSocket upgrades an HTTP connection and joins a presence channel.
func (h *Hub) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	channel := r.URL.Query().Get("channel")
	if channel == "" {
		http.Error(w, "missing channel parameter", http.StatusBadRequest)
		return
	}

	// Authenticate
	var user *User
	if h.config.AuthFunc != nil {
		u, err := h.config.AuthFunc(r, channel)
		if err != nil || u == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		user = u
	} else {
		// Default: use query param or generate anonymous user
		id := r.URL.Query().Get("user_id")
		name := r.URL.Query().Get("user_name")
		if id == "" {
			id = fmt.Sprintf("anon-%d", time.Now().UnixNano()%100000)
		}
		if name == "" {
			name = id
		}
		user = &User{ID: id, Name: name}
	}

	// Upgrade connection
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[presence] upgrade failed: %v", err)
		return
	}

	client := &Client{
		user:    user,
		conn:    conn,
		channel: channel,
		send:    make(chan []byte, 256),
		hub:     h,
	}

	ch := h.getOrCreateChannel(channel)
	ch.Join(client)

	go client.writePump()
	go client.readPump(ch)
}

// ---------------------------------------------------------------------------
// Client Read/Write Pumps
// ---------------------------------------------------------------------------

func (c *Client) readPump(ch *Channel) {
	defer func() {
		ch.Leave(c)
		c.conn.Close()
	}()

	c.conn.SetReadLimit(c.hub.config.MaxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(c.hub.config.PingInterval * 2))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(c.hub.config.PingInterval * 2))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("[presence] read error: %v", err)
			}
			return
		}

		// Parse incoming message
		var incoming struct {
			Type string `json:"type"`
			Data any    `json:"data"`
		}
		if err := json.Unmarshal(message, &incoming); err != nil {
			continue
		}

		switch incoming.Type {
		case "typing":
			// Broadcast typing indicator to others
			event := Event{
				Type:    "presence:typing",
				Channel: ch.name,
				User:    c.user,
				Data:    incoming.Data,
			}
			ch.BroadcastExcept(event, c.user.ID)

		case "message":
			// Broadcast message to all in channel
			event := Event{
				Type:    "message",
				Channel: ch.name,
				User:    c.user,
				Data:    incoming.Data,
			}
			ch.Broadcast(event)

		case "whisper":
			// Private message to specific user
			if dataMap, ok := incoming.Data.(map[string]any); ok {
				targetID, _ := dataMap["to"].(string)
				ch.mu.RLock()
				target, exists := ch.clients[targetID]
				ch.mu.RUnlock()
				if exists {
					event := Event{
						Type:    "whisper",
						Channel: ch.name,
						User:    c.user,
						Data:    dataMap["message"],
					}
					data, _ := json.Marshal(event)
					select {
					case target.send <- data:
					default:
					}
				}
			}

		case "state":
			// Custom state update broadcast
			event := Event{
				Type:    "presence:update",
				Channel: ch.name,
				User:    c.user,
				Data:    incoming.Data,
			}
			ch.BroadcastExcept(event, c.user.ID)

		default:
			// Forward custom events to all channel members
			event := Event{
				Type:    "custom:" + incoming.Type,
				Channel: ch.name,
				User:    c.user,
				Data:    incoming.Data,
			}
			ch.BroadcastExcept(event, c.user.ID)
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(c.hub.config.PingInterval)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(c.hub.config.WriteTimeout))
			if !ok {
				// Channel closed
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(c.hub.config.WriteTimeout))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
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
