package telescope

import (
	"fmt"
	"sync"
	"time"
)

// EntryType identifies the kind of telescope entry.
type EntryType string

const (
	EntryRequest    EntryType = "request"
	EntryCommand    EntryType = "command"
	EntrySchedule   EntryType = "schedule"
	EntryJob        EntryType = "job"
	EntryBatch      EntryType = "batch"
	EntryCache      EntryType = "cache"
	EntryDump       EntryType = "dump"
	EntryEvent      EntryType = "event"
	EntryException  EntryType = "exception"
	EntryGate       EntryType = "gate"
	EntryHTTPClient EntryType = "http_client"
	EntryLog        EntryType = "log"
	EntryMail       EntryType = "mail"
	EntryModel      EntryType = "model"
	EntryNotification EntryType = "notification"
	EntryQuery      EntryType = "query"
	EntryRedis      EntryType = "redis"
	EntryView       EntryType = "view"
)

// Entry represents a single telescope record.
type Entry struct {
	ID        string                 `json:"id"`
	Type      EntryType              `json:"type"`
	Content   map[string]any         `json:"content"`
	Tags      []string               `json:"tags,omitempty"`
	BatchID   string                 `json:"batch_id,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
}

// RequestContent holds request/response data.
type RequestContent struct {
	Method        string            `json:"method"`
	Path          string            `json:"path"`
	Query         string            `json:"query"`
	Headers       map[string]string `json:"headers,omitempty"`
	ResponseStatus int               `json:"response_status"`
	Duration      time.Duration     `json:"duration_ms"`
	Memory        int64             `json:"memory,omitempty"`
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
	SQL      string        `json:"sql"`
	Bindings []any         `json:"bindings,omitempty"`
	Duration time.Duration `json:"duration_ms"`
	Connection string      `json:"connection,omitempty"`
}

// LogContent holds log entry data.
type LogContent struct {
	Level   string `json:"level"`
	Message string `json:"message"`
	Context map[string]any `json:"context,omitempty"`
}

// Store holds telescope entries in a ring buffer.
type Store struct {
	mu      sync.RWMutex
	entries []*Entry
	max     int
	next    int
	idSeq   int
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

// Record adds an entry to the store.
func (s *Store) Record(entry *Entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.idSeq++
	entry.ID = fmt.Sprintf("t%d", s.idSeq)
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}
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
	defer s.mu.Unlock()
	s.entries = s.entries[:0]
	s.next = 0
}
