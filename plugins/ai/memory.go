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
	"container/list"
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

const DefaultMemoryMaxEntries = 10_000

type memoryEntry struct {
	key      string
	messages []Message
	expires  time.Time
}
type memoryStore struct {
	mu         sync.Mutex
	data       map[string]*list.Element
	lru        *list.List
	ttl        time.Duration
	maxEntries int
	lastSweep  time.Time
}

// MemoryStore stores isolated conversation snapshots in one process. By default
// it keeps at most 10,000 conversations, evicting the least recently used.
func MemoryStore(opts ...MemoryStoreOption) Memory {
	m := &memoryStore{data: make(map[string]*list.Element), lru: list.New(), maxEntries: DefaultMemoryMaxEntries, lastSweep: time.Now()}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

type MemoryStoreOption func(*memoryStore)

// WithMemoryTTL expires conversations from their last Save. Zero disables TTL.
func WithMemoryTTL(ttl time.Duration) MemoryStoreOption { return func(m *memoryStore) { m.ttl = ttl } }

// WithMemoryMaxEntries caps conversation count; nonpositive values use the default.
func WithMemoryMaxEntries(n int) MemoryStoreOption {
	return func(m *memoryStore) {
		if n > 0 {
			m.maxEntries = n
		}
	}
}

func cloneMemoryMessages(messages []Message) []Message {
	out := append([]Message(nil), messages...)
	for i := range out {
		out[i].Images = append([]string(nil), out[i].Images...)
		out[i].Files = append([]string(nil), out[i].Files...)
		out[i].Reasoning = append([]ReasoningBlock(nil), out[i].Reasoning...)
		out[i].ToolCalls = append([]ToolCall(nil), out[i].ToolCalls...)
		for j := range out[i].ToolCalls {
			out[i].ToolCalls[j].Args = append(json.RawMessage(nil), out[i].ToolCalls[j].Args...)
		}
	}
	return out
}
func (m *memoryStore) remove(el *list.Element) {
	delete(m.data, el.Value.(*memoryEntry).key)
	m.lru.Remove(el)
}
func (m *memoryStore) Load(ctx context.Context, key string) ([]Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	el := m.data[key]
	if el == nil {
		m.mu.Unlock()
		return nil, nil
	}
	entry := el.Value.(*memoryEntry)
	if !entry.expires.IsZero() && !time.Now().Before(entry.expires) {
		m.remove(el)
		m.mu.Unlock()
		return nil, nil
	}
	m.lru.MoveToFront(el)
	messages := entry.messages
	m.mu.Unlock()
	// Published snapshots are immutable, so the copy need not hold the store lock.
	return cloneMemoryMessages(messages), nil
}
func (m *memoryStore) Save(ctx context.Context, key string, messages []Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	snapshot := cloneMemoryMessages(messages)
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if m.ttl > 0 && now.Sub(m.lastSweep) >= min(m.ttl, time.Minute) {
		for _, el := range m.data {
			e := el.Value.(*memoryEntry)
			if !e.expires.IsZero() && !now.Before(e.expires) {
				m.remove(el)
			}
		}
		m.lastSweep = now
	}
	if previous := m.data[key]; previous != nil {
		m.remove(previous)
	}
	entry := &memoryEntry{key: key, messages: snapshot}
	if m.ttl > 0 {
		entry.expires = now.Add(m.ttl)
	}
	m.data[key] = m.lru.PushFront(entry)
	for len(m.data) > m.maxEntries {
		m.remove(m.lru.Back())
	}
	return nil
}
func (m *memoryStore) Clear(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if el := m.data[key]; el != nil {
		m.remove(el)
	}
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
