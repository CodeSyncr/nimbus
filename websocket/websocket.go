package websocket

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/CodeSyncr/nimbus/redis"
	"github.com/gorilla/websocket"
)

const DefaultRedisTopic = "nimbus:websocket:broadcast"

var ErrClosed = errors.New("websocket: hub closed")
var ErrBackpressure = errors.New("websocket: broadcast queue full")
var ErrMessageTooLarge = errors.New("websocket: message exceeds configured limit")

// HubOptions bounds per-connection and hub resources. Nonpositive values use
// safe defaults. Configure before serving; options are immutable after creation.
type HubOptions struct {
	MaxMessageBytes int64         // default 64 KiB (inbound and broadcasts)
	MaxConnections  int           // default 1024, including pending upgrades
	SendBuffer      int           // default 32 messages per client
	WriteTimeout    time.Duration // default 10s
	PongTimeout     time.Duration // default 60s
	PingInterval    time.Duration // default 25s, always below PongTimeout
	PublishTimeout  time.Duration // default 5s
}

type Hub struct {
	mu             sync.RWMutex
	clients        map[*Conn]struct{}
	broadcast      chan []byte
	allowedOrigins map[string]struct{}
	options        HubOptions
	pending        int
	closed         bool
	ctx            context.Context
	cancel         context.CancelFunc
	runOnce        sync.Once
	closeOnce      sync.Once
	workers        sync.WaitGroup
	closeErr       error
	redis          *redis.Client
	topic          string
	pubsub         *redis.PubSub
}

type Conn struct {
	*websocket.Conn
	send    chan []byte
	done    chan struct{}
	once    sync.Once
	options HubOptions
}

