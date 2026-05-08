package tenancy

import (
	"context"
	"fmt"
	"net/http"

	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/lucid"
	"github.com/CodeSyncr/nimbus/router"
)

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
func DB(c *nhttp.Context) *lucid.DB {
	val := c.Request.Context().Value(tenantDBKey)
	if val == nil {
		return nil
	}
	return val.(*lucid.DB)
}

// ID returns just the tenant ID from context.
func ID(c *nhttp.Context) string {
	t := Current(c)
	if t == nil {
		return ""
	}
	return t.ID
}
