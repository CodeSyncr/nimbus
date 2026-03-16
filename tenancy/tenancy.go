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
|   // Database-level: returns the tenant's own *gorm.DB
|
*/

package tenancy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/CodeSyncr/nimbus"
	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
	"gorm.io/gorm"
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
	DefaultDB      *gorm.DB                               // the main database connection
	DBResolver     func(tenant *Tenant) (*gorm.DB, error) // for StrategyDatabase
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
	dbs     sync.Map // id -> *gorm.DB (for database strategy caching)
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

// ---------------------------------------------------------------------------
// Database Scoping
// ---------------------------------------------------------------------------

// ScopeDB returns a tenant-scoped database connection.
func (m *Manager) ScopeDB(tenant *Tenant) (*gorm.DB, error) {
	switch m.config.Strategy {
	case StrategyRow:
		return m.scopeRow(tenant)
	case StrategySchema:
		return m.scopeSchema(tenant)
	case StrategyDatabase:
		return m.scopeDatabase(tenant)
	default:
		return nil, fmt.Errorf("tenancy: unknown strategy %q", m.config.Strategy)
	}
}

func (m *Manager) scopeRow(tenant *Tenant) (*gorm.DB, error) {
	if m.config.DefaultDB == nil {
		return nil, fmt.Errorf("tenancy: DefaultDB not configured")
	}
	// Add a global scope that filters by tenant_id
	db := m.config.DefaultDB.Session(&gorm.Session{NewDB: true})
	db = db.Where("tenant_id = ?", tenant.ID)
	// Add a callback to auto-set tenant_id on create
	db.Callback().Create().Before("gorm:create").Register("tenancy:set_tenant_id", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil {
			for _, field := range tx.Statement.Schema.Fields {
				if field.DBName == "tenant_id" {
					_ = field.Set(tx.Statement.Context, tx.Statement.ReflectValue, tenant.ID)
				}
			}
		}
	})
	return db, nil
}

func (m *Manager) scopeSchema(tenant *Tenant) (*gorm.DB, error) {
	if m.config.DefaultDB == nil {
		return nil, fmt.Errorf("tenancy: DefaultDB not configured")
	}
	schema := tenant.Schema
	if schema == "" {
		schema = "tenant_" + tenant.ID
	}
	db := m.config.DefaultDB.Session(&gorm.Session{NewDB: true})
	db = db.Exec("SET search_path TO " + schema + ", public")
	return db, nil
}

func (m *Manager) scopeDatabase(tenant *Tenant) (*gorm.DB, error) {
	// Check cache first
	if cached, ok := m.dbs.Load(tenant.ID); ok {
		return cached.(*gorm.DB), nil
	}
	if m.config.DBResolver == nil {
		return nil, fmt.Errorf("tenancy: DBResolver not configured for database strategy")
	}
	db, err := m.config.DBResolver(tenant)
	if err != nil {
		return nil, fmt.Errorf("tenancy: resolve DB for tenant %s: %w", tenant.ID, err)
	}
	m.dbs.Store(tenant.ID, db)
	return db, nil
}

// ---------------------------------------------------------------------------
// Middleware
// ---------------------------------------------------------------------------

// Middleware returns the tenancy resolution middleware.
func (m *Manager) Middleware() router.Middleware {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *nhttp.Context) error {
			tenantID, err := m.Resolve(c.Request)
			if err != nil {
				return c.JSON(http.StatusBadRequest, map[string]string{
					"error": "Could not resolve tenant: " + err.Error(),
				})
			}

			// Lookup tenant
			tenant, ok := m.Get(tenantID)
			if !ok && m.store != nil {
				tenant, err = m.store.FindByID(c.Request.Context(), tenantID)
				if err != nil || tenant == nil {
					return c.JSON(http.StatusNotFound, map[string]string{
						"error": fmt.Sprintf("Tenant %q not found", tenantID),
					})
				}
				m.Register(tenant)
			} else if !ok {
				return c.JSON(http.StatusNotFound, map[string]string{
					"error": fmt.Sprintf("Tenant %q not found", tenantID),
				})
			}

			if !tenant.Active {
				return c.JSON(http.StatusForbidden, map[string]string{
					"error": "Tenant is inactive",
				})
			}

			// Scope database
			db, err := m.ScopeDB(tenant)
			if err != nil {
				return c.JSON(http.StatusInternalServerError, map[string]string{
					"error": "Database scoping failed: " + err.Error(),
				})
			}

			// Store in context
			ctx := context.WithValue(c.Request.Context(), tenantKey, tenant)
			ctx = context.WithValue(ctx, tenantDBKey, db)
			c.Request = c.Request.WithContext(ctx)

			return next(c)
		}
	}
}

// ---------------------------------------------------------------------------
// Context Helpers
// ---------------------------------------------------------------------------

// Current returns the current tenant from the request context.
func Current(c *nhttp.Context) *Tenant {
	val := c.Request.Context().Value(tenantKey)
	if val == nil {
		return nil
	}
	return val.(*Tenant)
}

