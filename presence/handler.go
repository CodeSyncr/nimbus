package presence

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// ---------------------------------------------------------------------------
// WebSocket Handler
// ---------------------------------------------------------------------------

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// CheckOrigin is set dynamically per request in HandleWebSocket
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
	u := upgrader
	u.CheckOrigin = h.checkOrigin
	conn, err := u.Upgrade(w, r, nil)
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
