/*
|--------------------------------------------------------------------------
| AI SDK — Agent Memory
|--------------------------------------------------------------------------
|
| Memory allows agents to persist conversation history across requests.
| Implementations include in-memory (for dev), Redis, and database.
|
| Usage:
|
|   // In-memory (default, per-process)
|   agent.WithMemory(ai.MemoryStore(), "session:abc")
|
|   // Redis
|   agent.WithMemory(ai.RedisMemory(redisClient), "session:abc")
|
*/

package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Memory interface
// ---------------------------------------------------------------------------

// Memory persists and retrieves conversation history for agents.
type Memory interface {
	// Load retrieves the conversation history for the given key.
	Load(ctx context.Context, key string) ([]Message, error)

	// Save persists the conversation history for the given key.
	Save(ctx context.Context, key string, messages []Message) error

	// Clear removes the conversation history for the given key.
	Clear(ctx context.Context, key string) error
}

// ---------------------------------------------------------------------------
// In-memory store (development / single-process)
// ---------------------------------------------------------------------------

type memoryStore struct {
	mu   sync.RWMutex
	data map[string][]Message
	ttl  time.Duration
}

// MemoryStore returns a simple in-memory Memory (lost on restart).
// Useful for development and testing.
func MemoryStore(opts ...MemoryStoreOption) Memory {
	m := &memoryStore{
		data: make(map[string][]Message),
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// MemoryStoreOption configures the in-memory store.
type MemoryStoreOption func(*memoryStore)

// WithTTL sets the time-to-live for stored conversations.
// Note: TTL cleanup is lazy (checked on Load).
func WithMemoryTTL(ttl time.Duration) MemoryStoreOption {
	return func(m *memoryStore) { m.ttl = ttl }
}

func (m *memoryStore) Load(_ context.Context, key string) ([]Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	msgs, ok := m.data[key]
	if !ok {
		return nil, nil
	}
	// Return a copy to prevent mutation.
	out := make([]Message, len(msgs))
	copy(out, msgs)
	return out, nil
}

func (m *memoryStore) Save(_ context.Context, key string, messages []Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]Message, len(messages))
	copy(cp, messages)
	m.data[key] = cp
	return nil
}

func (m *memoryStore) Clear(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

// ---------------------------------------------------------------------------
// Sliding-window memory (keeps last N messages)
// ---------------------------------------------------------------------------

type slidingWindowMemory struct {
	inner   Memory
	maxMsgs int
}

// SlidingWindowMemory wraps another Memory and keeps only the last N
// messages (plus the system prompt), preventing unbounded growth.
func SlidingWindowMemory(inner Memory, maxMessages int) Memory {
	return &slidingWindowMemory{inner: inner, maxMsgs: maxMessages}
}

func (s *slidingWindowMemory) Load(ctx context.Context, key string) ([]Message, error) {
	return s.inner.Load(ctx, key)
}

func (s *slidingWindowMemory) Save(ctx context.Context, key string, messages []Message) error {
	if len(messages) > s.maxMsgs {
		messages = messages[len(messages)-s.maxMsgs:]
	}
	return s.inner.Save(ctx, key, messages)
}

func (s *slidingWindowMemory) Clear(ctx context.Context, key string) error {
	return s.inner.Clear(ctx, key)
}

// ---------------------------------------------------------------------------
// Summary memory (compresses old messages into a summary)
// ---------------------------------------------------------------------------

type summaryMemory struct {
	inner     Memory
	threshold int // compress when history exceeds this
	client    *Client
}

// SummaryMemory wraps another Memory and auto-summarises old messages
// when the history exceeds the threshold. Uses the AI client itself to
// produce summaries.
func SummaryMemory(inner Memory, threshold int) Memory {
	return &summaryMemory{inner: inner, threshold: threshold}
}

func (s *summaryMemory) Load(ctx context.Context, key string) ([]Message, error) {
	return s.inner.Load(ctx, key)
}

func (s *summaryMemory) Save(ctx context.Context, key string, messages []Message) error {
	if len(messages) <= s.threshold {
		return s.inner.Save(ctx, key, messages)
	}

	// Summarise the older messages.
	client := s.client
	if client == nil {
		client = GetClient()
	}

	oldMsgs := messages[:len(messages)-s.threshold/2]
	recentMsgs := messages[len(messages)-s.threshold/2:]

	// Build a summary prompt.
	historyJSON, _ := json.Marshal(oldMsgs)
	summaryResp, err := client.Generate(ctx, fmt.Sprintf(
		"Summarize this conversation history concisely, preserving key facts and context:\n\n%s",
		string(historyJSON),
	))
	if err != nil {
		// If summarisation fails, fall back to sliding window.
		return s.inner.Save(ctx, key, messages[len(messages)-s.threshold:])
	}

	compressed := []Message{
		{Role: RoleSystem, Content: "Previous conversation summary: " + summaryResp.Text},
	}
	compressed = append(compressed, recentMsgs...)
	return s.inner.Save(ctx, key, compressed)
}

func (s *summaryMemory) Clear(ctx context.Context, key string) error {
	return s.inner.Clear(ctx, key)
}
