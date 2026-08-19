package tenancy

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/lucid"
	"github.com/CodeSyncr/nimbus/router"
)

type Project struct {
	ID       uint   `gorm:"primaryKey"`
	Name     string `json:"name"`
	TenantID string `json:"tenant_id"`
}

func openTestDB(t *testing.T) *lucid.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "tenancy-test.db")
	db, err := lucid.Open(sqlite.Open(dbPath), &lucid.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	return db
}

func TestResolveMethods(t *testing.T) {
	// Subdomain
	mSub := New(Config{ResolveBy: ResolveSubdomain})
	req1 := httptest.NewRequest("GET", "http://tenant1.example.com/test", nil)
	id1, err := mSub.Resolve(req1)
	if err != nil || id1 != "tenant1" {
		t.Errorf("ResolveSubdomain failed: got %q, err %v", id1, err)
	}

	req1Err := httptest.NewRequest("GET", "http://example.com/test", nil)
	_, err = mSub.Resolve(req1Err)
	if err == nil {
		t.Error("ResolveSubdomain expected error when no subdomain, got nil")
	}

	// Header
	mHeader := New(Config{ResolveBy: ResolveHeader, HeaderName: "X-My-Tenant"})
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.Header.Set("X-My-Tenant", "tenant2")
	id2, err := mHeader.Resolve(req2)
	if err != nil || id2 != "tenant2" {
		t.Errorf("ResolveHeader failed: got %q, err %v", id2, err)
	}

	// Path
	mPath := New(Config{ResolveBy: ResolvePath})
	req3 := httptest.NewRequest("GET", "/tenant3/posts/1", nil)
	id3, err := mPath.Resolve(req3)
	if err != nil || id3 != "tenant3" {
		t.Errorf("ResolvePath failed: got %q, err %v", id3, err)
	}

	// Custom
	mCustom := New(Config{
		ResolveBy: ResolveCustom,
		CustomResolver: func(r *http.Request) (string, error) {
			return "custom-tenant", nil
		},
	})
	req4 := httptest.NewRequest("GET", "/", nil)
	id4, err := mCustom.Resolve(req4)
	if err != nil || id4 != "custom-tenant" {
		t.Errorf("ResolveCustom failed: got %q, err %v", id4, err)
	}
}

func TestScopeRow(t *testing.T) {
	db := openTestDB(t)
	if err := db.AutoMigrate(&Project{}); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}

	m := New(Config{
		Strategy:  StrategyRow,
		DefaultDB: db,
	})

	t1DB, err := m.ScopeDB(&Tenant{ID: "t1"})
	if err != nil {
		t.Fatalf("ScopeDB failed: %v", err)
	}

	// Create a project in t1
	proj := Project{Name: "Project Alpha"}
	if err := t1DB.Create(&proj).Error; err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Verify tenant_id was automatically set by hook
	var check Project
	if err := db.First(&check, proj.ID).Error; err != nil {
		t.Fatalf("find failed: %v", err)
	}
	if check.TenantID != "t1" {
		t.Errorf("expected TenantID 't1', got %q", check.TenantID)
	}

	// Create a project in t2
	t2DB, _ := m.ScopeDB(&Tenant{ID: "t2"})
	proj2 := Project{Name: "Project Beta"}
	if err := t2DB.Create(&proj2).Error; err != nil {
		t.Fatalf("create in t2 failed: %v", err)
	}

	// Retrieve projects using t1's DB
	var t1Projects []Project
	if err := t1DB.Find(&t1Projects).Error; err != nil {
		t.Fatalf("find projects t1 failed: %v", err)
	}
	if len(t1Projects) != 1 || t1Projects[0].Name != "Project Alpha" {
		t.Errorf("expected 1 project (Project Alpha) for t1, got: %v", t1Projects)
	}
}

