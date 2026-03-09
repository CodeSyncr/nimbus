package drive

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var ErrDriveNotRegistered = errors.New("drive: plugin not registered or app not booted")

var (
	globalManager *Manager
	globalMu      sync.RWMutex
)

// Manager holds disks and provides access.
type Manager struct {
	config Config
	disks  map[string]Disk
}

// NewManager creates a manager with the given config.
func NewManager(cfg Config) *Manager {
	m := &Manager{
		config: cfg,
		disks:  make(map[string]Disk),
	}
	m.initDisks()
	return m
}

func (m *Manager) initDisks() {
	for name, dc := range m.config.Disks {
		switch dc.Driver {
		case "fs":
			location := dc.Location
			if location == "" {
				location = "storage"
			}
			absRoot, _ := filepath.Abs(location)
			if absRoot == "" {
				absRoot = location
			}
			_ = os.MkdirAll(absRoot, 0755)

			baseURL := os.Getenv("APP_URL")
			if baseURL == "" {
				baseURL = "http://localhost:" + os.Getenv("PORT")
				if baseURL == "http://localhost:" {
					baseURL = "http://localhost:3333"
				}
			}
			basePath := dc.RouteBasePath
			if basePath == "" {
				basePath = "/uploads"
			}
			m.disks[name] = NewFSDisk(location, baseURL, basePath)
		case "s3":
			_ = name
		case "gcs":
			_ = name
		}
	}
}

// Use returns the disk with the given name. Empty name uses default.
func (m *Manager) Use(name string) (Disk, error) {
	if name == "" {
		name = m.config.Default
	}
	if name == "" {
		name = "fs"
	}
	d, ok := m.disks[name]
	if !ok {
		return nil, fmt.Errorf("drive: disk %q not configured", name)
	}
	return d, nil
}

// UseDefault returns the default disk.
func (m *Manager) UseDefault() (Disk, error) {
	return m.Use("")
}

// SetGlobal sets the global drive manager (called by plugin Boot).
func SetGlobal(manager *Manager) {
	globalMu.Lock()
	defer globalMu.Unlock()
	globalManager = manager
}

// GetGlobal returns the global drive manager.
func GetGlobal() *Manager {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return globalManager
}
