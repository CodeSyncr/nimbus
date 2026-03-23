package database

import (
	"database/sql"
	"fmt"
	"strings"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// CreateConfig holds the minimal information needed to create a database
// when it does not exist. It mirrors the generated config.Database struct.
type CreateConfig struct {
	Driver   string
	Host     string
	Port     string
	User     string
	Password string
	Database string
}

// CreateDatabaseIfNotExists creates the configured database when supported
// by the driver. For sqlite, this is effectively a no-op (the file is created
// automatically when connecting).
func CreateDatabaseIfNotExists(cfg CreateConfig) error {
	switch cfg.Driver {
	case "postgres", "pg":
		return createPostgresDatabase(cfg)
	case "mysql":
		return createMySQLDatabase(cfg)
	default:
		// sqlite and others: nothing to do, connecting will create the file.
		return nil
	}
}

func createPostgresDatabase(cfg CreateConfig) error {
	host := cfg.Host
	if host == "" {
		host = "localhost"
	}
	port := cfg.Port
	if port == "" {
		port = "5432"
	}
	// Connect to the default "postgres" database to create the target database.
	// Only include user/password fragments when they are non-empty so that
	// the driver can fall back to its own defaults or environment variables.
	dsn := fmt.Sprintf("host=%s port=%s dbname=postgres sslmode=disable", host, port)
	if cfg.User != "" {
		dsn += fmt.Sprintf(" user=%s", cfg.User)
	}
	if cfg.Password != "" {
		dsn += fmt.Sprintf(" password=%s", cfg.Password)
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err == nil {
		defer sqlDB.Close()
	}

	// Check if database already exists.
	var one int
	row := db.Raw("SELECT 1 FROM pg_database WHERE datname = ?", cfg.Database).Row()
	err = row.Scan(&one)
	if err == nil {
		// Row present → database exists.
		return nil
	}
	if err != nil && err != sql.ErrNoRows {
		return err
	}

	// Create database (quote name to be safe).
	name := strings.ReplaceAll(cfg.Database, `"`, `""`)
	stmt := fmt.Sprintf(`CREATE DATABASE "%s"`, name)
	return db.Exec(stmt).Error
}

func createMySQLDatabase(cfg CreateConfig) error {
	host := cfg.Host
	if host == "" {
		host = "localhost"
	}
	port := cfg.Port
	if port == "" {
		port = "3306"
	}
	// DSN without database, just server-level connection.
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/?charset=utf8mb4&parseTime=True&multiStatements=true",
		cfg.User, cfg.Password, host, port)

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err == nil {
		defer sqlDB.Close()
	}

	// CREATE DATABASE IF NOT EXISTS dbname
	name := strings.ReplaceAll(cfg.Database, "`", "``")
	stmt := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci", name)
	return db.Exec(stmt).Error
}
