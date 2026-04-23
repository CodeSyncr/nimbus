package database

import (
	"errors"
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"github.com/CodeSyncr/nimbus/lucid"
)

func openTestDB(t *testing.T) *lucid.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "migrate-test.db")
	db, err := lucid.Open(sqlite.Open(dbPath), &lucid.Config{})
	if err != nil {
		t.Fatalf("open sqlite test db: %v", err)
	}
	return db
}

func tableExists(t *testing.T, db *lucid.DB, name string) bool {
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
			Up: func(db *lucid.DB) error {
				if err := db.Exec("CREATE TABLE tx_table (id INTEGER PRIMARY KEY)").Error; err != nil {
					return err
				}
				return errors.New("force failure")
			},
			Down: func(db *lucid.DB) error {
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
			Up: func(db *lucid.DB) error {
				if err := db.Exec("CREATE TABLE non_tx_table (id INTEGER PRIMARY KEY)").Error; err != nil {
					return err
				}
				return errors.New("force failure")
			},
			Down: func(db *lucid.DB) error {
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

func TestDropTableSQL(t *testing.T) {
	t.Parallel()
	if got := dropTableSQL("postgres", `t"bl`); got != `DROP TABLE IF EXISTS "t""bl" CASCADE` {
		t.Fatalf("postgres: %q", got)
	}
	if got := dropTableSQL("mysql", "users"); got != "DROP TABLE IF EXISTS `users`" {
		t.Fatalf("mysql: %q", got)
	}
	if got := dropTableSQL("sqlite", "items"); got != "DROP TABLE IF EXISTS `items`" {
		t.Fatalf("sqlite: %q", got)
	}
}

func TestMigratorFreshSQLite(t *testing.T) {
	db := openTestDB(t)
	migs := []Migration{
		{
			Name: "202603230010_fresh_a",
			Up: func(db *lucid.DB) error {
				return db.Exec("CREATE TABLE fresh_a (id INTEGER PRIMARY KEY)").Error
			},
			Down: func(db *lucid.DB) error {
				return db.Exec("DROP TABLE IF EXISTS fresh_a").Error
			},
		},
		{
			Name: "202603230011_fresh_b",
			Up: func(db *lucid.DB) error {
				return db.Exec("CREATE TABLE fresh_b (id INTEGER PRIMARY KEY)").Error
			},
			Down: func(db *lucid.DB) error {
				return db.Exec("DROP TABLE IF EXISTS fresh_b").Error
			},
		},
	}
	m := NewMigrator(db, migs)
	if err := m.Up(); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if !tableExists(t, db, "fresh_a") || !tableExists(t, db, "fresh_b") {
		t.Fatal("expected tables after Up")
	}
	if err := m.Fresh(); err != nil {
		t.Fatalf("Fresh: %v", err)
	}
	if !tableExists(t, db, "fresh_a") || !tableExists(t, db, "fresh_b") {
		t.Fatal("expected tables re-created after Fresh")
	}
	if !tableExists(t, db, "schema_migrations") {
		t.Fatal("expected schema_migrations after Fresh")
	}
}
