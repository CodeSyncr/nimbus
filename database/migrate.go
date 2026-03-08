package database

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gorm.io/gorm"
)

// Migration runs a single migration (Up/Down).
type Migration struct {
	Name string
	Up   func(*gorm.DB) error
	Down func(*gorm.DB) error
}

// Migrator runs migrations from a directory or list (AdonisJS database/migrations).
type Migrator struct {
	db     *gorm.DB
	run    []Migration
	sorted []Migration
}

// NewMigrator creates a migrator with the given migrations.
func NewMigrator(db *gorm.DB, migrations []Migration) *Migrator {
	m := &Migrator{db: db, run: migrations}
	m.sorted = make([]Migration, len(migrations))
	copy(m.sorted, migrations)
	sort.Slice(m.sorted, func(i, j int) bool { return m.sorted[i].Name < m.sorted[j].Name })
	return m
}

// Up runs all pending migrations.
func (m *Migrator) Up() error {
	for _, mig := range m.sorted {
		if err := mig.Up(m.db); err != nil {
			return fmt.Errorf("migration %s: %w", mig.Name, err)
		}
	}
	return nil
}

// Down runs the last migration's Down.
func (m *Migrator) Down() error {
	if len(m.sorted) == 0 {
		return nil
	}
	last := m.sorted[len(m.sorted)-1]
	return last.Down(m.db)
}

// RunMigrationsFromDir discovers Go files in dir and runs Up on a migrator.
// Convention: each file defines a Migration and registers via RegisterMigration.
// This is a placeholder; real usage would use go:generate or a separate migration runner.
func RunMigrationsFromDir(db *gorm.DB, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var list []Migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		// In practice, load migrations from build tags or a registry.
		_ = filepath.Join(dir, e.Name())
		list = append(list, Migration{Name: e.Name(), Up: func(*gorm.DB) error { return nil }, Down: func(*gorm.DB) error { return nil }})
	}
	NewMigrator(db, list).Up()
	return nil
}
