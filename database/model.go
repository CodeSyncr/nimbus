package database

import (
	"time"

	"gorm.io/gorm"
)

// Model is a base model with ID and timestamps (AdonisJS Lucid BaseModel style).
type Model struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// BaseModel allows embedding in app models for consistent fields.
type BaseModel = Model