func NewHub() *Hub { return NewHubWithOptions(HubOptions{}) }
func NewHubWithOptions(o HubOptions) *Hub {
	if o.MaxMessageBytes <= 0 {
		o.MaxMessageBytes = 64 << 10
	}
	if o.MaxConnections <= 0 {
		o.MaxConnections = 1024
	}
	if o.SendBuffer <= 0 {
		o.SendBuffer = 32
	}
	if o.WriteTimeout <= 0 {
		o.WriteTimeout = 10 * time.Second
	}
	if o.PongTimeout <= 0 {
		o.PongTimeout = time.Minute
	}
	if o.PingInterval <= 0 {
		o.PingInterval = 25 * time.Second
	}
	if o.PingInterval >= o.PongTimeout {
		o.PingInterval = max(time.Nanosecond, o.PongTimeout/2)
	}
	if o.PublishTimeout <= 0 {
		o.PublishTimeout = 5 * time.Second
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Hub{clients: make(map[*Conn]struct{}), broadcast: make(chan []byte, 256), allowedOrigins: make(map[string]struct{}), options: o, ctx: ctx, cancel: cancel}
}
func (h *Hub) SetAllowedOrigins(origins []string) {
	h.mu.Lock()
	h.allowedOrigins = normalizeOrigins(origins)
	h.mu.Unlock()
}

// UseRedis subscribes once, before Run. Forwarding and subscription stop at Close.
func (h *Hub) UseRedis(client *redis.Client, topic string) error {
	if client == nil {
		return errors.New("websocket: nil Redis client")
	}
	if topic == "" {
		topic = DefaultRedisTopic
	}
	ctx, cancel := context.WithTimeout(h.ctx, h.options.PublishTimeout)
	defer cancel()
	ps := client.Subscribe(h.ctx, topic)
	if _, err := ps.Receive(ctx); err != nil {
		ps.Close()
		return err
	}
	h.mu.Lock()
	if h.closed || h.pubsub != nil {
		closed := h.closed
		h.mu.Unlock()
		ps.Close()
		if closed {
			return ErrClosed
		}
		return errors.New("websocket: Redis already configured")
	}
	h.redis, h.topic, h.pubsub = client, topic, ps
	h.workers.Add(1)
	h.mu.Unlock()
	go func() {
		defer h.workers.Done()
		ch := ps.Channel()
		for {
			select {
			case <-h.ctx.Done():
				return
			case message, ok := <-ch:
				if !ok {
					return
				}
				if int64(len(message.Payload)) > h.options.MaxMessageBytes {
					continue
				}
				select {
				case h.broadcast <- []byte(message.Payload):
				case <-h.ctx.Done():
					return
				}
			}
		}
	}()
	return nil
}

// Close stops admission, closes clients/subscription and waits for owned pumps.
// It is idempotent and also works when Run was never called.
func (h *Hub) Close() error {
	h.closeOnce.Do(func() {
		h.mu.Lock()
		h.closed = true
		h.cancel()
		ps := h.pubsub
		for c := range h.clients {
			c.stop()
			delete(h.clients, c)
		}
		h.mu.Unlock()
		if ps != nil {
			h.closeErr = ps.Close()
		}
		h.workers.Wait()
		// Release queued messages retained by a closed hub even if Run never started.
		for {
			select {
			case <-h.broadcast:
			default:
				return
			}
		}
	})
	return h.closeErr
}

// Run delivers broadcasts until Close. Calling it more than once shares one loop.
func (h *Hub) Run() {
	h.runOnce.Do(func() {
		h.mu.Lock()
		if h.closed {
			h.mu.Unlock()
			return
		}
		h.workers.Add(1)
		h.mu.Unlock()
		defer h.workers.Done()
		for {
			select {
			case <-h.ctx.Done():
				return
			case msg := <-h.broadcast:
				h.mu.Lock()
				for c := range h.clients {
					select {
					case c.send <- msg:
					default:
						c.stop()
						delete(h.clients, c)
					}
				}
				h.mu.Unlock()
			}
		}
	})
}

// Broadcast retains the original convenience API. Use BroadcastContext when the
// caller needs to handle cancellation, oversize or saturation errors explicitly.
func (h *Hub) Broadcast(msg []byte) {
	if err := h.BroadcastContext(context.Background(), msg); err != nil {
		log.Printf("[websocket] broadcast: %v", err)
	}
}
func (h *Hub) BroadcastContext(ctx context.Context, msg []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if int64(len(msg)) > h.options.MaxMessageBytes {
		return ErrMessageTooLarge
	}
	h.mu.RLock()
	if h.closed {
		h.mu.RUnlock()
		return ErrClosed
	}
	client, topic := h.redis, h.topic
	if client != nil {
		h.mu.RUnlock()
		publishCtx, cancel := context.WithTimeout(ctx, h.options.PublishTimeout)
		defer cancel()
		stop := context.AfterFunc(h.ctx, cancel)
		defer stop()
		return client.Publish(publishCtx, topic, msg).Err()
	}
	defer h.mu.RUnlock()
	snapshot := append([]byte(nil), msg...)
	select {
	case h.broadcast <- snapshot:
		return nil
	default:
		return ErrBackpressure
	}
}

func (h *Hub) Len() int { h.mu.RLock(); defer h.mu.RUnlock(); return len(h.clients) }

func (h *Hub) Upgrade(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	h.mu.Lock()
	if h.closed || len(h.clients)+h.pending >= h.options.MaxConnections {
		err := ErrBackpressure
		if h.closed {
			err = ErrClosed
		}
		h.mu.Unlock()
		http.Error(w, "WebSocket unavailable", http.StatusServiceUnavailable)
		return nil, err
	}
	h.pending++
	h.mu.Unlock()
	u := websocket.Upgrader{ReadBufferSize: 1024, WriteBufferSize: 1024, HandshakeTimeout: h.options.WriteTimeout, CheckOrigin: h.checkOrigin}
	raw, err := u.Upgrade(w, r, nil)
	h.mu.Lock()
	h.pending--
	if err != nil {
		h.mu.Unlock()
		return nil, err
	}
	if h.closed {
		h.mu.Unlock()
		raw.Close()
		return nil, ErrClosed
	}
	c := &Conn{Conn: raw, send: make(chan []byte, h.options.SendBuffer), done: make(chan struct{}), options: h.options}
	raw.SetReadLimit(h.options.MaxMessageBytes)
	_ = raw.SetReadDeadline(time.Now().Add(h.options.PongTimeout))
	raw.SetPongHandler(func(string) error { return raw.SetReadDeadline(time.Now().Add(h.options.PongTimeout)) })
	h.clients[c] = struct{}{}
	h.workers.Add(2)
	h.mu.Unlock()
	go func() { defer h.workers.Done(); defer h.remove(c); c.readPump() }()
	go func() { defer h.workers.Done(); defer h.remove(c); c.writePump() }()
	return c, nil
}
func (h *Hub) remove(c *Conn) { h.mu.Lock(); delete(h.clients, c); h.mu.Unlock(); c.stop() }
func (c *Conn) stop()         { c.once.Do(func() { close(c.done); c.Conn.Close() }) }

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

func (c *Conn) readPump() {
	for {
		_, reader, err := c.Conn.NextReader()
		if err != nil {
			return
		}
		if _, err := io.Copy(io.Discard, reader); err != nil {
			return
		}
	}
}
func (c *Conn) writePump() {
	ticker := time.NewTicker(c.options.PingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.done:
			return
		case msg := <-c.send:
			if err := c.Conn.SetWriteDeadline(time.Now().Add(c.options.WriteTimeout)); err != nil {
				return
			}
			if err := c.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			if err := c.Conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(c.options.WriteTimeout)); err != nil {
				return
			}
		}
	}
}