// DB returns the tenant-scoped database from the request context.
func DB(c *nhttp.Context) *gorm.DB {
	val := c.Request.Context().Value(tenantDBKey)
	if val == nil {
		return nil
	}
	return val.(*gorm.DB)
}

// ID returns just the tenant ID from context.
func ID(c *nhttp.Context) string {
	t := Current(c)
	if t == nil {
		return ""
	}
	return t.ID
}

// ---------------------------------------------------------------------------
// Nimbus Plugin
// ---------------------------------------------------------------------------

var (
	_ nimbus.Plugin        = (*TenantPlugin)(nil)
	_ nimbus.HasRoutes     = (*TenantPlugin)(nil)
	_ nimbus.HasMiddleware = (*TenantPlugin)(nil)
)

// TenantPlugin integrates multi-tenancy with Nimbus.
type TenantPlugin struct {
	nimbus.BasePlugin
	Manager *Manager
}

// NewPlugin creates a tenancy plugin.
func NewPlugin(cfg Config) *TenantPlugin {
	return &TenantPlugin{
		BasePlugin: nimbus.BasePlugin{
			PluginName:    "tenancy",
			PluginVersion: "1.0.0",
		},
		Manager: New(cfg),
	}
}

func (p *TenantPlugin) Register(app *nimbus.App) error {
	app.Container.Singleton("tenancy.manager", func() *Manager { return p.Manager })
	return nil
}

func (p *TenantPlugin) Boot(app *nimbus.App) error {
	return nil
}

func (p *TenantPlugin) Middleware() map[string]router.Middleware {
	return map[string]router.Middleware{
		"tenant": p.Manager.Middleware(),
	}
}

func (p *TenantPlugin) RegisterRoutes(r *router.Router) {
	grp := r.Group("/_tenants")
	grp.Get("/", p.listTenants)
	grp.Post("/", p.createTenant)
	grp.Get("/:id", p.getTenant)
	grp.Delete("/:id", p.deleteTenant)
	grp.Get("/current", p.currentTenant)
}

func (p *TenantPlugin) listTenants(c *nhttp.Context) error {
	if p.Manager.store == nil {
		// Return from memory
		var tenants []*Tenant
		p.Manager.tenants.Range(func(key, value any) bool {
			tenants = append(tenants, value.(*Tenant))
			return true
		})
		return c.JSON(http.StatusOK, tenants)
	}
	tenants, err := p.Manager.store.All(c.Request.Context())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, tenants)
}

func (p *TenantPlugin) createTenant(c *nhttp.Context) error {
	var t Tenant
	if err := json.NewDecoder(c.Request.Body).Decode(&t); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	if t.ID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "tenant id required"})
	}
	t.Active = true
	p.Manager.Register(&t)
	if p.Manager.store != nil {
		if err := p.Manager.store.Save(c.Request.Context(), &t); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
	}
	return c.JSON(http.StatusCreated, t)
}

func (p *TenantPlugin) getTenant(c *nhttp.Context) error {
	id := c.Param("id")
	tenant, ok := p.Manager.Get(id)
	if !ok {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "tenant not found"})
	}
	return c.JSON(http.StatusOK, tenant)
}

func (p *TenantPlugin) deleteTenant(c *nhttp.Context) error {
	id := c.Param("id")
	p.Manager.tenants.Delete(id)
	if p.Manager.store != nil {
		_ = p.Manager.store.Delete(c.Request.Context(), id)
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "deleted"})
}

func (p *TenantPlugin) currentTenant(c *nhttp.Context) error {
	t := Current(c)
	if t == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "no tenant in context"})
	}
	return c.JSON(http.StatusOK, t)
}

// ---------------------------------------------------------------------------
// GORM Tenant Store
// ---------------------------------------------------------------------------

// GormStore implements TenantStore using GORM.
type GormStore struct {
	db *gorm.DB
}

// NewGormStore creates a GORM-backed tenant store.
func NewGormStore(db *gorm.DB) *GormStore {
	_ = db.AutoMigrate(&Tenant{})
	return &GormStore{db: db}
}

func (s *GormStore) FindByID(_ context.Context, id string) (*Tenant, error) {
	var t Tenant
	if err := s.db.First(&t, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *GormStore) FindByDomain(_ context.Context, domain string) (*Tenant, error) {
	var t Tenant
	if err := s.db.First(&t, "domain = ?", domain).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *GormStore) All(_ context.Context) ([]*Tenant, error) {
	var tenants []*Tenant
	if err := s.db.Find(&tenants).Error; err != nil {
		return nil, err
	}
	return tenants, nil
}

func (s *GormStore) Save(_ context.Context, tenant *Tenant) error {
	return s.db.Save(tenant).Error
}

func (s *GormStore) Delete(_ context.Context, id string) error {
	return s.db.Delete(&Tenant{}, "id = ?", id).Error
}
