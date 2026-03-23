package database

import (
	"errors"
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "migrate-test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite test db: %v", err)
	}
	return db
}

func tableExists(t *testing.T, db *gorm.DB, name string) bool {
	t.Helper()
	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name = ?", name).Scan(&count).Error; err != nil {
		t.Fatalf("check table exists: %v", err)
	}
	return count > 0
}

func TestMigratorTransactionalRollbackOnFailure(t *testing.T) {
	db := openTestDB(t)
	m := NewMigrator(db, []Migration{
		{
			Name: "202603230001_create_tx_table",
			Up: func(db *gorm.DB) error {
				if err := db.Exec("CREATE TABLE tx_table (id INTEGER PRIMARY KEY)").Error; err != nil {
					return err
				}
				return errors.New("force failure")
			},
			Down: func(db *gorm.DB) error {
				return db.Exec("DROP TABLE IF EXISTS tx_table").Error
			},
		},
	})

	if err := m.Up(); err == nil {
		t.Fatal("expected migration to fail")
	}
	if tableExists(t, db, "tx_table") {
		t.Fatal("expected tx_table to be rolled back for transactional migration")
	}
}

func TestMigratorNonTransactionalKeepsPartialDDL(t *testing.T) {
	db := openTestDB(t)
	m := NewMigrator(db, []Migration{
		{
			Name: "202603230002_create_non_tx_table",
			Up: func(db *gorm.DB) error {
				if err := db.Exec("CREATE TABLE non_tx_table (id INTEGER PRIMARY KEY)").Error; err != nil {
					return err
				}
				return errors.New("force failure")
			},
			Down: func(db *gorm.DB) error {
				return db.Exec("DROP TABLE IF EXISTS non_tx_table").Error
			},
			NonTransactional: true,
		},
	})

	if err := m.Up(); err == nil {
		t.Fatal("expected migration to fail")
	}
	if !tableExists(t, db, "non_tx_table") {
		t.Fatal("expected non_tx_table to remain after non-transactional migration failure")
	}
}
