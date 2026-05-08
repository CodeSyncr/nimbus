package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// ---------------------------------------------------------------------------
// Store Interface
// ---------------------------------------------------------------------------

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
	mu   sync.RWMutex
	runs map[string]*RunInstance
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{runs: make(map[string]*RunInstance)}
}

func (s *MemoryStore) Save(_ context.Context, run *RunInstance) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Deep copy to avoid mutation issues
	data, _ := json.Marshal(run)
	var copy RunInstance
	_ = json.Unmarshal(data, &copy)
	s.runs[run.ID] = &copy
	return nil
}

func (s *MemoryStore) Load(_ context.Context, id string) (*RunInstance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	run, ok := s.runs[id]
	if !ok {
		return nil, fmt.Errorf("workflow run %q not found", id)
	}
	return run, nil
}

func (s *MemoryStore) List(_ context.Context, workflow string, limit int) ([]*RunInstance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*RunInstance
	for _, run := range s.runs {
		if workflow == "" || run.Workflow == workflow {
			result = append(result, run)
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
	return nil
}
