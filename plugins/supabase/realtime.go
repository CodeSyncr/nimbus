package supabase

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// RealtimeEvent is the type of database change event.
type RealtimeEvent string

const (
	EventInsert RealtimeEvent = "INSERT"
	EventUpdate RealtimeEvent = "UPDATE"
	EventDelete RealtimeEvent = "DELETE"
	EventAll    RealtimeEvent = "*"
)

// ChangePayload is delivered to listeners on Postgres change events.
type ChangePayload struct {
	Schema    string         `json:"schema"`
	Table     string         `json:"table"`
	Type      RealtimeEvent  `json:"type"`
	Record    map[string]any `json:"record"`
	OldRecord map[string]any `json:"old_record"`
	Columns   []ColumnInfo   `json:"columns"`
}

// ColumnInfo describes a single column in a change payload.
type ColumnInfo struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// BroadcastPayload is delivered for broadcast events.
type BroadcastPayload struct {
	Event   string         `json:"event"`
	Payload map[string]any `json:"payload"`
}

// PresencePayload represents a presence state change.
type PresencePayload struct {
	Key      string         `json:"key"`
	NewState map[string]any `json:"new_state"`
	OldState map[string]any `json:"old_state"`
}

// ChangeHandler is called when a Postgres change event matches.
type ChangeHandler func(payload ChangePayload)

// BroadcastHandler is called on broadcast messages.
type BroadcastHandler func(payload BroadcastPayload)

// PresenceHandler is called on presence sync/join/leave.
type PresenceHandler func(payload PresencePayload)

// RealtimeClient manages WebSocket channels to Supabase Realtime.
type RealtimeClient struct {
	client   *Client
	mu       sync.RWMutex
	writeMu  sync.Mutex // protects WebSocket writes (gorilla/websocket requires exclusive writer)
	channels map[string]*Channel
	conn     *websocket.Conn
	done     chan struct{}
	closeOnce sync.Once

	// Reconnection settings.
	maxRetries int           // 0 = unlimited. Default: 0.
	baseDelay  time.Duration // initial backoff delay. Default: 1s.
	maxDelay   time.Duration // max backoff cap. Default: 30s.

	// OnError is called when a connection or reconnection error occurs.
	// Set this before calling Connect to observe errors.
	OnError func(err error)
}

// Channel represents a subscription to a Supabase Realtime channel.
type Channel struct {
	realtime          *RealtimeClient
	topic             string
	mu                sync.Mutex
	changeHandlers    map[RealtimeEvent][]ChangeHandler
	broadcastHandlers map[string][]BroadcastHandler
	presenceHandlers  []PresenceHandler
	joined            bool
}

// realtimeMessage is the wire format for Supabase Realtime WebSocket messages.
type realtimeMessage struct {
	Topic   string         `json:"topic"`
	Event   string         `json:"event"`
	Payload map[string]any `json:"payload"`
	Ref     string         `json:"ref,omitempty"`
}

// Connect establishes the WebSocket connection to Supabase Realtime.
func (r *RealtimeClient) Connect() error {
	if r.client == nil || r.client.url == "" {
		return fmt.Errorf("supabase: realtime: client not configured")
	}

	// Set reconnection defaults if not configured.
	if r.baseDelay == 0 {
		r.baseDelay = 1 * time.Second
	}
	if r.maxDelay == 0 {
		r.maxDelay = 30 * time.Second
	}

	if err := r.dial(); err != nil {
		return err
	}

	r.mu.Lock()
	r.done = make(chan struct{})
	r.closeOnce = sync.Once{}
	r.mu.Unlock()

	go r.readLoop()
	go r.heartbeat()
	return nil
}

// dial creates the underlying WebSocket connection.
func (r *RealtimeClient) dial() error {
	wsURL := r.client.url
	if len(wsURL) > 8 && wsURL[:8] == "https://" {
		wsURL = "wss://" + wsURL[8:]
	} else if len(wsURL) > 7 && wsURL[:7] == "http://" {
		wsURL = "ws://" + wsURL[7:]
	}
	wsURL += "/realtime/v1/websocket?apikey=" + r.client.apiKey() + "&vsn=1.0.0"

	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("supabase: realtime connect: %w", err)
	}

	r.mu.Lock()
	r.conn = conn
	r.mu.Unlock()
	return nil
}

