package presence

import (
	"encoding/json"
	"log"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Hub
// ---------------------------------------------------------------------------

// Hub manages all presence channels and their connected clients.
type Hub struct {
	config         Config
	channels       sync.Map // channel name -> *Channel
	allowedOrigins map[string]struct{}
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
	return &Hub{
		config:         cfg,
		allowedOrigins: normalizeOrigins(cfg.AllowedOrigins),
	}
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
	
	// Snapshot users while still holding the lock
	users := make([]User, 0, len(ch.clients))
	for _, c := range ch.clients {
		users = append(users, *c.user)
	}
	ch.mu.Unlock()

	// Send current presence state to the new client
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
