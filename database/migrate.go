package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
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
	// Use a quiet logger so nimbus db:migrate does not spam low-level SQL logs.
	quietDB := db.Session(&gorm.Session{
		Logger: logger.Default.LogMode(logger.Error),
	})
	m := &Migrator{db: quietDB, run: migrations}
	m.sorted = make([]Migration, len(migrations))
	copy(m.sorted, migrations)
	sort.Slice(m.sorted, func(i, j int) bool { return m.sorted[i].Name < m.sorted[j].Name })
	return m
}

func (m *Migrator) ensureSchemaMigrations() error {
	switch m.db.Dialector.Name() {
	case "postgres":
		return m.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
			id BIGSERIAL PRIMARY KEY,
			name TEXT UNIQUE,
			batch INT NOT NULL,
			migration_time TIMESTAMP NOT NULL
		)`).Error
	case "mysql":
		return m.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(255) UNIQUE,
			batch INT NOT NULL,
			migration_time TIMESTAMP NOT NULL
		)`).Error
	default: // sqlite and others
		return m.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT UNIQUE,
			batch INT NOT NULL,
			migration_time DATETIME NOT NULL
		)`).Error
	}
}

func (m *Migrator) isMigrated(name string) (bool, error) {
	var count int64
	err := m.db.Raw("SELECT 1 FROM schema_migrations WHERE name = ?", name).Scan(&count).Error
	return count > 0, err
}

func (m *Migrator) recordMigration(name string, batch int) error {
	return m.db.Exec(
		"INSERT INTO schema_migrations (name, batch, migration_time) VALUES (?, ?, ?)",
		name,
		batch,
		time.Now().UTC(),
	).Error
}

// nextBatch returns the next batch number (max(batch)+1), starting from 1.
func (m *Migrator) nextBatch() (int, error) {
	var maxBatch sql.NullInt64
	if err := m.db.Raw("SELECT MAX(batch) FROM schema_migrations").Scan(&maxBatch).Error; err != nil {
		return 0, err
	}
	if !maxBatch.Valid {
		return 1, nil
	}
	return int(maxBatch.Int64) + 1, nil
}

// Up runs all pending migrations.
func (m *Migrator) Up() error {
	if err := m.ensureSchemaMigrations(); err != nil {
		return fmt.Errorf("schema_migrations: %w", err)
	}
	batch, err := m.nextBatch()
	if err != nil {
		return fmt.Errorf("schema_migrations batch: %w", err)
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
		if err := m.recordMigration(mig.Name, batch); err != nil {
			return fmt.Errorf("record migration %s: %w", mig.Name, err)
		}
		fmt.Fprintf(os.Stdout, "  %s %s%s completed%s\n", mig.Name, colorGreen, checkMark, colorReset)
	}
	return nil
}

// Down rolls back the last batch of migrations (Laravel/Adonis-style).
func (m *Migrator) Down() error {
	if err := m.ensureSchemaMigrations(); err != nil {
		return fmt.Errorf("schema_migrations: %w", err)
	}

	// Find last batch number.
	var maxBatch sql.NullInt64
	if err := m.db.Raw("SELECT MAX(batch) FROM schema_migrations").Scan(&maxBatch).Error; err != nil {
		return fmt.Errorf("schema_migrations batch: %w", err)
	}
	if !maxBatch.Valid {
		// Nothing to rollback.
		return nil
	}

	// Load migration names in this batch, most recent first.
	var applied []string
	if err := m.db.Raw("SELECT name FROM schema_migrations WHERE batch = ? ORDER BY id DESC", maxBatch.Int64).Scan(&applied).Error; err != nil {
		return fmt.Errorf("schema_migrations names: %w", err)
	}
	if len(applied) == 0 {
		return nil
	}

	// Map migration name -> Migration for quick lookup.
	migMap := make(map[string]Migration, len(m.sorted))
	for _, mig := range m.sorted {
		migMap[mig.Name] = mig
	}

	for _, name := range applied {
		mig, ok := migMap[name]
		if !ok || mig.Down == nil {
			// No matching migration in code; skip but keep row so we don't lose history.
			continue
		}
		if err := mig.Down(m.db); err != nil {
			return fmt.Errorf("rollback %s: %w", name, err)
		}
		if err := m.db.Exec("DELETE FROM schema_migrations WHERE name = ?", name).Error; err != nil {
			return fmt.Errorf("delete schema_migration %s: %w", name, err)
		}
		fmt.Fprintf(os.Stdout, "  %s %srolled back%s\n", name, colorYellow, colorReset)
	}
	return nil
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

// Fresh drops all tables and re-runs all migrations from scratch.
// Equivalent to Laravel's migrate:fresh.
func (m *Migrator) Fresh() error {
	if err := m.ensureSchemaMigrations(); err != nil {
		return fmt.Errorf("schema_migrations: %w", err)
	}

	// Get all table names.
	var tables []string
	switch m.db.Dialector.Name() {
	case "postgres":
		if err := m.db.Raw("SELECT tablename FROM pg_tables WHERE schemaname = 'public'").Scan(&tables).Error; err != nil {
			return fmt.Errorf("list tables: %w", err)
		}
	case "mysql":
		if err := m.db.Raw("SHOW TABLES").Scan(&tables).Error; err != nil {
			return fmt.Errorf("list tables: %w", err)
		}
	default: // sqlite
		if err := m.db.Raw("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tables).Error; err != nil {
			return fmt.Errorf("list tables: %w", err)
		}
	}

	// Drop all tables.
	for _, table := range tables {
		if err := m.db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %q CASCADE", table)).Error; err != nil {
			fmt.Fprintf(os.Stdout, "  %sdrop %s failed%s\n", colorRed, table, colorReset)
		} else {
			fmt.Fprintf(os.Stdout, "  %s%s dropped %s%s\n", colorYellow, crossMark, table, colorReset)
		}
	}

	fmt.Fprintf(os.Stdout, "\n  Re-running all migrations...\n\n")
	return m.Up()
}

// MigrationStatus represents the status of a single migration.
type MigrationStatus struct {
	Name  string
	Ran   bool
	Batch int
	RanAt string
}

// Status returns the status of all migrations (ran and pending).
func (m *Migrator) Status() ([]MigrationStatus, error) {
	if err := m.ensureSchemaMigrations(); err != nil {
		return nil, fmt.Errorf("schema_migrations: %w", err)
	}

	// Load all applied migrations.
	type record struct {
		Name          string
		Batch         int
		MigrationTime time.Time
	}
	var records []record
	if err := m.db.Raw("SELECT name, batch, migration_time FROM schema_migrations ORDER BY id").Scan(&records).Error; err != nil {
		return nil, fmt.Errorf("load schema_migrations: %w", err)
	}
	applied := make(map[string]record, len(records))
	for _, r := range records {
		applied[r.Name] = r
	}

	var statuses []MigrationStatus
	for _, mig := range m.sorted {
		ms := MigrationStatus{Name: mig.Name}
		if rec, ok := applied[mig.Name]; ok {
			ms.Ran = true
			ms.Batch = rec.Batch
			ms.RanAt = rec.MigrationTime.Format("2006-01-02 15:04:05")
		}
		statuses = append(statuses, ms)
	}
	return statuses, nil
}

// PrintStatus prints a formatted migration status table to stdout.
func (m *Migrator) PrintStatus() error {
	statuses, err := m.Status()
	if err != nil {
		return err
	}
	if len(statuses) == 0 {
		fmt.Fprintln(os.Stdout, "  No migrations found.")
		return nil
	}

	// Table header.
	fmt.Fprintf(os.Stdout, "\n  %-50s %-10s %-6s %s\n", "Migration", "Status", "Batch", "Ran At")
	fmt.Fprintf(os.Stdout, "  %s\n", strings.Repeat("─", 90))

	for _, s := range statuses {
		if s.Ran {
			fmt.Fprintf(os.Stdout, "  %-50s %s%-10s%s %-6d %s\n", s.Name, colorGreen, "Ran", colorReset, s.Batch, s.RanAt)
		} else {
			fmt.Fprintf(os.Stdout, "  %-50s %s%-10s%s\n", s.Name, colorYellow, "Pending", colorReset)
		}
	}
	fmt.Fprintln(os.Stdout)
	return nil
}
