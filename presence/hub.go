package presence

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Hub
// ---------------------------------------------------------------------------

// Hub manages presence channels and the connections this instance serves.
// With Config.Redis set, membership and events are shared by every hub
// using the same Redis, so users connected to different app instances see
// each other.
type Hub struct {
	config         Config
	instanceID     string
	allowedOrigins map[string]struct{}
	backend        backend

	mu       sync.RWMutex
	channels map[string]*Channel // channels with connections on this instance
}

// Channel is a presence channel as seen from one hub. Membership queries
// (Users, Count) cover every instance; Broadcast reaches every instance.
type Channel struct {
	name    string
	hub     *Hub
	mu      sync.RWMutex
	clients map[*Client]struct{}
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
	h := &Hub{
		config:         cfg,
		instanceID:     randomID(),
		allowedOrigins: normalizeOrigins(cfg.AllowedOrigins),
		channels:       make(map[string]*Channel),
	}
	if cfg.Redis != nil {
		h.backend = newRedisBackend(h, cfg.Redis, cfg.RedisPrefix)
	} else {
		h.backend = newLocalBackend(h)
	}
	return h
}

// Close stops cross-instance syncing. Connections stay open.
func (h *Hub) Close() error {
	return h.backend.close()
}

func randomID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (h *Hub) ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}

// getOrCreateChannel returns or creates this instance's view of a channel.
func (h *Hub) getOrCreateChannel(name string) *Channel {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch, ok := h.channels[name]
	if !ok {
		ch = &Channel{name: name, hub: h, clients: make(map[*Client]struct{})}
		h.channels[name] = ch
	}
	return ch
}

// GetChannel returns a channel if anyone is in it, nil otherwise.
func (h *Hub) GetChannel(name string) *Channel {
	h.mu.RLock()
	ch, ok := h.channels[name]
	h.mu.RUnlock()
	if ok {
		return ch
	}
	if h.UserCount(name) == 0 {
		return nil
	}
	return &Channel{name: name, hub: h, clients: map[*Client]struct{}{}}
}

// Channels returns the names of channels with anyone in them.
func (h *Hub) Channels() []string {
	ctx, cancel := h.ctx()
	defer cancel()
	names, err := h.backend.channels(ctx)
	if err != nil {
		log.Printf("[presence] list channels: %v", err)
	}
	return names
}

// Broadcast sends an event to everyone in a channel.
func (h *Hub) Broadcast(channel string, event Event) {
	h.publishEvent(channel, event, "", "")
}

// BroadcastExcept sends an event to everyone in a channel except one user
// (all of that user's connections).
func (h *Hub) BroadcastExcept(channel string, event Event, exceptUserID string) {
	h.publishEvent(channel, event, exceptUserID, "")
}

// SendTo sends an event to every connection of one user in a channel.
func (h *Hub) SendTo(channel string, event Event, userID string) {
	h.publishEvent(channel, event, "", userID)
}

// UsersIn returns the users in a channel, or nil if it is empty.
func (h *Hub) UsersIn(channel string) []User {
	ctx, cancel := h.ctx()
	defer cancel()
	users, err := h.backend.users(ctx, channel)
	if err != nil {
		log.Printf("[presence] users in %q: %v", channel, err)
		return nil
	}
	if len(users) == 0 {
		return nil
	}
	return users
}

// UserCount returns the number of distinct users in a channel.
func (h *Hub) UserCount(channel string) int {
	ctx, cancel := h.ctx()
	defer cancel()
	n, err := h.backend.userCount(ctx, channel)
	if err != nil {
		log.Printf("[presence] count %q: %v", channel, err)
	}
	return n
}

func (h *Hub) publishEvent(channel string, event Event, exceptUser, toUser string) {
	ctx, cancel := h.ctx()
	defer cancel()
	h.publish(ctx, channel, event, exceptUser, toUser)
}

func (h *Hub) publish(ctx context.Context, channel string, event Event, exceptUser, toUser string) {
	if event.Channel == "" {
		event.Channel = channel
	}
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	r := routed{Channel: channel, Data: data, ExceptUser: exceptUser, ToUser: toUser}
	if err := h.backend.publish(ctx, r); err != nil {
		log.Printf("[presence] publish to %q: %v", channel, err)
	}
}

