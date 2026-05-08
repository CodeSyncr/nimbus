package tenancy

import (
	"fmt"
	"regexp"

	"github.com/CodeSyncr/nimbus/lucid"
)

// ---------------------------------------------------------------------------
// Database Scoping
// ---------------------------------------------------------------------------

var schemaNameRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// ScopeDB returns a tenant-scoped database connection.
func (m *Manager) ScopeDB(tenant *Tenant) (*lucid.DB, error) {
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

func (m *Manager) scopeRow(tenant *Tenant) (*lucid.DB, error) {
	if m.config.DefaultDB == nil {
		return nil, fmt.Errorf("tenancy: DefaultDB not configured")
	}
	// Add a global scope that filters by tenant_id
	db := m.config.DefaultDB.Session(&lucid.Session{NewDB: true})
	db = db.Where("tenant_id = ?", tenant.ID)
	// Add a callback to auto-set tenant_id on create
	db.Callback().Create().Before("gorm:create").Register("tenancy:set_tenant_id", func(tx *lucid.DB) {
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

func (m *Manager) scopeSchema(tenant *Tenant) (*lucid.DB, error) {
	if m.config.DefaultDB == nil {
		return nil, fmt.Errorf("tenancy: DefaultDB not configured")
	}
	schema := tenant.Schema
	if schema == "" {
		schema = "tenant_" + tenant.ID
	}
	
	// SECURITY FIX: validate schema name to prevent SQL injection
	if !schemaNameRegex.MatchString(schema) {
		return nil, fmt.Errorf("tenancy: invalid schema name %q", schema)
	}

	db := m.config.DefaultDB.Session(&lucid.Session{NewDB: true})
	// Quoting identifier for double safety
	db = db.Exec(fmt.Sprintf("SET search_path TO %q, public", schema))
	return db, nil
}

func (m *Manager) scopeDatabase(tenant *Tenant) (*lucid.DB, error) {
	// Check cache first
	if cached, ok := m.dbs.Load(tenant.ID); ok {
		return cached.(*lucid.DB), nil
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
