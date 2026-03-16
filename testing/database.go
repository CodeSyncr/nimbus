package testing

import (
	"fmt"
	"testing"

	"gorm.io/gorm"
)

// ── Database Transaction Per Test ───────────────────────────────

// RefreshDatabase wraps a test in a database transaction that is
// rolled back after the test completes. This ensures each test
// starts with a clean database state without running migrations.
//
// Usage:
//
//	func TestCreateUser(t *testing.T) {
//	    tx := nimbustesting.RefreshDatabase(t, db)
//	    // use tx instead of db in your test
//	    tx.Create(&User{Name: "Alice"})
//	    // transaction is automatically rolled back when test ends
//	}
func RefreshDatabase(t testing.TB, db *gorm.DB) *gorm.DB {
	t.Helper()
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("failed to start transaction: %v", tx.Error)
	}
	t.Cleanup(func() {
		tx.Rollback()
	})
	return tx
}

// TruncateTables truncates the given table names. Useful for test setup
// when RefreshDatabase (transaction-per-test) is not suitable.
func TruncateTables(db *gorm.DB, tables ...string) error {
	for _, table := range tables {
		if err := db.Exec("TRUNCATE TABLE " + table + " CASCADE").Error; err != nil {
			// Fall back to DELETE for databases not supporting TRUNCATE CASCADE.
			if err2 := db.Exec("DELETE FROM " + table).Error; err2 != nil {
				return err2
			}
		}
	}
	return nil
}

// SeedDatabase runs a seeder function within test scope.
// The seeder receives a *gorm.DB (which could be a transaction).
func SeedDatabase(db *gorm.DB, seeder func(db *gorm.DB) error) error {
	return seeder(db)
}

// ── Database Assertions ─────────────────────────────────────────

// AssertDatabaseHas asserts that a record matching the given conditions exists
// in the specified table. Conditions is a map[string]any of column=value pairs.
//
//	nimbustesting.AssertDatabaseHas(t, db, "users", map[string]any{"email": "alice@example.com"})
func AssertDatabaseHas(t testing.TB, db *gorm.DB, table string, conditions map[string]any) {
	t.Helper()
	var count int64
	q := db.Table(table)
	for col, val := range conditions {
		q = q.Where(fmt.Sprintf("%s = ?", col), val)
	}
	if err := q.Count(&count).Error; err != nil {
		t.Fatalf("AssertDatabaseHas: query failed: %v", err)
	}
	if count == 0 {
		t.Errorf("AssertDatabaseHas: expected record in %q matching %v, but none found", table, conditions)
	}
}

// AssertDatabaseMissing asserts that no record matching the given conditions
// exists in the specified table.
//
//	nimbustesting.AssertDatabaseMissing(t, db, "users", map[string]any{"email": "deleted@example.com"})
func AssertDatabaseMissing(t testing.TB, db *gorm.DB, table string, conditions map[string]any) {
	t.Helper()
	var count int64
	q := db.Table(table)
	for col, val := range conditions {
		q = q.Where(fmt.Sprintf("%s = ?", col), val)
	}
	if err := q.Count(&count).Error; err != nil {
		t.Fatalf("AssertDatabaseMissing: query failed: %v", err)
	}
	if count > 0 {
		t.Errorf("AssertDatabaseMissing: expected no record in %q matching %v, but found %d", table, conditions, count)
	}
}

// AssertDatabaseCount asserts the table has exactly n records (optionally matching conditions).
//
//	nimbustesting.AssertDatabaseCount(t, db, "users", 5, nil)
//	nimbustesting.AssertDatabaseCount(t, db, "users", 2, map[string]any{"role": "admin"})
func AssertDatabaseCount(t testing.TB, db *gorm.DB, table string, expected int64, conditions map[string]any) {
	t.Helper()
	var count int64
	q := db.Table(table)
	for col, val := range conditions {
		q = q.Where(fmt.Sprintf("%s = ?", col), val)
	}
	if err := q.Count(&count).Error; err != nil {
		t.Fatalf("AssertDatabaseCount: query failed: %v", err)
	}
	if count != expected {
		t.Errorf("AssertDatabaseCount: expected %d records in %q, but found %d", expected, table, count)
	}
}

// AssertSoftDeleted asserts that a soft-deleted record exists in the table
// (i.e., deleted_at IS NOT NULL).
func AssertSoftDeleted(t testing.TB, db *gorm.DB, table string, conditions map[string]any) {
	t.Helper()
	var count int64
	q := db.Table(table).Where("deleted_at IS NOT NULL")
	for col, val := range conditions {
		q = q.Where(fmt.Sprintf("%s = ?", col), val)
	}
	if err := q.Count(&count).Error; err != nil {
		t.Fatalf("AssertSoftDeleted: query failed: %v", err)
	}
	if count == 0 {
		t.Errorf("AssertSoftDeleted: expected soft-deleted record in %q matching %v, but none found", table, conditions)
	}
}

// AssertNotSoftDeleted asserts that a record exists and is NOT soft-deleted.
func AssertNotSoftDeleted(t testing.TB, db *gorm.DB, table string, conditions map[string]any) {
	t.Helper()
	var count int64
	q := db.Table(table).Where("deleted_at IS NULL")
	for col, val := range conditions {
		q = q.Where(fmt.Sprintf("%s = ?", col), val)
	}
	if err := q.Count(&count).Error; err != nil {
		t.Fatalf("AssertNotSoftDeleted: query failed: %v", err)
	}
	if count == 0 {
		t.Errorf("AssertNotSoftDeleted: expected non-deleted record in %q matching %v, but none found", table, conditions)
	}
}

// AssertModelExists asserts that the given GORM model exists in the database.
func AssertModelExists(t testing.TB, db *gorm.DB, model any) {
	t.Helper()
	result := db.First(model)
	if result.Error != nil {
		t.Errorf("AssertModelExists: expected model to exist, but got: %v", result.Error)
	}
}

// AssertModelMissing asserts that the given GORM model does NOT exist.
func AssertModelMissing(t testing.TB, db *gorm.DB, model any) {
	t.Helper()
	result := db.First(model)
	if result.Error == nil {
		t.Errorf("AssertModelMissing: expected model to not exist, but it was found")
	}
}