// deliver hands an event to this instance's connections in the channel.
func (h *Hub) deliver(r routed) {
	h.mu.RLock()
	ch := h.channels[r.Channel]
	h.mu.RUnlock()
	if ch == nil {
		return
	}
	ch.mu.RLock()
	defer ch.mu.RUnlock()
	for c := range ch.clients {
		if r.ExceptUser != "" && c.user.ID == r.ExceptUser {
			continue
		}
		if r.ToUser != "" && c.user.ID != r.ToUser {
			continue
		}
		c.trySend(r.Data)
	}
}

// localClients snapshots this instance's connections by channel.
func (h *Hub) localClients() map[string][]*Client {
	h.mu.RLock()
	chans := make([]*Channel, 0, len(h.channels))
	for _, ch := range h.channels {
		chans = append(chans, ch)
	}
	h.mu.RUnlock()
	out := make(map[string][]*Client, len(chans))
	for _, ch := range chans {
		ch.mu.RLock()
		for c := range ch.clients {
			out[ch.name] = append(out[ch.name], c)
		}
		ch.mu.RUnlock()
	}
	return out
}

// ---------------------------------------------------------------------------
// Channel Operations
// ---------------------------------------------------------------------------

// Join adds a connection to the channel. Other members get presence:join
// only for the user's first connection; a second tab joins silently.
func (ch *Channel) Join(client *Client) {
	h := ch.hub
	h.mu.Lock()
	// The channel may have been emptied and dropped since it was looked up.
	if cur, ok := h.channels[ch.name]; ok && cur != ch {
		ch = cur
	} else if !ok {
		h.channels[ch.name] = ch
	}
	ch.mu.Lock()
	ch.clients[client] = struct{}{}
	ch.mu.Unlock()
	h.mu.Unlock()

	ctx, cancel := h.ctx()
	defer cancel()
	first, err := h.backend.join(ctx, ch.name, client)
	if err != nil {
		log.Printf("[presence] join %q: %v", ch.name, err)
	}

	users, _ := h.backend.users(ctx, ch.name)
	state, _ := json.Marshal(Event{Type: "presence:state", Channel: ch.name, Users: users})
	client.trySend(state)

	if first {
		h.publish(ctx, ch.name, Event{Type: "presence:join", Channel: ch.name, User: client.user}, client.user.ID, "")
	}
}

// Leave removes a connection from the channel. Members get presence:leave
// only when it was the user's last connection.
func (ch *Channel) Leave(client *Client) {
	h := ch.hub
	h.mu.Lock()
	ch.mu.Lock()
	_, present := ch.clients[client]
	delete(ch.clients, client)
	empty := len(ch.clients) == 0
	ch.mu.Unlock()
	if empty && h.channels[ch.name] == ch {
		delete(h.channels, ch.name)
	}
	h.mu.Unlock()
	if !present {
		return
	}

	ctx, cancel := h.ctx()
	defer cancel()
	last, err := h.backend.leave(ctx, ch.name, client)
	if err != nil {
		log.Printf("[presence] leave %q: %v", ch.name, err)
		return
	}
	if last {
		h.publish(ctx, ch.name, Event{Type: "presence:leave", Channel: ch.name, User: client.user}, "", "")
	}
}

// Broadcast sends an event to everyone in the channel.
func (ch *Channel) Broadcast(event Event) {
	ch.hub.Broadcast(ch.name, event)
}

// BroadcastExcept sends to everyone in the channel except one user.
func (ch *Channel) BroadcastExcept(event Event, exceptUserID string) {
	ch.hub.BroadcastExcept(ch.name, event, exceptUserID)
}

// Users returns the distinct users in the channel, across instances.
func (ch *Channel) Users() []User {
	users := ch.hub.UsersIn(ch.name)
	if users == nil {
		return []User{}
	}
	return users
}

// Count returns the number of distinct users in the channel.
func (ch *Channel) Count() int {
	return ch.hub.UserCount(ch.name)
}
