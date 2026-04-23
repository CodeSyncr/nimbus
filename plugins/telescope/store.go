package telescope

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// EntryType identifies the kind of telescope entry.
type EntryType string

const (
	EntryRequest      EntryType = "request"
	EntryCommand      EntryType = "command"
	EntrySchedule     EntryType = "schedule"
	EntryJob          EntryType = "job"
	EntryBatch        EntryType = "batch"
	EntryCache        EntryType = "cache"
	EntryDump         EntryType = "dump"
	EntryEvent        EntryType = "event"
	EntryException    EntryType = "exception"
	EntryGate         EntryType = "gate"
	EntryHTTPClient   EntryType = "http_client"
	EntryLog          EntryType = "log"
	EntryMail         EntryType = "mail"
	EntryModel        EntryType = "model"
	EntryNotification EntryType = "notification"
	EntryQuery        EntryType = "query"
	EntryRedis        EntryType = "redis"
	EntryView         EntryType = "view"
)

// Entry represents a single telescope record.
type Entry struct {
	ID        string         `json:"id"`
	Type      EntryType      `json:"type"`
	Content   map[string]any `json:"content"`
	Tags      []string       `json:"tags,omitempty"`
	BatchID   string         `json:"batch_id,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

// RequestContent holds request/response data.
type RequestContent struct {
	Method         string            `json:"method"`
	Path           string            `json:"path"`
	Query          string            `json:"query"`
	Headers        map[string]string `json:"headers,omitempty"`
	ResponseStatus int               `json:"response_status"`
	Duration       time.Duration     `json:"duration_ms"`
	Memory         int64             `json:"memory,omitempty"`
}

// ExceptionContent holds exception/panic data.
type ExceptionContent struct {
	Class   string `json:"class"`
	Message string `json:"message"`
	File    string `json:"file,omitempty"`
	Line    int    `json:"line,omitempty"`
	Trace   string `json:"trace,omitempty"`
}

// QueryContent holds database query data.
type QueryContent struct {
	SQL        string        `json:"sql"`
	Bindings   []any         `json:"bindings,omitempty"`
	Duration   time.Duration `json:"duration_ms"`
	Connection string        `json:"connection,omitempty"`
}

// LogContent holds log entry data.
type LogContent struct {
	Level   string         `json:"level"`
	Message string         `json:"message"`
	Context map[string]any `json:"context,omitempty"`
}

// Store holds telescope entries in a ring buffer.
type Store struct {
	mu      sync.RWMutex
	entries []*Entry
	max     int
	next    int
	persist persistBackend
	enabled map[EntryType]bool // nil means all enabled
}

type persistBackend interface {
	Latest(limit int) ([]*Entry, error)
	Insert(entry *Entry) error
	Clear() error
}

type persistPruner interface {
	PruneBefore(t time.Time) error
}

// NewStore creates a store with max entries (ring buffer).
func NewStore(max int) *Store {
	if max < 10 {
		max = 10
	}
	return &Store{
		entries: make([]*Entry, 0, max),
		max:     max,
	}
}

func (s *Store) SetPersistBackend(p persistBackend) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.persist = p
}

// EnableOnly enables recording only for the given entry types.
// If no types are provided, all entry types are enabled.
func (s *Store) EnableOnly(types ...EntryType) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(types) == 0 {
		s.enabled = nil
		return
	}
	m := make(map[EntryType]bool, len(types))
	for _, t := range types {
		if t != "" {
			m[t] = true
		}
	}
	s.enabled = m
}

func (s *Store) isEnabled(t EntryType) bool {
	if t == "" {
		return true
	}
	if s.enabled == nil {
		return true
	}
	return s.enabled[t]
}

func (s *Store) LoadLatestFromBackend(limit int) {
	s.mu.RLock()
	p := s.persist
	s.mu.RUnlock()
	if p == nil {
		return
	}
	if limit <= 0 {
		limit = s.max
	}
	entries, err := p.Latest(limit)
	if err != nil {
		return
	}
	// entries should already be newest-first. Convert to oldest-first for ring buffer.
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	s.mu.Lock()
	s.entries = append(s.entries[:0], entries...)
	s.next = 0
	s.mu.Unlock()
}

// Record adds an entry to the store.
func (s *Store) Record(entry *Entry) {
	s.mu.Lock()
	p := s.persist
	enabled := true
	if entry != nil {
		enabled = s.isEnabled(entry.Type)
	}
	s.mu.Unlock()
	if !enabled {
		return
	}

	if entry.ID == "" {
		entry.ID = "t" + uuid.New().String()
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}

	// Persist first so we don't lose entries on crash (best-effort).
	if p != nil {
		_ = p.Insert(entry)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.entries) < s.max {
		s.entries = append(s.entries, entry)
	} else {
		s.entries[s.next] = entry
		s.next = (s.next + 1) % s.max
	}
}

// Entries returns entries filtered by type, newest first.
func (s *Store) Entries(typ EntryType, limit int) []*Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 {
		limit = 50
	}
	var out []*Entry
	n := len(s.entries)
	for i := n - 1; i >= 0 && len(out) < limit; i-- {
		e := s.entries[i]
		if e == nil {
			continue
		}
		if typ == "" || e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

// All returns all entries, newest first.
func (s *Store) All(limit int) []*Entry {
	return s.Entries("", limit)
}

// Get returns an entry by ID.
func (s *Store) Get(id string) *Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, e := range s.entries {
		if e != nil && e.ID == id {
			return e
		}
	}
	return nil
}

// Clear removes all entries.
func (s *Store) Clear() {
	s.mu.Lock()
	p := s.persist
	s.entries = s.entries[:0]
	s.next = 0
	s.mu.Unlock()
	if p != nil {
		_ = p.Clear()
	}
}

func (s *Store) PruneBefore(t time.Time) {
	s.mu.RLock()
	p := s.persist
	s.mu.RUnlock()
	if p == nil {
		return
	}
	if pr, ok := p.(persistPruner); ok {
		_ = pr.PruneBefore(t)
	}
}

func intEnv(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func jsonString(v any) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}
