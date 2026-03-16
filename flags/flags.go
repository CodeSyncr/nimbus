/*
|--------------------------------------------------------------------------
| Nimbus Feature Flags
|--------------------------------------------------------------------------
|
| Built-in feature flag system with percentage rollouts, user/group
| targeting, A/B testing, and a runtime toggle API.
|
| Usage:
|
|   // Define flags
|   flags.Define("dark-mode", flags.Config{
|       Default: false,
|       Description: "Enable dark mode UI",
|   })
|
|   flags.Define("new-checkout", flags.Config{
|       Default: false,
|       RolloutPercent: 25,  // 25% of users
|       Groups: []string{"beta-testers"},
|   })
|
|   // Check in handlers
|   if flags.Active("dark-mode", c) { ... }
|   if flags.For(userID).Active("new-checkout") { ... }
|
|   // A/B testing
|   variant := flags.Variant("checkout-flow", userID) // "A" or "B"
|
*/

package flags

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Core Types
// ---------------------------------------------------------------------------

// Config describes a feature flag.
type Config struct {
	Default        bool       `json:"default"`
	Description    string     `json:"description,omitempty"`
	RolloutPercent int        `json:"rollout_percent,omitempty"` // 0-100
	Groups         []string   `json:"groups,omitempty"`          // group names
	Users          []string   `json:"users,omitempty"`           // specific user IDs
	Variants       []string   `json:"variants,omitempty"`        // for A/B tests
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
}

// Flag is the runtime representation.
type Flag struct {
	Name      string    `json:"name"`
	Config    Config    `json:"config"`
	Enabled   bool      `json:"enabled"`
	UpdatedAt time.Time `json:"updated_at"`
}

// UserContext represents a user for flag evaluation.
type UserContext struct {
	ID     string
	Groups []string
	Attrs  map[string]string
}

// ---------------------------------------------------------------------------
// Store Interface
// ---------------------------------------------------------------------------

// Store persists feature flag state.
type Store interface {
	Load(ctx context.Context) (map[string]*Flag, error)
	Save(ctx context.Context, flags map[string]*Flag) error
}

// ---------------------------------------------------------------------------
// Manager
// ---------------------------------------------------------------------------

// Manager is the central feature flag registry.
type Manager struct {
	mu     sync.RWMutex
	flags  map[string]*Flag
	store  Store
	groups map[string]func(UserContext) bool // group resolvers
}

var (
	global   *Manager
	globalMu sync.RWMutex
)

// New creates a new feature flag manager.
func New(store Store) *Manager {
	if store == nil {
		store = &MemoryStore{}
	}
	m := &Manager{
		flags:  make(map[string]*Flag),
		store:  store,
		groups: make(map[string]func(UserContext) bool),
	}
	// Load from store
	if flags, err := store.Load(context.Background()); err == nil && flags != nil {
		m.flags = flags
	}
	globalMu.Lock()
	global = m
	globalMu.Unlock()
	return m
}

// Default returns the global manager.
func Default() *Manager {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return global
}

// ---------------------------------------------------------------------------
// Flag Definition
// ---------------------------------------------------------------------------

// Define registers a feature flag.
func (m *Manager) Define(name string, cfg Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.flags[name]; ok {
		// Preserve runtime state, update config
		existing.Config = cfg
		existing.UpdatedAt = time.Now()
		return
	}
	m.flags[name] = &Flag{
		Name:      name,
		Config:    cfg,
		Enabled:   cfg.Default,
		UpdatedAt: time.Now(),
	}
}

// Define registers a flag on the global manager.
func Define(name string, cfg Config) {
	Default().Define(name, cfg)
}

// ---------------------------------------------------------------------------
// Group Registration
// ---------------------------------------------------------------------------

// Group registers a group resolver function.
func (m *Manager) Group(name string, resolver func(UserContext) bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.groups[name] = resolver
}

// ---------------------------------------------------------------------------
// Flag Evaluation
// ---------------------------------------------------------------------------

// Active checks if a flag is active for the given user context.
func (m *Manager) Active(name string, user *UserContext) bool {
	m.mu.RLock()
	f, ok := m.flags[name]
	m.mu.RUnlock()
	if !ok {
		return false
	}

	// Check expiry
	if f.Config.ExpiresAt != nil && time.Now().After(*f.Config.ExpiresAt) {
		return false
	}

	// If globally disabled, short-circuit
	if !f.Enabled {
		return false
	}

	// No user context — return the global enabled state
	if user == nil {
		return f.Enabled
	}

	// Check specific user targeting
	for _, uid := range f.Config.Users {
		if uid == user.ID {
			return true
		}
	}

	// Check group targeting
	for _, groupName := range f.Config.Groups {
		// Check manager-level group resolver
		m.mu.RLock()
		resolver, hasResolver := m.groups[groupName]
		m.mu.RUnlock()
		if hasResolver && resolver(*user) {
			return true
		}
		// Check user's direct group membership
		for _, ug := range user.Groups {
			if ug == groupName {
				return true
			}
		}
	}

	// Check rollout percentage (deterministic hashing)
	if f.Config.RolloutPercent > 0 && f.Config.RolloutPercent < 100 {
		hash := hashUserFlag(user.ID, name)
		bucket := int(hash % 100)
		return bucket < f.Config.RolloutPercent
	}

	// If groups/users were specified but user didn't match, return false
	if len(f.Config.Groups) > 0 || len(f.Config.Users) > 0 {
		return false
	}

	return f.Enabled
}

