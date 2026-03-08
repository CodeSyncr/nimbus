package database

import (
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// DB is the global GORM instance (AdonisJS Lucid-style; in production use DI).
var DB *gorm.DB

// Connect opens a connection based on driver and DSN (from config).
func Connect(driver, dsn string) (*gorm.DB, error) {
	var dialector gorm.Dialector
	switch driver {
	case "sqlite", "":
		dialector = sqlite.Open(dsn)
	default:
		// Add mysql/postgres when needed:
		// dialector = mysql.Open(dsn)
		dialector = sqlite.Open(dsn)
	}
	db, err := gorm.Open(dialector, &gorm.Config{})
	if err != nil {
		return nil, err
	}
	DB = db
	return db, nil
}

// Get returns the global DB (panic if not connected).
func Get() *gorm.DB {
	return DB
}
