package database

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gorm.io/gorm"
)

const (
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorRed    = "\033[31m"
	colorReset  = "\033[0m"
	checkMark   = "✓"
	crossMark   = "✗"
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

func (m *Migrator) ensureSchemaMigrations() error {
	return m.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY)`).Error
}

func (m *Migrator) isMigrated(name string) (bool, error) {
	var count int64
	err := m.db.Raw("SELECT 1 FROM schema_migrations WHERE name = ?", name).Scan(&count).Error
	return count > 0, err
}

func (m *Migrator) recordMigration(name string) error {
	return m.db.Exec("INSERT INTO schema_migrations (name) VALUES (?)", name).Error
}

// Up runs all pending migrations.
func (m *Migrator) Up() error {
	if err := m.ensureSchemaMigrations(); err != nil {
		return fmt.Errorf("schema_migrations: %w", err)
	}
	for _, mig := range m.sorted {
		done, err := m.isMigrated(mig.Name)
		if err != nil {
			return fmt.Errorf("check migration %s: %w", mig.Name, err)
		}
		if done {
			fmt.Fprintf(os.Stdout, "  %s %sskipped%s\n", mig.Name, colorYellow, colorReset)
			continue
		}
		if err := mig.Up(m.db); err != nil {
			fmt.Fprintf(os.Stdout, "  %s %s%s failed%s\n", mig.Name, colorRed, crossMark, colorReset)
			return fmt.Errorf("migration %s: %w", mig.Name, err)
		}
		if err := m.recordMigration(mig.Name); err != nil {
			return fmt.Errorf("record migration %s: %w", mig.Name, err)
		}
		fmt.Fprintf(os.Stdout, "  %s %s%s completed%s\n", mig.Name, colorGreen, checkMark, colorReset)
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
