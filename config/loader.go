package config

import (
	"os"
	"strings"

	"github.com/joho/godotenv"
)

// store holds the merged config (dot-notation keys).
var store = make(map[string]any)

// defaultEnvMap maps common env var names to config keys.
var defaultEnvMap = map[string]string{
	"PORT":                    "app.port",
	"HOST":                    "app.host",
	"APP_ENV":                 "app.env",
	"APP_NAME":                "app.name",
	"APP_KEY":                 "app.key",
	"DB_DRIVER":               "database.driver",
	"DB_DSN":                  "database.dsn",
	"DB_HOST":                 "database.host",
	"DB_PORT":                 "database.port",
	"DB_USER":                 "database.user",
	"DB_PASSWORD":             "database.password",
	"DB_DATABASE":             "database.database",
	"REDIS_URL":               "redis.url",
	"QUEUE_DRIVER":            "queue.driver",
	"CACHE_DRIVER":            "cache.driver",
	"MEMCACHED_SERVERS":       "cache.memcached.servers",
	"CACHE_DYNAMO_TABLE":      "cache.dynamodb.table",
	"CLOUDFLARE_ACCOUNT_ID":   "cache.cloudflare.account_id",
	"CLOUDFLARE_NAMESPACE_ID": "cache.cloudflare.namespace_id",
	"CLOUDFLARE_API_TOKEN":    "cache.cloudflare.api_token",
}

// LoadFromEnv loads .env and merges into store. Call AddEnvMapping to customize.
func LoadFromEnv(paths ...string) {
	for _, p := range paths {
		_ = godotenv.Load(p)
	}
	if len(paths) == 0 {
		_ = godotenv.Load()
	}
	for envKey, configKey := range defaultEnvMap {
		if v := os.Getenv(envKey); v != "" {
			setByPath(store, configKey, v)
		}
	}
}

// AddEnvMapping adds env var -> config key mapping (e.g. "CUSTOM_VAR" -> "custom.key").
func AddEnvMapping(envKey, configKey string) {
	defaultEnvMap[envKey] = configKey
	if v := os.Getenv(envKey); v != "" {
		setByPath(store, configKey, v)
	}
}

// LoadAuto loads .env and populates the config store from environment variables.
func LoadAuto() error {
	LoadFromEnv()
	return nil
}

func setByPath(m map[string]any, path, value string) {
	parts := strings.Split(path, ".")
	cur := m
	for i, p := range parts[:len(parts)-1] {
		if _, ok := cur[p]; !ok {
			cur[p] = make(map[string]any)
		}
		cur = cur[p].(map[string]any)
		_ = i
	}
	cur[parts[len(parts)-1]] = value
}

func getByPath(m map[string]any, path string) (any, bool) {
	parts := strings.Split(path, ".")
	cur := any(m)
	for _, p := range parts {
		mp, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = mp[p]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}