// Channel returns (or creates) a channel for the given topic.
func (r *RealtimeClient) Channel(topic string) *Channel {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ch, ok := r.channels[topic]; ok {
		return ch
	}
	ch := &Channel{
		realtime:          r,
		topic:             topic,
		changeHandlers:    make(map[RealtimeEvent][]ChangeHandler),
		broadcastHandlers: make(map[string][]BroadcastHandler),
	}
	r.channels[topic] = ch
	return ch
}

// Close shuts down the realtime connection. Safe to call multiple times.
func (r *RealtimeClient) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()

	// sync.Once ensures we never double-close the done channel.
	r.closeOnce.Do(func() {
		if r.done != nil {
			close(r.done)
		}
	})

	if r.conn != nil {
		r.writeMu.Lock()
		_ = r.conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		r.writeMu.Unlock()
		_ = r.conn.Close()
		r.conn = nil
	}
}

func (r *RealtimeClient) readLoop() {
	defer func() {
		r.mu.Lock()
		if r.conn != nil {
			_ = r.conn.Close()
			r.conn = nil
		}
		r.mu.Unlock()
	}()

	for {
		select {
		case <-r.done:
			return
		default:
		}

		r.mu.RLock()
		conn := r.conn
		r.mu.RUnlock()
		if conn == nil {
			return
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			// Check if this is an intentional close.
			select {
			case <-r.done:
				return
			default:
			}
			// Connection lost — attempt to reconnect.
			r.reconnect()
			return
		}

		var msg realtimeMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			continue
		}
		r.dispatch(msg)
	}
}

// reconnect attempts to re-establish the WebSocket with exponential backoff.
func (r *RealtimeClient) reconnect() {
	delay := r.baseDelay
	attempts := 0

	for {
		select {
		case <-r.done:
			return
		default:
		}

		if r.maxRetries > 0 && attempts >= r.maxRetries {
			if r.OnError != nil {
				r.OnError(fmt.Errorf("supabase: realtime: max reconnect attempts (%d) reached", r.maxRetries))
			}
			return
		}

		attempts++
		time.Sleep(delay)

		if err := r.dial(); err != nil {
			if r.OnError != nil {
				r.OnError(fmt.Errorf("supabase: realtime reconnect attempt %d: %w", attempts, err))
			}
			// Exponential backoff with cap.
			delay *= 2
			if delay > r.maxDelay {
				delay = r.maxDelay
			}
			continue
		}

		// Successfully reconnected — re-subscribe all joined channels.
		r.resubscribeAll()

		// Restart read and heartbeat loops.
		go r.readLoop()
		go r.heartbeat()
		return
	}
}

// resubscribeAll re-joins all channels that were previously subscribed.
func (r *RealtimeClient) resubscribeAll() {
	r.mu.RLock()
	channels := make([]*Channel, 0, len(r.channels))
	for _, ch := range r.channels {
		channels = append(channels, ch)
	}
	r.mu.RUnlock()

	for _, ch := range channels {
		ch.mu.Lock()
		wasJoined := ch.joined
		ch.joined = false // reset so Subscribe() sends phx_join again
		ch.mu.Unlock()

		if wasJoined {
			if err := ch.Subscribe(); err != nil && r.OnError != nil {
				r.OnError(fmt.Errorf("supabase: realtime resubscribe %s: %w", ch.topic, err))
			}
		}
	}
}

func (r *RealtimeClient) heartbeat() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.done:
			return
		case <-ticker.C:
			r.mu.RLock()
			conn := r.conn
			r.mu.RUnlock()
			if conn == nil {
				return
			}
			b, _ := json.Marshal(realtimeMessage{
				Topic: "phoenix", Event: "heartbeat", Payload: map[string]any{},
			})
			r.writeMu.Lock()
			_ = conn.WriteMessage(websocket.TextMessage, b)
			r.writeMu.Unlock()
		}
	}
}

func (r *RealtimeClient) dispatch(msg realtimeMessage) {
	r.mu.RLock()
	ch, ok := r.channels[msg.Topic]
	r.mu.RUnlock()
	if !ok {
		return
	}
	switch msg.Event {
	case "postgres_changes":
		ch.dispatchChange(msg.Payload)
	case "broadcast":
		ch.dispatchBroadcast(msg.Payload)
	case "presence_diff", "presence_state":
		ch.dispatchPresence(msg.Payload)
	}
}

func (r *RealtimeClient) send(msg realtimeMessage) error {
	r.mu.RLock()
	conn := r.conn
	r.mu.RUnlock()
	if conn == nil {
		return fmt.Errorf("supabase: realtime: not connected")
	}
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	return conn.WriteMessage(websocket.TextMessage, b)
}

