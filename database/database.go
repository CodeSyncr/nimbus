package database

import (
	"log"
	"os"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DB is the global GORM instance (AdonisJS Lucid-style; in production use DI).
var DB *gorm.DB

// ConnectConfig holds options for Connect.
type ConnectConfig struct {
	Driver string
	DSN    string
	// Debug enables SQL logging (pretty-print in development).
	Debug bool
}

// Connect opens a connection based on driver and DSN (from config).
func Connect(driver, dsn string) (*gorm.DB, error) {
	return ConnectWithConfig(ConnectConfig{Driver: driver, DSN: dsn})
}

// ConnectWithConfig opens a connection with full config.
func ConnectWithConfig(cfg ConnectConfig) (*gorm.DB, error) {
	var dialector gorm.Dialector
	switch cfg.Driver {
	case "postgres", "pg":
		dialector = postgres.Open(cfg.DSN)
	case "mysql":
		dialector = mysql.Open(cfg.DSN)
	case "sqlite", "":
		dialector = sqlite.Open(cfg.DSN)
	default:
		dialector = sqlite.Open(cfg.DSN)
	}

	gormConfig := &gorm.Config{}
	if cfg.Debug || os.Getenv("APP_ENV") == "development" {
		gormConfig.Logger = logger.Default.LogMode(logger.Info)
	}

	db, err := gorm.Open(dialector, gormConfig)
	if err != nil {
		return nil, err
	}

	// Register the Nimbus events plugin to broadcast DB operations
	_ = db.Use(&eventPlugin{})

	DB = db
	return db, nil
}

// Get returns the global DB (panic if not connected).
func Get() *gorm.DB {
	return DB
}

// Debug enables query logging for the global DB.
func Debug() {
	if DB != nil {
		DB = DB.Debug()
	}
}

// PrettyPrintQueries logs SQL to stdout (development).
func PrettyPrintQueries() {
	if DB != nil {
		DB = DB.Debug()
		log.Println("[database] Query debugging enabled")
	}
}
