package database

import (
	"github.com/CodeSyncr/nimbus/lucid"
	"github.com/CodeSyncr/nimbus/timex"
)

// Model is a base model with ID and timestamps (AdonisJS Lucid BaseModel style).
type Model struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	CreatedAt timex.Time     `json:"created_at"`
	UpdatedAt timex.Time     `json:"updated_at"`
	// DeletedAt stays gorm.DeletedAt (via lucid.DeletedAt) so GORM soft-delete queries work unchanged.
	// It maps to DATETIME(6) / TIMESTAMPTZ via the same conventions as timex.Time in migrations.
	DeletedAt lucid.DeletedAt `gorm:"index" json:"-"`
}

// BaseModel allows embedding in app models for consistent fields.
type BaseModel = Model
