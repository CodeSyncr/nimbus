package inertia

import "sync"

var (
	mu      sync.RWMutex
	manager Manager
)

func setManager(m Manager) {
	mu.Lock()
	defer mu.Unlock()
	manager = m
}

func getManager() Manager {
	mu.RLock()
	defer mu.RUnlock()
	return manager
}
