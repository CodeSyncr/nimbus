package cashier

import (
	"fmt"
	"sort"
	"sync"
)

// GatewayManager holds the app's registered gateways and the default selection.
// It is safe for concurrent use — one app can serve many gateways at once and
// pick per request.
type GatewayManager struct {
	mu          sync.RWMutex
	gateways    map[string]Gateway
	defaultName string
}

// NewGatewayManager creates an empty gateway registry.
func NewGatewayManager() *GatewayManager {
	return &GatewayManager{gateways: map[string]Gateway{}}
}

// Register adds a gateway. The first one registered becomes the default until
// SetDefault is called. Re-registering the same name replaces it.
func (m *GatewayManager) Register(g Gateway) *GatewayManager {
	if g == nil {
		return m
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gateways[g.Name()] = g
	if m.defaultName == "" {
		m.defaultName = g.Name()
	}
	return m
}

// SetDefault selects the default gateway by name (no-op if not registered).
func (m *GatewayManager) SetDefault(name string) *GatewayManager {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.gateways[name]; ok {
		m.defaultName = name
	}
	return m
}

// Gateway returns the gateway registered under name.
func (m *GatewayManager) Gateway(name string) (Gateway, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	g, ok := m.gateways[name]
	if !ok {
		return nil, fmt.Errorf("cashier: gateway %q is not registered", name)
	}
	return g, nil
}

// Default returns the default gateway, or nil when none are registered.
func (m *GatewayManager) Default() Gateway {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.gateways[m.defaultName]
}

// DefaultName returns the default gateway's name.
func (m *GatewayManager) DefaultName() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.defaultName
}

// Use resolves a gateway by name, falling back to the default when name is
// empty — the typical per-request selector.
func (m *GatewayManager) Use(name string) (Gateway, error) {
	if name == "" {
		if g := m.Default(); g != nil {
			return g, nil
		}
		return nil, fmt.Errorf("cashier: no default gateway registered")
	}
	return m.Gateway(name)
}

// Names returns the registered gateway names, sorted.
func (m *GatewayManager) Names() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.gateways))
	for n := range m.gateways {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
