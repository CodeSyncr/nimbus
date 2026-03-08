package database

import "gorm.io/gorm"

// Seeder runs seed data (AdonisJS database/seeders).
type Seeder interface {
	Run(db *gorm.DB) error
}

// SeedFunc adapts a function to Seeder.
type SeedFunc func(*gorm.DB) error

func (f SeedFunc) Run(db *gorm.DB) error { return f(db) }

// SeedRunner runs multiple seeders in order.
type SeedRunner struct {
	db       *gorm.DB
	seeders  []Seeder
}

// NewSeedRunner creates a runner for the given seeders.
func NewSeedRunner(db *gorm.DB, seeders []Seeder) *SeedRunner {
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