// Active checks a flag on the global manager (no user context).
func Active(name string) bool {
	return Default().Active(name, nil)
}

// For creates a user-scoped evaluator.
func For(userID string, groups ...string) *UserEvaluator {
	return &UserEvaluator{
		manager: Default(),
		user:    &UserContext{ID: userID, Groups: groups},
	}
}

// UserEvaluator evaluates flags for a specific user.
type UserEvaluator struct {
	manager *Manager
	user    *UserContext
}

func (e *UserEvaluator) Active(name string) bool {
	return e.manager.Active(name, e.user)
}

func (e *UserEvaluator) Variant(name string) string {
	return e.manager.Variant(name, e.user.ID)
}

// Variant returns which variant a user gets for an A/B test flag.
func (m *Manager) Variant(name string, userID string) string {
	m.mu.RLock()
	f, ok := m.flags[name]
	m.mu.RUnlock()
	if !ok || len(f.Config.Variants) == 0 {
		return ""
	}
	hash := hashUserFlag(userID, name)
	idx := int(hash % uint64(len(f.Config.Variants)))
	return f.Config.Variants[idx]
}

// Variant on the global manager.
func Variant(name, userID string) string {
	return Default().Variant(name, userID)
}

// ---------------------------------------------------------------------------
// Runtime Toggle
// ---------------------------------------------------------------------------

// Enable turns a flag on globally.
func (m *Manager) Enable(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.flags[name]
	if !ok {
		return fmt.Errorf("flag %q not defined", name)
	}
	f.Enabled = true
	f.UpdatedAt = time.Now()
	return m.persist()
}

// Disable turns a flag off globally.
func (m *Manager) Disable(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.flags[name]
	if !ok {
		return fmt.Errorf("flag %q not defined", name)
	}
	f.Enabled = false
	f.UpdatedAt = time.Now()
	return m.persist()
}

// SetRollout updates the rollout percentage.
func (m *Manager) SetRollout(name string, percent int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.flags[name]
	if !ok {
		return fmt.Errorf("flag %q not defined", name)
	}
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	f.Config.RolloutPercent = percent
	f.UpdatedAt = time.Now()
	return m.persist()
}

// All returns all defined flags.
func (m *Manager) All() map[string]*Flag {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]*Flag, len(m.flags))
	for k, v := range m.flags {
		result[k] = v
	}
	return result
}

func (m *Manager) persist() error {
	return m.store.Save(context.Background(), m.flags)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func hashUserFlag(userID, flagName string) uint64 {
	h := sha256.Sum256([]byte(userID + ":" + flagName))
	return binary.BigEndian.Uint64(h[:8])
}

// ---------------------------------------------------------------------------
// Memory Store
// ---------------------------------------------------------------------------

type MemoryStore struct {
	mu    sync.RWMutex
	flags map[string]*Flag
}

func (s *MemoryStore) Load(_ context.Context) (map[string]*Flag, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.flags, nil
}

func (s *MemoryStore) Save(_ context.Context, flags map[string]*Flag) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flags = flags
	return nil
}

// ---------------------------------------------------------------------------
// File Store (JSON)
// ---------------------------------------------------------------------------

// FileStore persists flags to a JSON file on disk.
type FileStore struct {
	path string
	mu   sync.RWMutex
}

func NewFileStore(path string) *FileStore {
	return &FileStore{path: path}
}

func (s *FileStore) Load(_ context.Context) (map[string]*Flag, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]*Flag), nil
		}
		return nil, err
	}
	var flags map[string]*Flag
	if err := json.Unmarshal(data, &flags); err != nil {
		return nil, err
	}
	return flags, nil
}

func (s *FileStore) Save(_ context.Context, flags map[string]*Flag) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.MarshalIndent(flags, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0644)
}

// ---------------------------------------------------------------------------
// Environment Loader
// ---------------------------------------------------------------------------

// LoadFromEnv loads flags from environment variables.
// Pattern: FEATURE_FLAG_NAME=true|false
func (m *Manager) LoadFromEnv() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]
		val := strings.ToLower(parts[1])
		if !strings.HasPrefix(key, "FEATURE_") {
			continue
		}
		name := strings.ToLower(strings.TrimPrefix(key, "FEATURE_"))
		name = strings.ReplaceAll(name, "_", "-")
		enabled := val == "true" || val == "1" || val == "on"
		if f, ok := m.flags[name]; ok {
			f.Enabled = enabled
		} else {
			m.flags[name] = &Flag{
				Name:      name,
				Config:    Config{Default: enabled},
				Enabled:   enabled,
				UpdatedAt: time.Now(),
			}
		}
	}
}
