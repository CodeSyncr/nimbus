package websocket

import (
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// Hub holds connected clients and broadcasts (plan: realtime chat, notifications).
type Hub struct {
	mu         sync.RWMutex
	clients    map[*Conn]struct{}
	broadcast  chan []byte
	register   chan *Conn
	unregister chan *Conn
	allowedOrigins map[string]struct{}
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

// Broadcast sends a message to all connected clients.
func (h *Hub) Broadcast(msg []byte) {
	h.broadcast <- msg
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
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}
	host := strings.ToLower(parsed.Host)

	h.mu.RLock()
	defer h.mu.RUnlock()
	if len(h.allowedOrigins) == 0 {
		return strings.EqualFold(host, r.Host)
	}
	_, ok := h.allowedOrigins[host]
	return ok
}

func normalizeOrigins(origins []string) map[string]struct{} {
	allowed := make(map[string]struct{}, len(origins))
	for _, origin := range origins {
		trimmed := strings.TrimSpace(origin)
		if trimmed == "" {
			continue
		}
		parsed, err := url.Parse(trimmed)
		if err != nil || parsed.Host == "" {
			continue
		}
		allowed[strings.ToLower(parsed.Host)] = struct{}{}
	}
	return allowed
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
