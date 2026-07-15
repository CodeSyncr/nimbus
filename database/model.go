package database

import (
	"time"

	"gorm.io/gorm"

	"github.com/CodeSyncr/nimbus/lucid"
	"github.com/CodeSyncr/nimbus/timex"
)

// Model is a base model with ID and timestamps (AdonisJS Lucid BaseModel style).
type Model struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	CreatedAt timex.Time     `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt timex.Time     `gorm:"autoUpdateTime" json:"updated_at"`
	// DeletedAt stays gorm.DeletedAt (via lucid.DeletedAt) so GORM soft-delete queries work unchanged.
	// It maps to DATETIME(6) / TIMESTAMPTZ via the same conventions as timex.Time in migrations.
	DeletedAt lucid.DeletedAt `gorm:"index" json:"-"`
}

// BaseModel allows embedding in app models for consistent fields.
type BaseModel = Model

// BeforeCreate GORM hook to set CreatedAt and UpdatedAt timestamps if they are zero.
func (m *Model) BeforeCreate(tx *gorm.DB) error {
	now := timex.New(time.Now())
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}
	if m.UpdatedAt.IsZero() {
		m.UpdatedAt = now
	}
	return nil
}

// BeforeUpdate GORM hook to set UpdatedAt timestamp.
func (m *Model) BeforeUpdate(tx *gorm.DB) error {
	m.UpdatedAt = timex.New(time.Now())
	return nil
}


