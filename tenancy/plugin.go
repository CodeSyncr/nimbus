package tenancy

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/CodeSyncr/nimbus"
	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/lucid"
	"github.com/CodeSyncr/nimbus/router"
)

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
	db *lucid.DB
}

// NewGormStore creates a GORM-backed tenant store.
func NewGormStore(db *lucid.DB) *GormStore {
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
