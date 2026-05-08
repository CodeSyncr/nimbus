/*
|--------------------------------------------------------------------------
| Nimbus Multi-Tenancy
|--------------------------------------------------------------------------
|
| First-class multi-tenant support with automatic tenant resolution
| from subdomain, header, path, or custom resolver. Supports three
| isolation strategies:
|
|   - Row-level: shared database, tenant_id column
|   - Schema-level: shared database, per-tenant schema (Postgres)
|   - Database-level: separate database per tenant
|
| Usage:
|
|   // Setup
|   tm := tenancy.New(tenancy.Config{
|       ResolveBy: tenancy.ResolveSubdomain, // or Header, Path, Custom
|       Strategy:  tenancy.StrategyRow,
|   })
|   app.Use(tm.Plugin())
|
|   // In handlers
|   tenant := tenancy.Current(c)
|   db := tenancy.DB(c) // tenant-scoped database connection
|
|   // Row-level: auto-applies WHERE tenant_id = ? to queries
|   // Schema-level: switches Postgres search_path
|   // Database-level: returns the tenant's own *lucid.DB
|
*/

package tenancy

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/CodeSyncr/nimbus/lucid"
)

// ---------------------------------------------------------------------------
// Core Types
// ---------------------------------------------------------------------------

// Strategy defines the tenancy isolation approach.
type Strategy string

const (
	StrategyRow      Strategy = "row"      // shared DB, tenant_id column
	StrategySchema   Strategy = "schema"   // shared DB, per-tenant schema
	StrategyDatabase Strategy = "database" // separate DB per tenant
)

// ResolveMethod defines how the tenant is identified from requests.
type ResolveMethod string

const (
	ResolveSubdomain ResolveMethod = "subdomain"
	ResolveHeader    ResolveMethod = "header"
	ResolvePath      ResolveMethod = "path"
	ResolveCustom    ResolveMethod = "custom"
)

// Tenant represents a single tenant.
type Tenant struct {
	ID       string            `json:"id" gorm:"primaryKey"`
	Name     string            `json:"name"`
	Domain   string            `json:"domain,omitempty"`
	Schema   string            `json:"schema,omitempty"`
	DBName   string            `json:"db_name,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty" gorm:"-"`
	Active   bool              `json:"active" gorm:"default:true"`
}

// Config configures the tenancy system.
type Config struct {
	ResolveBy      ResolveMethod
	HeaderName     string // for ResolveHeader (default: X-Tenant-ID)
	PathPrefix     string // for ResolvePath (default: first path segment)
	Strategy       Strategy
	DefaultDB      *lucid.DB                               // the main database connection
	DBResolver     func(tenant *Tenant) (*lucid.DB, error) // for StrategyDatabase
	CustomResolver func(r *http.Request) (string, error)  // for ResolveCustom
}

type contextKey string

const tenantKey contextKey = "nimbus.tenant"
const tenantDBKey contextKey = "nimbus.tenant.db"

// ---------------------------------------------------------------------------
// Manager
// ---------------------------------------------------------------------------

// Manager handles tenant resolution and database scoping.
type Manager struct {
	config  Config
	tenants sync.Map // id -> *Tenant
	dbs     sync.Map // id -> *lucid.DB (for database strategy caching)
	store   TenantStore
}

// TenantStore is the interface for loading/saving tenants.
type TenantStore interface {
	FindByID(ctx context.Context, id string) (*Tenant, error)
	FindByDomain(ctx context.Context, domain string) (*Tenant, error)
	All(ctx context.Context) ([]*Tenant, error)
	Save(ctx context.Context, tenant *Tenant) error
	Delete(ctx context.Context, id string) error
}

// New creates a new tenancy manager.
func New(cfg Config) *Manager {
	if cfg.HeaderName == "" {
		cfg.HeaderName = "X-Tenant-ID"
	}
	if cfg.Strategy == "" {
		cfg.Strategy = StrategyRow
	}
	m := &Manager{config: cfg}
	return m
}

// SetStore configures the tenant store.
func (m *Manager) SetStore(store TenantStore) {
	m.store = store
}

// Register adds a tenant.
func (m *Manager) Register(t *Tenant) {
	m.tenants.Store(t.ID, t)
}

// Get returns a tenant by ID.
func (m *Manager) Get(id string) (*Tenant, bool) {
	val, ok := m.tenants.Load(id)
	if !ok {
		return nil, false
	}
	return val.(*Tenant), true
}

// ---------------------------------------------------------------------------
// Tenant Resolution
// ---------------------------------------------------------------------------

// Resolve extracts the tenant ID from an HTTP request.
func (m *Manager) Resolve(r *http.Request) (string, error) {
	switch m.config.ResolveBy {
	case ResolveSubdomain:
		return m.resolveSubdomain(r)
	case ResolveHeader:
		return m.resolveHeader(r)
	case ResolvePath:
		return m.resolvePath(r)
	case ResolveCustom:
		if m.config.CustomResolver == nil {
			return "", fmt.Errorf("tenancy: custom resolver not configured")
		}
		return m.config.CustomResolver(r)
	default:
		return "", fmt.Errorf("tenancy: unknown resolve method %q", m.config.ResolveBy)
	}
}

func (m *Manager) resolveSubdomain(r *http.Request) (string, error) {
	host := r.Host
	// Remove port
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}
	parts := strings.SplitN(host, ".", 3)
	if len(parts) < 3 {
		return "", fmt.Errorf("tenancy: no subdomain in host %q", r.Host)
	}
	return parts[0], nil
}

func (m *Manager) resolveHeader(r *http.Request) (string, error) {
	id := r.Header.Get(m.config.HeaderName)
	if id == "" {
		return "", fmt.Errorf("tenancy: missing header %q", m.config.HeaderName)
	}
	return id, nil
}

func (m *Manager) resolvePath(r *http.Request) (string, error) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		return "", fmt.Errorf("tenancy: no tenant in path")
	}
	return parts[0], nil
}