func TestScopeSchema_Security(t *testing.T) {
	db := openTestDB(t)
	m := New(Config{
		Strategy:  StrategySchema,
		DefaultDB: db,
	})

	// Valid schemas should pass validation (but fail on sqlite SET search_path syntax)
	valid := []string{"tenant_1", "t_123", "a", "my_tenant_schema"}
	for _, v := range valid {
		_, err := m.ScopeDB(&Tenant{ID: "1", Schema: v})
		if err == nil {
			t.Errorf("expected sqlite syntax error, got nil")
		} else if strings.Contains(err.Error(), "invalid schema name") {
			t.Errorf("expected valid schema name %q to pass validation, but got: %v", v, err)
		}
	}

	// Invalid schemas (potential SQL injections) should be rejected immediately
	invalid := []string{
		"tenant; DROP TABLE users;",
		"tenant1 --",
		"tenant1' OR '1'='1",
		"tenant-1",
	}
	for _, v := range invalid {
		_, err := m.ScopeDB(&Tenant{ID: v, Schema: v})
		if err == nil {
			t.Errorf("expected error for invalid schema name %q, got nil", v)
		} else if !strings.Contains(err.Error(), "invalid schema name") {
			t.Errorf("expected invalid schema name error for %q, got: %v", v, err)
		}
	}
}

func TestScopeDatabase(t *testing.T) {
	db1 := openTestDB(t)
	db2 := openTestDB(t)

	resolvedCount := 0
	m := New(Config{
		Strategy: StrategyDatabase,
		DBResolver: func(tenant *Tenant) (*lucid.DB, error) {
			resolvedCount++
			if tenant.ID == "t1" {
				return db1, nil
			}
			return db2, nil
		},
	})

	t1DB1, err := m.ScopeDB(&Tenant{ID: "t1"})
	if err != nil || t1DB1 != db1 {
		t.Errorf("expected db1 for t1, got: %v, err %v", t1DB1, err)
	}

	// Test cache - should not hit DBResolver again
	t1DB2, err := m.ScopeDB(&Tenant{ID: "t1"})
	if err != nil || t1DB2 != db1 {
		t.Errorf("expected db1 on second call, got: %v, err %v", t1DB2, err)
	}
	if resolvedCount != 1 {
		t.Errorf("expected DBResolver to be called exactly 1 time, got %d", resolvedCount)
	}
}

func TestTenancyMiddleware(t *testing.T) {
	db := openTestDB(t)
	m := New(Config{
		ResolveBy:  ResolveHeader,
		HeaderName: "X-Tenant-ID",
		DefaultDB:  db,
	})

	m.Register(&Tenant{ID: "t1", Active: true})
	m.Register(&Tenant{ID: "t2", Active: false})

	r := router.New()
	r.Use(m.Middleware())
	r.Get("/test", func(c *nhttp.Context) error {
		tenant := Current(c)
		tenantDB := DB(c)
		tenantID := ID(c)
		c.JSON(200, map[string]any{
			"id":         tenantID,
			"active":     tenant.Active,
			"db_not_nil": tenantDB != nil,
		})
		return nil
	})

	// 1. Success t1
	req1 := httptest.NewRequest("GET", "/test", nil)
	req1.Header.Set("X-Tenant-ID", "t1")
	rec1 := httptest.NewRecorder()
	r.ServeHTTP(rec1, req1)
	if rec1.Code != 200 {
		t.Errorf("expected status 200, got %d", rec1.Code)
	}
	var resp1 map[string]any
	_ = json.Unmarshal(rec1.Body.Bytes(), &resp1)
	if resp1["id"] != "t1" || resp1["db_not_nil"] != true {
		t.Errorf("invalid response payload: %v", resp1)
	}

	// 2. Inactive t2
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.Header.Set("X-Tenant-ID", "t2")
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	if rec2.Code != 403 {
		t.Errorf("expected status 403, got %d", rec2.Code)
	}

	// 3. Not Found t3
	req3 := httptest.NewRequest("GET", "/test", nil)
	req3.Header.Set("X-Tenant-ID", "t3")
	rec3 := httptest.NewRecorder()
	r.ServeHTTP(rec3, req3)
	if rec3.Code != 404 {
		t.Errorf("expected status 404, got %d", rec3.Code)
	}

	// 4. Missing tenant resolution
	req4 := httptest.NewRequest("GET", "/test", nil)
	rec4 := httptest.NewRecorder()
	r.ServeHTTP(rec4, req4)
	if rec4.Code != 400 {
		t.Errorf("expected status 400, got %d", rec4.Code)
	}
}

