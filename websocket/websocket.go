package websocket

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/url"
	"sync"

	"github.com/CodeSyncr/nimbus/redis"
	"github.com/gorilla/websocket"
)

// DefaultRedisTopic is the Pub/Sub topic hubs use to share broadcasts.
const DefaultRedisTopic = "nimbus:websocket:broadcast"

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// Hub holds connected clients and broadcasts to them. With UseRedis, a
// broadcast on any instance reaches the clients of every instance.
type Hub struct {
	mu             sync.RWMutex
	clients        map[*Conn]struct{}
	broadcast      chan []byte
	register       chan *Conn
	unregister     chan *Conn
	allowedOrigins map[string]struct{}

	redis  *redis.Client
	topic  string
	pubsub *redis.PubSub
	cancel context.CancelFunc
}

// Conn wraps a websocket connection.
type Conn struct {
	*websocket.Conn
	send chan []byte
}

// NewHub returns a new hub. Call Run() to start.
func NewHub() *Hub {
	return &Hub{
		clients:        make(map[*Conn]struct{}),
		broadcast:      make(chan []byte, 256),
		register:       make(chan *Conn),
		unregister:     make(chan *Conn),
		allowedOrigins: make(map[string]struct{}),
	}
}

// SetAllowedOrigins configures allowed websocket origins.
// When empty, same-origin requests are allowed by default.
func (h *Hub) SetAllowedOrigins(origins []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.allowedOrigins = normalizeOrigins(origins)
}

// UseRedis makes Broadcast go through Redis Pub/Sub so every hub
// subscribed to topic (default DefaultRedisTopic) delivers it to its own
// clients. Call before Run.
func (h *Hub) UseRedis(client *redis.Client, topic string) error {
	if topic == "" {
		topic = DefaultRedisTopic
	}
	ctx, cancel := context.WithCancel(context.Background())
	ps := client.Subscribe(ctx, topic)
	if _, err := ps.Receive(ctx); err != nil {
		cancel()
		_ = ps.Close()
		return err
	}
	h.mu.Lock()
	h.redis, h.topic, h.pubsub, h.cancel = client, topic, ps, cancel
	h.mu.Unlock()
	go func() {
		for msg := range ps.Channel() {
			h.broadcast <- []byte(msg.Payload)
		}
	}()
	return nil
}

// Close stops the Redis subscription, if any.
func (h *Hub) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cancel != nil {
		h.cancel()
	}
	if h.pubsub != nil {
		return h.pubsub.Close()
	}
	return nil
}

// Run runs the hub (blocks). Call in a goroutine.
func (h *Hub) Run() {
	for {
		select {
		case c := <-h.register:
			h.mu.Lock()
			h.clients[c] = struct{}{}
			h.mu.Unlock()
		case c := <-h.unregister:
			h.mu.Lock()
			delete(h.clients, c)
			close(c.send)
			h.mu.Unlock()
		case msg := <-h.broadcast:
			h.mu.RLock()
			for c := range h.clients {
				select {
				case c.send <- msg:
				default:
					// skip full buffer
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Broadcast sends a message to all connected clients (of every instance,
// with UseRedis).
func (h *Hub) Broadcast(msg []byte) {
	h.mu.RLock()
	client, topic := h.redis, h.topic
	h.mu.RUnlock()
	if client != nil {
		if err := client.Publish(context.Background(), topic, msg).Err(); err != nil {
			log.Printf("[websocket] redis publish: %v", err)
		}
		return
	}
	h.broadcast <- msg
}

// Len returns the number of clients connected to this instance.
func (h *Hub) Len() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// Upgrade upgrades the HTTP request to WebSocket and registers the conn with the hub.
func (h *Hub) Upgrade(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	u := upgrader
	u.CheckOrigin = h.checkOrigin
	raw, err := u.Upgrade(w, r, nil)
	if err != nil {
		return nil, err
	}
	c := &Conn{Conn: raw, send: make(chan []byte, 256)}
	h.register <- c
	go c.writePump()
	go c.readPump(h)
	return c, nil
}

func (h *Hub) checkOrigin(r *http.Request) bool {
	origin := trimASCIIWhitespace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}
	host := lowerASCII(parsed.Host)

	h.mu.RLock()
	defer h.mu.RUnlock()
	if len(h.allowedOrigins) == 0 {
		return bytes.EqualFold([]byte(host), []byte(r.Host))
	}
	_, ok := h.allowedOrigins[host]
	return ok
}

func normalizeOrigins(origins []string) map[string]struct{} {
	allowed := make(map[string]struct{}, len(origins))
	for _, origin := range origins {
		trimmed := trimASCIIWhitespace(origin)
		if trimmed == "" {
			continue
		}
		parsed, err := url.Parse(trimmed)
		if err != nil || parsed.Host == "" {
			continue
		}
		allowed[lowerASCII(parsed.Host)] = struct{}{}
	}
	return allowed
}

func lowerASCII(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}

func trimASCIIWhitespace(s string) string {
	start := 0
	end := len(s)
	for start < end {
		c := s[start]
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			break
		}
		start++
	}
	for end > start {
		c := s[end-1]
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			break
		}
		end--
	}
	return s[start:end]
}

func (c *Conn) readPump(h *Hub) {
	defer func() { h.unregister <- c; c.Conn.Close() }()
	for {
		_, _, err := c.Conn.ReadMessage()
		if err != nil {
			return
		}
	}
}

func (c *Conn) writePump() {
	defer c.Conn.Close()
	for msg := range c.send {
		if err := c.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			return
		}
	}
}
