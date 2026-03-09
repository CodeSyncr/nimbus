/*
|--------------------------------------------------------------------------
| Transmit Store
|--------------------------------------------------------------------------
|
| Tracks SSE connections and channel subscriptions. Each client has a UID.
| Channels map to sets of UIDs. Broadcast sends to all subscribers.
|
*/

package transmit

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Client represents an SSE-connected client.
type Client struct {
	UID    string
	Events chan []byte
	Done   chan struct{}
}

// Store holds connections and channel subscriptions.
type Store struct {
	mu sync.RWMutex
	// uid -> client
	clients map[string]*Client
	// channel -> set of uid
	channels map[string]map[string]bool
}

// NewStore creates a new transmit store.
func NewStore() *Store {
	return &Store{
		clients:  make(map[string]*Client),
		channels: make(map[string]map[string]bool),
	}
}

// Connect adds a new client and returns its UID.
func (s *Store) Connect() (string, *Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	uid := uuid.New().String()
	c := &Client{
		UID:    uid,
		Events: make(chan []byte, 64),
		Done:   make(chan struct{}),
	}
	s.clients[uid] = c
	return uid, c
}

// Disconnect removes a client and unsubscribes from all channels.
func (s *Store) Disconnect(uid string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.clients[uid]; ok {
		close(c.Done)
		delete(s.clients, uid)
	}
	for _, subs := range s.channels {
		delete(subs, uid)
	}
}

// Subscribe adds uid to channel.
func (s *Store) Subscribe(channel, uid string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.channels[channel] == nil {
		s.channels[channel] = make(map[string]bool)
	}
	s.channels[channel][uid] = true
}

// Unsubscribe removes uid from channel.
func (s *Store) Unsubscribe(channel, uid string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if subs := s.channels[channel]; subs != nil {
		delete(subs, uid)
	}
}

// Subscribers returns UIDs subscribed to channel.
func (s *Store) Subscribers(channel string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	subs := s.channels[channel]
	if len(subs) == 0 {
		return nil
	}
	out := make([]string, 0, len(subs))
	for uid := range subs {
		if s.clients[uid] != nil {
			out = append(out, uid)
		}
	}
	return out
}

// SendToChannel sends payload to all subscribers of channel, optionally excluding UIDs.
// Emits OnBroadcast. Use DeliverToChannel when relaying from transport (no emit).
func (s *Store) SendToChannel(channel string, payload any, excludeUIDs ...string) {
	emitBroadcast(channel, payload)
	s.DeliverToChannel(channel, payload, excludeUIDs...)
}

// DeliverToChannel delivers to local subscribers without emitting OnBroadcast.
// Used by transport when relaying from other instances.
func (s *Store) DeliverToChannel(channel string, payload any, excludeUIDs ...string) {
	exclude := make(map[string]bool)
	for _, u := range excludeUIDs {
		exclude[u] = true
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	msg := append([]byte("data: "), data...)
	msg = append(msg, '\n', '\n')

	s.mu.RLock()
	subs := s.channels[channel]
	if subs == nil {
		s.mu.RUnlock()
		return
	}
	uids := make([]string, 0, len(subs))
	for uid := range subs {
		if !exclude[uid] {
			uids = append(uids, uid)
		}
	}
	s.mu.RUnlock()

	for _, uid := range uids {
		s.mu.RLock()
		c := s.clients[uid]
		s.mu.RUnlock()
		if c != nil {
			select {
			case c.Events <- msg:
			case <-time.After(2 * time.Second):
				// client slow, skip
			default:
				// buffer full, skip
			}
		}
	}
}
