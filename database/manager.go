package database

import (
	"fmt"
	"sync"

	"gorm.io/gorm"
)

// ══════════════════════════════════════════════════════════════════
// Multi-DB Connection Manager
// ══════════════════════════════════════════════════════════════════
//
// Nimbus supports multiple named database connections. Applications
// typically define connections in config/database.go and register
// them at boot time:
//
//	database.AddConnection("default", db)
//	database.AddConnection("analytics", analyticsDB)
//	database.AddConnection("logs", logsDB)
//
// Then resolve anywhere:
//
//	db := database.Connection("analytics")
//	db.Find(&events)
//
// The "default" connection is what GetDB / database.Get returns.

// ConnectionManager manages named database connections.
type ConnectionManager struct {
	mu          sync.RWMutex
	connections map[string]*gorm.DB
	defaultName string
}

var (
	manager     *ConnectionManager
	managerOnce sync.Once
)

// getManager returns the singleton connection manager.
func getManager() *ConnectionManager {
	managerOnce.Do(func() {
		manager = &ConnectionManager{
			connections: make(map[string]*gorm.DB),
			defaultName: "default",
		}
	})
	return manager
}

// AddConnection registers a named database connection.
// The first connection registered is also set as the global DB.
func AddConnection(name string, db *gorm.DB) {
	m := getManager()
	m.mu.Lock()
	defer m.mu.Unlock()

	m.connections[name] = db

	// First connection becomes the default global DB
	if len(m.connections) == 1 || name == m.defaultName {
		DB = db
	}
}

// Connection returns a named database connection.
// Returns nil if the connection name is not registered.
func Connection(name string) *gorm.DB {
	m := getManager()
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connections[name]
}

// MustConnection returns a named connection or panics if not found.
func MustConnection(name string) *gorm.DB {
	db := Connection(name)
	if db == nil {
		panic(fmt.Sprintf("database: connection %q not registered", name))
	}
	return db
}

// SetDefault sets which named connection is the "default" one
// (returned by Get() and used globally).
func SetDefault(name string) error {
	m := getManager()
	m.mu.Lock()
	defer m.mu.Unlock()

	conn, ok := m.connections[name]
	if !ok {
		return fmt.Errorf("database: connection %q not registered", name)
	}
	m.defaultName = name
	DB = conn
	return nil
}

// ConnectionNames returns all registered connection names.
func ConnectionNames() []string {
	m := getManager()
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.connections))
	for name := range m.connections {
		names = append(names, name)
	}
	return names
}

// CloseAll closes all registered connections (call at shutdown).
func CloseAll() error {
	m := getManager()
	m.mu.Lock()
	defer m.mu.Unlock()

	var lastErr error
	for name, db := range m.connections {
		sqlDB, err := db.DB()
		if err != nil {
			lastErr = fmt.Errorf("database: close %q: %w", name, err)
			continue
		}
		if err := sqlDB.Close(); err != nil {
			lastErr = fmt.Errorf("database: close %q: %w", name, err)
		}
	}
	m.connections = make(map[string]*gorm.DB)
	DB = nil
	return lastErr
}

// ── Multi-Connection Config ─────────────────────────────────────

// ConnectionConfig holds configuration for a single database connection.
type ConnectionConfig struct {
	// Name is the connection identifier (e.g. "default", "analytics", "logs").
	Name string

	// Driver: "postgres", "mysql", "sqlite"
	Driver string

	// DSN is the full connection string.
	DSN string

	// Debug enables SQL logging.
	Debug bool

	// MaxOpenConns sets the max number of open connections (0 = unlimited).
	MaxOpenConns int

	// MaxIdleConns sets the max number of idle connections.
	MaxIdleConns int
}

// ConnectAll establishes multiple database connections from config.
func ConnectAll(configs []ConnectionConfig) error {
	for _, cfg := range configs {
		db, err := ConnectWithConfig(ConnectConfig{
			Driver: cfg.Driver,
			DSN:    cfg.DSN,
			Debug:  cfg.Debug,
		})
		if err != nil {
			return fmt.Errorf("database: connect %q: %w", cfg.Name, err)
		}

		// Configure connection pool
		if cfg.MaxOpenConns > 0 || cfg.MaxIdleConns > 0 {
			sqlDB, err := db.DB()
			if err == nil {
				if cfg.MaxOpenConns > 0 {
					sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
				}
				if cfg.MaxIdleConns > 0 {
					sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
				}
			}
		}

		name := cfg.Name
		if name == "" {
			name = "default"
		}
		AddConnection(name, db)
	}
	return nil
}

// ── On-Connection Query Builder ─────────────────────────────────

// On returns a Query builder on a specific named connection.
//
//	database.On("analytics").Where("event_type = ?", "click").Get(&events)
func On(connectionName string) *Query {
	db := Connection(connectionName)
	if db == nil {
		// Return a query that will fail gracefully
		return &Query{db: &gorm.DB{}}
	}
	return &Query{db: db}
}

// OnModel returns a Query builder for a model on a specific named connection.
//
//	database.OnModel("analytics", &AnalyticsEvent{}).Where("user_id = ?", uid).Get(&events)
func OnModel(connectionName string, model any) *Query {
	db := Connection(connectionName)
	if db == nil {
		return &Query{db: &gorm.DB{}}
	}
	return &Query{db: db.Model(model)}
}