// ── Channel Methods ─────────────────────────────────────────────

// OnPostgresChanges registers a handler for Postgres change events.
func (ch *Channel) OnPostgresChanges(event RealtimeEvent, handler ChangeHandler) *Channel {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	ch.changeHandlers[event] = append(ch.changeHandlers[event], handler)
	return ch
}

// OnBroadcast registers a handler for broadcast events.
func (ch *Channel) OnBroadcast(event string, handler BroadcastHandler) *Channel {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	ch.broadcastHandlers[event] = append(ch.broadcastHandlers[event], handler)
	return ch
}

// OnPresence registers a handler for presence events.
func (ch *Channel) OnPresence(handler PresenceHandler) *Channel {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	ch.presenceHandlers = append(ch.presenceHandlers, handler)
	return ch
}

// Subscribe joins the channel.
func (ch *Channel) Subscribe() error {
	ch.mu.Lock()
	if ch.joined {
		ch.mu.Unlock()
		return nil
	}
	ch.mu.Unlock()
	msg := realtimeMessage{
		Topic: ch.topic,
		Event: "phx_join",
		Payload: map[string]any{
			"config": map[string]any{
				"postgres_changes": []map[string]any{
					{"event": "*", "schema": "public"},
				},
			},
		},
	}
	if err := ch.realtime.send(msg); err != nil {
		return err
	}
	ch.mu.Lock()
	ch.joined = true
	ch.mu.Unlock()
	return nil
}

// Unsubscribe leaves the channel.
func (ch *Channel) Unsubscribe() error {
	ch.mu.Lock()
	if !ch.joined {
		ch.mu.Unlock()
		return nil
	}
	ch.mu.Unlock()
	if err := ch.realtime.send(realtimeMessage{
		Topic: ch.topic, Event: "phx_leave", Payload: map[string]any{},
	}); err != nil {
		return err
	}
	ch.mu.Lock()
	ch.joined = false
	ch.mu.Unlock()
	return nil
}

// Broadcast sends a broadcast message to the channel.
func (ch *Channel) Broadcast(event string, payload map[string]any) error {
	return ch.realtime.send(realtimeMessage{
		Topic: ch.topic,
		Event: "broadcast",
		Payload: map[string]any{
			"event": event, "payload": payload, "type": "broadcast",
		},
	})
}

// Track sends a presence track message.
func (ch *Channel) Track(state map[string]any) error {
	return ch.realtime.send(realtimeMessage{
		Topic: ch.topic,
		Event: "presence",
		Payload: map[string]any{
			"type": "track", "state": state,
		},
	})
}

func (ch *Channel) dispatchChange(raw map[string]any) {
	b, err := json.Marshal(raw)
	if err != nil {
		return
	}
	var payload ChangePayload
	if err := json.Unmarshal(b, &payload); err != nil {
		return
	}
	ch.mu.Lock()
	var handlers []ChangeHandler
	if fns, ok := ch.changeHandlers[payload.Type]; ok {
		handlers = append(handlers, fns...)
	}
	if fns, ok := ch.changeHandlers[EventAll]; ok {
		handlers = append(handlers, fns...)
	}
	ch.mu.Unlock()
	for _, fn := range handlers {
		fn(payload)
	}
}

func (ch *Channel) dispatchBroadcast(raw map[string]any) {
	b, err := json.Marshal(raw)
	if err != nil {
		return
	}
	var payload BroadcastPayload
	if err := json.Unmarshal(b, &payload); err != nil {
		return
	}
	ch.mu.Lock()
	handlers := make([]BroadcastHandler, 0)
	if fns, ok := ch.broadcastHandlers[payload.Event]; ok {
		handlers = append(handlers, fns...)
	}
	ch.mu.Unlock()
	for _, fn := range handlers {
		fn(payload)
	}
}

func (ch *Channel) dispatchPresence(raw map[string]any) {
	b, err := json.Marshal(raw)
	if err != nil {
		return
	}
	var payload PresencePayload
	if err := json.Unmarshal(b, &payload); err != nil {
		return
	}
	ch.mu.Lock()
	handlers := make([]PresenceHandler, len(ch.presenceHandlers))
	copy(handlers, ch.presenceHandlers)
	ch.mu.Unlock()
	for _, fn := range handlers {
		fn(payload)
	}
}
