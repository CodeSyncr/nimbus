package presence

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Client represents a single websocket connection. A user may have several
// (one per tab or device).
type Client struct {
	id      string
	user    *User
	conn    *websocket.Conn
	channel string
	send    chan []byte
	done    chan struct{}
	hub     *Hub
	mu      sync.Mutex
}

func newClient(h *Hub, user *User, conn *websocket.Conn, channel string) *Client {
	return &Client{
		id:      randomID(),
		user:    user,
		conn:    conn,
		channel: channel,
		send:    make(chan []byte, 256),
		done:    make(chan struct{}),
		hub:     h,
	}
}

// trySend queues a message without blocking; a client whose buffer is full
// misses it.
func (c *Client) trySend(data []byte) {
	select {
	case c.send <- data:
	default:
	}
}

// ---------------------------------------------------------------------------
// Client Read/Write Pumps
// ---------------------------------------------------------------------------

func (c *Client) readPump(ch *Channel) {
	defer func() {
		close(c.done)
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
			// Private message to every connection of one user, on any instance.
			if dataMap, ok := incoming.Data.(map[string]any); ok {
				targetID, _ := dataMap["to"].(string)
				if targetID != "" {
					c.hub.SendTo(ch.name, Event{
						Type:    "whisper",
						Channel: ch.name,
						User:    c.user,
						Data:    dataMap["message"],
					}, targetID)
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
		case <-c.done:
			return
		case msg := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(c.hub.config.WriteTimeout))
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
