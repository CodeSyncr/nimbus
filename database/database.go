package database

import (
	"log"
	"os"

	"github.com/CodeSyncr/nimbus/lucid"
	lucidlog "github.com/CodeSyncr/nimbus/lucid/logger"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
)

// DB is the global GORM instance (AdonisJS Lucid-style; in production use DI).
var DB *lucid.DB

// ConnectConfig holds options for Connect.
type ConnectConfig struct {
	Driver string
	DSN    string
	// Debug enables SQL logging (pretty-print in development).
	Debug bool
	PoolConfig
}

// Connect opens a connection based on driver and DSN (from config).
func Connect(driver, dsn string) (*lucid.DB, error) {
	return ConnectWithConfig(ConnectConfig{Driver: driver, DSN: dsn})
}

// openConnection opens GORM without mutating the package-global DB.
// Use ConnectWithConfig for the default connection; ConnectAll uses this
// so each iteration does not overwrite DB before AddConnection runs.
func openConnection(cfg ConnectConfig) (*lucid.DB, error) {
	var dialector lucid.Dialector
	switch cfg.Driver {
	case "postgres", "pg", "supabase":
		dialector = postgres.Open(cfg.DSN)
	case "mysql":
		dialector = mysql.Open(cfg.DSN)
	case "sqlite", "":
		dialector = sqlite.Open(cfg.DSN)
	default:
		dialector = sqlite.Open(cfg.DSN)
	}

	gormConfig := &lucid.Config{}
	if cfg.Debug || os.Getenv("APP_ENV") == "development" {
		gormConfig.Logger = lucidlog.Default.LogMode(lucidlog.Info)
	}

	db, err := lucid.Open(dialector, gormConfig)
	if err != nil {
		return nil, err
	}

	// Register the Nimbus events plugin to broadcast DB operations
	_ = db.Use(&eventPlugin{})

	if err := ApplyPool(db, cfg.PoolConfig); err != nil {
		return nil, err
	}

	return db, nil
}

// ConnectWithConfig opens a connection with full config and sets the
// package-global DB to this handle.
func ConnectWithConfig(cfg ConnectConfig) (*lucid.DB, error) {
	db, err := openConnection(cfg)
	if err != nil {
		return nil, err
	}
	DB = db
	return db, nil
}

// Get returns the global DB (panic if not connected).
func Get() *lucid.DB {
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