func TestGormStore(t *testing.T) {
	db := openTestDB(t)
	store := NewGormStore(db)

	tenant := &Tenant{
		ID:     "store-tenant",
		Name:   "Store Tenant",
		Domain: "store.example.com",
		Active: true,
	}

	ctx := context.Background()

	// Save
	if err := store.Save(ctx, tenant); err != nil {
		t.Fatalf("save tenant failed: %v", err)
	}

	// FindByID
	tID, err := store.FindByID(ctx, "store-tenant")
	if err != nil || tID.Name != "Store Tenant" {
		t.Errorf("FindByID failed: got %v, err %v", tID, err)
	}

	// FindByDomain
	tDom, err := store.FindByDomain(ctx, "store.example.com")
	if err != nil || tDom.ID != "store-tenant" {
		t.Errorf("FindByDomain failed: got %v, err %v", tDom, err)
	}

	// All
	tenants, err := store.All(ctx)
	if err != nil || len(tenants) != 1 {
		t.Errorf("All failed: got %d tenants, err %v", len(tenants), err)
	}

	// Delete
	if err := store.Delete(ctx, "store-tenant"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	// Find after delete
	_, err = store.FindByID(ctx, "store-tenant")
	if err == nil {
		t.Error("expected error finding deleted tenant, got nil")
	}
}

func TestTenantPlugin_Routes(t *testing.T) {
	db := openTestDB(t)
	p := NewPlugin(Config{
		ResolveBy:  ResolveHeader,
		HeaderName: "X-Tenant-ID",
		DefaultDB:  db,
	})
	p.Manager.Register(&Tenant{ID: "t1", Name: "Tenant One", Active: true})

	r := router.New()
	p.RegisterRoutes(r)

	// List Tenants
	reqList := httptest.NewRequest("GET", "/_tenants", nil)
	recList := httptest.NewRecorder()
	r.ServeHTTP(recList, reqList)
	if recList.Code != 200 {
		t.Errorf("list tenants status got %d", recList.Code)
	}
	var list []Tenant
	_ = json.Unmarshal(recList.Body.Bytes(), &list)
	if len(list) != 1 || list[0].ID != "t1" {
		t.Errorf("list tenants returned unexpected payload: %v", list)
	}

	// Get Tenant
	reqGet := httptest.NewRequest("GET", "/_tenants/t1", nil)
	recGet := httptest.NewRecorder()
	r.ServeHTTP(recGet, reqGet)
	if recGet.Code != 200 {
		t.Errorf("get tenant status got %d", recGet.Code)
	}
	var got Tenant
	_ = json.Unmarshal(recGet.Body.Bytes(), &got)
	if got.ID != "t1" {
		t.Errorf("get tenant returned unexpected ID %q", got.ID)
	}

	// Create Tenant
	newTenant := Tenant{ID: "t2", Name: "Tenant Two"}
	body, _ := json.Marshal(newTenant)
	reqCreate := httptest.NewRequest("POST", "/_tenants", bytes.NewBuffer(body))
	recCreate := httptest.NewRecorder()
	r.ServeHTTP(recCreate, reqCreate)
	if recCreate.Code != 201 {
		t.Errorf("create tenant status got %d", recCreate.Code)
	}
	var created Tenant
	_ = json.Unmarshal(recCreate.Body.Bytes(), &created)
	if created.ID != "t2" || !created.Active {
		t.Errorf("create tenant returned unexpected payload: %v", created)
	}

	// Delete Tenant
	reqDel := httptest.NewRequest("DELETE", "/_tenants/t2", nil)
	recDel := httptest.NewRecorder()
	r.ServeHTTP(recDel, reqDel)
	if recDel.Code != 200 {
		t.Errorf("delete tenant status got %d", recDel.Code)
	}

	// Check it was deleted
	reqGetDeleted := httptest.NewRequest("GET", "/_tenants/t2", nil)
	recGetDeleted := httptest.NewRecorder()
	r.ServeHTTP(recGetDeleted, reqGetDeleted)
	if recGetDeleted.Code != 404 {
		t.Errorf("expected 404 for deleted tenant, got %d", recGetDeleted.Code)
	}
}
