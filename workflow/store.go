package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Store Interface
// ---------------------------------------------------------------------------

// SignalStore is implemented by stores that keep signals for waiting steps,
// so Engine.Signal works whichever instance receives it and even before the
// step starts waiting. Signals for one run and event are delivered in order.
type SignalStore interface {
	PushSignal(ctx context.Context, runID, event string, data Payload) error
	// PopSignal removes and returns the oldest signal, if any.
	PopSignal(ctx context.Context, runID, event string) (Payload, bool, error)
}

// Store persists workflow run state.
type Store interface {
	Save(ctx context.Context, run *RunInstance) error
	Load(ctx context.Context, id string) (*RunInstance, error)
	List(ctx context.Context, workflow string, limit int) ([]*RunInstance, error)
	Delete(ctx context.Context, id string) error
}

// ---------------------------------------------------------------------------
// Memory Store
// ---------------------------------------------------------------------------

// MemoryStore provides an in-memory Store implementation.
type MemoryStore struct {
	mu      sync.RWMutex
	runs    map[string]*RunInstance
	leases  memoryLeases
	signals map[string][]Payload // runID:event -> queued signals
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{runs: make(map[string]*RunInstance)}
}

func (s *MemoryStore) Save(_ context.Context, run *RunInstance) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Deep copy to avoid mutation issues
	s.runs[run.ID] = cloneRun(run)
	return nil
}

func (s *MemoryStore) Load(_ context.Context, id string) (*RunInstance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	run, ok := s.runs[id]
	if !ok {
		return nil, fmt.Errorf("workflow run %q not found", id)
	}
	return cloneRun(run), nil
}

func cloneRun(run *RunInstance) *RunInstance {
	data, _ := json.Marshal(run)
	var cp RunInstance
	_ = json.Unmarshal(data, &cp)
	return &cp
}

func (s *MemoryStore) List(_ context.Context, workflow string, limit int) ([]*RunInstance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*RunInstance
	for _, run := range s.runs {
		if workflow == "" || run.Workflow == workflow {
			result = append(result, cloneRun(run))
		}
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result, nil
}

func (s *MemoryStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.runs, id)
	for key := range s.signals {
		if strings.HasPrefix(key, id+":") {
			delete(s.signals, key)
		}
	}
	return nil
}

func (s *MemoryStore) Claim(_ context.Context, runID, owner string, ttl time.Duration) (bool, error) {
	return s.leases.claim(runID, owner, ttl), nil
}

func (s *MemoryStore) Renew(_ context.Context, runID, owner string, ttl time.Duration) (bool, error) {
	return s.leases.renew(runID, owner, ttl), nil
}

func (s *MemoryStore) Release(_ context.Context, runID, owner string) error {
	s.leases.release(runID, owner)
	return nil
}

func (s *MemoryStore) Unfinished(_ context.Context, limit int) ([]*RunInstance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*RunInstance
	for _, run := range s.runs {
		if !finished(run.Status) {
			out = append(out, cloneRun(run))
		}
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *MemoryStore) PushSignal(_ context.Context, runID, event string, data Payload) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.signals == nil {
		s.signals = map[string][]Payload{}
	}
	key := runID + ":" + event
	s.signals[key] = append(s.signals[key], clonePayload(data))
	return nil
}

func (s *MemoryStore) PopSignal(_ context.Context, runID, event string) (Payload, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := runID + ":" + event
	q := s.signals[key]
	if len(q) == 0 {
		return nil, false, nil
	}
	data := q[0]
	if len(q) == 1 {
		delete(s.signals, key)
	} else {
		s.signals[key] = q[1:]
	}
	return data, true, nil
}
