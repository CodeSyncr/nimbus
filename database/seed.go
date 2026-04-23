package database

import "github.com/CodeSyncr/nimbus/lucid"

// Seeder runs seed data (AdonisJS database/seeders).
type Seeder interface {
	Run(db *lucid.DB) error
}

// SeedFunc adapts a function to Seeder.
type SeedFunc func(*lucid.DB) error

func (f SeedFunc) Run(db *lucid.DB) error { return f(db) }

// SeedRunner runs multiple seeders in order.
type SeedRunner struct {
	db      *lucid.DB
	seeders []Seeder
}

// NewSeedRunner creates a runner for the given seeders.
func NewSeedRunner(db *lucid.DB, seeders []Seeder) *SeedRunner {
	return &SeedRunner{db: db, seeders: seeders}
}

// Run executes all seeders.
func (r *SeedRunner) Run() error {
	for _, s := range r.seeders {
		if err := s.Run(r.db); err != nil {
			return err
		}
	}
	return nil
}
