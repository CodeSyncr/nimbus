package lucid_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/CodeSyncr/nimbus/lucid"
	"github.com/CodeSyncr/nimbus/lucid/clause"
	"gorm.io/driver/sqlite"
)

type note struct {
	ID        uint   `gorm:"primaryKey"`
	Slug      string `gorm:"uniqueIndex"`
	Body      string
	DeletedAt lucid.DeletedAt
}

func open(t *testing.T) *lucid.DB {
	t.Helper()
	db, err := lucid.Open(sqlite.Open(filepath.Join(t.TempDir(), "l.db")), &lucid.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&note{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestCRUDAndErrors(t *testing.T) {
	db := open(t)
	if err := db.Create(&note{Slug: "a", Body: "one"}).Error; err != nil {
		t.Fatal(err)
	}
	var n note
	if err := db.Where("slug = ?", "a").First(&n).Error; err != nil || n.Body != "one" {
		t.Fatalf("First = %+v, %v", n, err)
	}
	if err := db.Where("slug = ?", "missing").First(&n).Error; !errors.Is(err, lucid.ErrRecordNotFound) {
		t.Fatalf("missing row: %v, want ErrRecordNotFound", err)
	}
	if err := db.Transaction(func(tx *lucid.DB) error {
		return tx.Model(&note{}).Where("slug = ?", "a").Update("body", "two").Error
	}); err != nil {
		t.Fatal(err)
	}
	db.First(&n, "slug = ?", "a")
	if n.Body != "two" {
		t.Fatalf("update in transaction not applied: %q", n.Body)
	}
}

func TestSoftDelete(t *testing.T) {
	db := open(t)
	db.Create(&note{Slug: "gone"})
	db.Where("slug = ?", "gone").Delete(&note{})
	var count int64
	db.Model(&note{}).Count(&count)
	if count != 0 {
		t.Fatalf("soft-deleted row still visible: %d", count)
	}
	db.Unscoped().Model(&note{}).Count(&count)
	if count != 1 {
		t.Fatalf("soft-deleted row should remain in the table: %d", count)
	}
}

func TestClauseUpsert(t *testing.T) {
	db := open(t)
	db.Create(&note{Slug: "k", Body: "old"})
	err := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "slug"}},
		DoUpdates: clause.AssignmentColumns([]string{"body"}),
	}).Create(&note{Slug: "k", Body: "new"}).Error
	if err != nil {
		t.Fatal(err)
	}
	var n note
	db.First(&n, "slug = ?", "k")
	if n.Body != "new" {
		t.Fatalf("upsert body = %q", n.Body)
	}
	res := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&note{Slug: "k", Body: "ignored"})
	if res.Error != nil || res.RowsAffected != 0 {
		t.Fatalf("DO NOTHING insert: rows=%d err=%v", res.RowsAffected, res.Error)
	}
}
