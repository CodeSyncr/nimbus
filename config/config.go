package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

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
	Key  string // APP_KEY — secret backing session encryption and HMAC token signing

	// HTTP server timeouts. Zero disables a given timeout (stdlib default,
	// NOT recommended in production — a client can hold a connection open
	// indefinitely, enabling Slowloris-style resource exhaustion). Safe
	// defaults are applied by Load(); override via env.
	ReadTimeout       time.Duration // SERVER_READ_TIMEOUT
	ReadHeaderTimeout time.Duration // SERVER_READ_HEADER_TIMEOUT
	WriteTimeout      time.Duration // SERVER_WRITE_TIMEOUT
	IdleTimeout       time.Duration // SERVER_IDLE_TIMEOUT
	ShutdownTimeout   time.Duration // SERVER_SHUTDOWN_TIMEOUT
	MaxHeaderBytes    int           // SERVER_MAX_HEADER_BYTES
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
			Key:  getEnv("APP_KEY", ""),
			// Bound every phase of a request's lifecycle so a slow or
			// malicious client cannot pin a connection open forever.
			ReadTimeout:       getEnvDuration("SERVER_READ_TIMEOUT", 15*time.Second),
			ReadHeaderTimeout: getEnvDuration("SERVER_READ_HEADER_TIMEOUT", 5*time.Second),
			WriteTimeout:      getEnvDuration("SERVER_WRITE_TIMEOUT", 30*time.Second),
			IdleTimeout:       getEnvDuration("SERVER_IDLE_TIMEOUT", 120*time.Second),
			ShutdownTimeout:   getEnvDuration("SERVER_SHUTDOWN_TIMEOUT", 10*time.Second),
			MaxHeaderBytes:    getEnvInt("SERVER_MAX_HEADER_BYTES", 1<<20), // 1 MiB
		},
		Database: DatabaseConfig{
			Driver: getEnv("DB_DRIVER", "sqlite"),
			DSN:    getEnv("DB_DSN", "database.sqlite"),
		},
	}
	current = cfg
	return cfg
}

// minAppKeyLen is the minimum acceptable APP_KEY length. 32 accepts a raw
// 32-byte key as well as its hex (64 chars) or base64 (44 chars) encodings,
// while rejecting obviously weak keys.
const minAppKeyLen = 32

// IsProduction reports whether the app is running in a production environment.
func (c *Config) IsProduction() bool {
	env := strings.ToLower(strings.TrimSpace(c.App.Env))
	return env == "production" || env == "prod"
}

// Validate performs fail-closed runtime validation of the loaded config.
//
// In production it returns an error when APP_KEY is missing or too short —
// booting with an empty/weak key would make session encryption and HMAC token
// signing predictable. Outside production a missing key is tolerated (dev
// convenience) and reported via the returned warnings slice instead.
//
// Callers should treat a non-nil error as fatal (App.Boot does).
func (c *Config) Validate() (warnings []string, err error) {
	keyLen := len(strings.TrimSpace(c.App.Key))

	if c.IsProduction() {
		switch {
		case keyLen == 0:
			return nil, fmt.Errorf("config: APP_KEY is required in production but is not set (generate one with 32+ random bytes)")
		case keyLen < minAppKeyLen:
			return nil, fmt.Errorf("config: APP_KEY is too short (%d chars); production requires at least %d for secure session encryption and token signing", keyLen, minAppKeyLen)
		}
		return nil, nil
	}

	switch {
	case keyLen == 0:
		warnings = append(warnings, "APP_KEY is not set — session encryption and signed tokens use a predictable key. Set APP_KEY before deploying to production.")
	case keyLen < minAppKeyLen:
		warnings = append(warnings, fmt.Sprintf("APP_KEY is short (%d chars); use at least %d random bytes before deploying to production.", keyLen, minAppKeyLen))
	}
	return warnings, nil
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

// getEnvDuration parses a Go duration (e.g. "15s", "2m") from env, falling
// back to the default when unset or unparseable. A value of "0" explicitly
// disables the timeout.
func getEnvDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}

// getEnvInt parses an integer from env, falling back to the default when unset
// or unparseable.
func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
