package config

import (
	"os"

	"github.com/joho/godotenv"
)

// Config holds application and provider configs (AdonisJS config/ style).
// It is intentionally small and focused; larger applications are encouraged
// to build their own typed config structs on top using LoadAuto / LoadInto.
type Config struct {
	App      AppConfig
	Database DatabaseConfig
}

// AppConfig is app-level config (port, env, app key).
type AppConfig struct {
	Port string
	Env  string
	Name string
}

// DatabaseConfig for database connection.
type DatabaseConfig struct {
	Driver string
	DSN    string
}

// current holds the most recently loaded Config. It is populated by Load.
var current *Config

// Load reads .env and builds Config (convention: config/*).
// For type-safe config, use Get[T], LoadInto, or LoadAuto.
func Load() *Config {
	_ = godotenv.Load()

	cfg := &Config{
		App: AppConfig{
			Port: getEnv("PORT", "3333"),
			Env:  getEnv("APP_ENV", "development"),
			Name: getEnv("APP_NAME", "nimbus"),
		},
		Database: DatabaseConfig{
			Driver: getEnv("DB_DRIVER", "sqlite"),
			DSN:    getEnv("DB_DSN", "database.sqlite"),
		},
	}
	current = cfg
	return cfg
}

// Current returns the last Config loaded via Load, or nil if Load has not
// been called yet. This is primarily useful for tests and tooling that need
// to inspect the effective configuration without re-parsing the environment.
func Current() *Config {
	return current
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
