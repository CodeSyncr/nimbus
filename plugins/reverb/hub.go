package reverb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/CodeSyncr/nimbus/redis"
	"github.com/gorilla/websocket"
)

// RedisFanoutChannel is the Redis Pub/Sub channel used to sync WebSocket
// broadcasts across Nimbus instances (set REVERB_REDIS_CHANNEL to override).
const defaultFanoutChannel = "nimbus:reverb:fanout"

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// clientEnvelope is sent to browsers.
type clientEnvelope struct {
	Event   string          `json:"event"`
	Channel string          `json:"channel,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// fanoutPayload is published on Redis so other app instances can deliver locally.
type fanoutPayload struct {
	Channel string          `json:"channel"`
	Data    json.RawMessage `json:"data"`
}

// clientCommand is parsed from the browser (text JSON frames).
type clientCommand struct {
	Action  string `json:"action"`
	Channel string `json:"channel"`
}

// Hub tracks WebSocket subscribers per logical channel.
type Hub struct {
	mu        sync.RWMutex
	byChannel map[string]map[*subscriber]struct{}
	redis     *redis.Client
	pubsub    *redis.PubSub
	cancel    context.CancelFunc
	origins   map[string]struct{}
	fanoutKey string
}

type subscriber struct {
	hub *Hub
	ws  *websocket.Conn
	mu  sync.Mutex
	ch  map[string]struct{}
}

func newSubscriber(h *Hub, c *websocket.Conn) *subscriber {
	return &subscriber{hub: h, ws: c, ch: make(map[string]struct{})}
}

func (s *subscriber) writeJSON(v any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ws.WriteJSON(v)
}

func newHub(r *redis.Client, origins []string, fanoutKey string) *Hub {
	if fanoutKey == "" {
		fanoutKey = defaultFanoutChannel
	}
	h := &Hub{
		byChannel: make(map[string]map[*subscriber]struct{}),
		redis:     r,
		origins:   normalizeOrigins(origins),
		fanoutKey: fanoutKey,
	}
	if r != nil {
		ctx, cancel := context.WithCancel(context.Background())
		h.cancel = cancel
		h.pubsub = r.Subscribe(ctx, fanoutKey)
		go h.redisLoop(ctx)
	}
	return h
}

func (h *Hub) shutdown() error {
	if h.cancel != nil {
		h.cancel()
	}
	if h.pubsub != nil {
		_ = h.pubsub.Close()
	}
	if h.redis != nil {
		_ = h.redis.Close()
	}
	return nil
}

func (h *Hub) redisLoop(ctx context.Context) {
	ch := h.pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			var fp fanoutPayload
			if err := json.Unmarshal([]byte(msg.Payload), &fp); err != nil {
				continue
			}
			if fp.Channel == "" {
				continue
			}
			h.deliver(fp.Channel, fp.Data)
		}
	}
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
	if len(h.origins) == 0 {
		return strings.EqualFold(host, r.Host)
	}
	_, ok := h.origins[host]
	return ok
}

func normalizeOrigins(origins []string) map[string]struct{} {
	m := make(map[string]struct{})
	for _, o := range origins {
		o = strings.TrimSpace(o)
		if o == "" {
			continue
		}
		parsed, err := url.Parse(o)
		if err != nil || parsed.Host == "" {
			continue
		}
		m[strings.ToLower(parsed.Host)] = struct{}{}
	}
	return m
}

func (h *Hub) subscribe(s *subscriber, channel string) {
	if channel == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.byChannel[channel] == nil {
		h.byChannel[channel] = make(map[*subscriber]struct{})
	}
	h.byChannel[channel][s] = struct{}{}
	s.ch[channel] = struct{}{}
}

func (h *Hub) unsubscribe(s *subscriber, channel string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if m, ok := h.byChannel[channel]; ok {
		delete(m, s)
		if len(m) == 0 {
			delete(h.byChannel, channel)
		}
	}
	delete(s.ch, channel)
}

func (h *Hub) removeSubscriber(s *subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range s.ch {
		if m, ok := h.byChannel[ch]; ok {
			delete(m, s)
			if len(m) == 0 {
				delete(h.byChannel, ch)
			}
		}
	}
	s.ch = make(map[string]struct{})
}

func (h *Hub) deliver(channel string, data json.RawMessage) {
	h.mu.RLock()
	m := h.byChannel[channel]
	list := make([]*subscriber, 0, len(m))
	for s := range m {
		list = append(list, s)
	}
	h.mu.RUnlock()

	env := clientEnvelope{Event: "message", Channel: channel, Data: data}
	for _, s := range list {
		_ = s.writeJSON(env)
	}
}

// Broadcast sends a JSON-serializable payload to every WebSocket subscribed to channel.
// With Redis configured, delivery happens via Pub/Sub so every instance (including this one)
// receives exactly one fanout per publish. Without Redis, delivery is local-only.
func (h *Hub) Broadcast(ctx context.Context, channel string, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if h.redis != nil {
		b, err := json.Marshal(fanoutPayload{Channel: channel, Data: raw})
		if err != nil {
			return err
		}
		return h.redis.Publish(ctx, h.fanoutKey, string(b)).Err()
	}
	h.deliver(channel, raw)
	return nil
}

func (h *Hub) serveWS(w http.ResponseWriter, r *http.Request) {
	u := upgrader
	u.CheckOrigin = h.checkOrigin
	conn, err := u.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	sub := newSubscriber(h, conn)
	_ = sub.writeJSON(clientEnvelope{Event: "connected"})
	go h.readPump(sub)
}

func (h *Hub) readPump(s *subscriber) {
	defer func() {
		h.removeSubscriber(s)
		_ = s.ws.Close()
	}()
	for {
		_, payload, err := s.ws.ReadMessage()
		if err != nil {
			return
		}
		var cmd clientCommand
		if err := json.Unmarshal(payload, &cmd); err != nil {
			continue
		}
		switch strings.ToLower(cmd.Action) {
		case "subscribe":
			h.subscribe(s, strings.TrimSpace(cmd.Channel))
			_ = s.writeJSON(clientEnvelope{Event: "subscribed", Channel: cmd.Channel})
		case "unsubscribe":
			h.unsubscribe(s, strings.TrimSpace(cmd.Channel))
			_ = s.writeJSON(clientEnvelope{Event: "unsubscribed", Channel: cmd.Channel})
		case "ping":
			_ = s.writeJSON(clientEnvelope{Event: "pong"})
		}
	}
}
