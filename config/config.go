package config

import (
	"os"

	"github.com/joho/godotenv"
)

// Config holds application and provider configs (AdonisJS config/ style).
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

// Load reads .env and builds Config (convention: config/*).
// For type-safe config, use Get[T], LoadInto, or LoadAuto.
func Load() *Config {
	_ = godotenv.Load()

	return &Config{
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
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
