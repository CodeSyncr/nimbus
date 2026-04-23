package schema

import (
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"github.com/CodeSyncr/nimbus/lucid"
)

func TestCreateTable_TimestampMarker_SQLiteUsesDatetime(t *testing.T) {
	db, err := lucid.Open(sqlite.Open(":memory:"), &lucid.Config{})
	if err != nil {
		t.Fatal(err)
	}
	s := New(db)
	if err := s.CreateTable("y2038_ts", func(tb *Table) {
		tb.String("label", 32)
		tb.Timestamps()
	}); err != nil {
		t.Fatal(err)
	}
	var ddl string
	if err := db.Raw(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'y2038_ts'`).Scan(&ddl).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToUpper(ddl), "DATETIME") {
		t.Fatalf("expected DATETIME in DDL, got: %s", ddl)
	}
}

func TestCreateTable_LegacyTimestamp_SQLiteUsesTimestampKeyword(t *testing.T) {
	db, err := lucid.Open(sqlite.Open(":memory:"), &lucid.Config{})
	if err != nil {
		t.Fatal(err)
	}
	s := New(db)
	if err := s.CreateTable("y2038_legacy", func(tb *Table) {
		tb.String("label", 32)
		tb.LegacyTimestamp("legacy_at")
	}); err != nil {
		t.Fatal(err)
	}
	var ddl string
	if err := db.Raw(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'y2038_legacy'`).Scan(&ddl).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToUpper(ddl), "TIMESTAMP") {
		t.Fatalf("expected TIMESTAMP in DDL for LegacyTimestamp, got: %s", ddl)
	}
}
