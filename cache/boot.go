package cache

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/redis/go-redis/v9"
)

// BootConfig configures cache boot. Pass nil for env-based config.
type BootConfig struct {
	Driver        string        // memory, redis, memcached, dynamodb, cloudflare
	RedisURL      string
	MemcachedServers string    // comma-separated, e.g. "localhost:11211"
	DynamoTable   string
	DynamoRegion string
	CloudflareAccountID   string
	CloudflareNamespaceID string
	CloudflareAPIToken    string
	DefaultTTL   time.Duration
}

var (
	globalStore Store
	globalMu    sync.RWMutex
)

// Boot initializes the cache from config/env and sets it globally.
func Boot(cfg *BootConfig) Store {
	config := BootConfig{Driver: "memory"}
	if cfg != nil {
		config = *cfg
	}
	if d := os.Getenv("CACHE_DRIVER"); d != "" {
		config.Driver = d
	}
	if url := os.Getenv("REDIS_URL"); url != "" {
		config.RedisURL = url
	}
	if s := os.Getenv("MEMCACHED_SERVERS"); s != "" {
		config.MemcachedServers = s
	}
	if t := os.Getenv("CACHE_DYNAMO_TABLE"); t != "" {
		config.DynamoTable = t
	}
	if r := os.Getenv("AWS_REGION"); r != "" {
		config.DynamoRegion = r
	}
	if a := os.Getenv("CLOUDFLARE_ACCOUNT_ID"); a != "" {
		config.CloudflareAccountID = a
	}
	if n := os.Getenv("CLOUDFLARE_NAMESPACE_ID"); n != "" {
		config.CloudflareNamespaceID = n
	}
	if t := os.Getenv("CLOUDFLARE_API_TOKEN"); t != "" {
		config.CloudflareAPIToken = t
	}

	var store Store
	switch config.Driver {
	case "redis":
		if config.RedisURL == "" {
			config.RedisURL = "redis://localhost:6379"
		}
		opt, err := redis.ParseURL(config.RedisURL)
		if err != nil {
			return nil
		}
		store = NewRedisStore(redis.NewClient(opt))
	case "memcached":
		servers := strings.Split(config.MemcachedServers, ",")
		for i, s := range servers {
			servers[i] = strings.TrimSpace(s)
		}
		if len(servers) == 0 || servers[0] == "" {
			servers = []string{"localhost:11211"}
		}
		store = NewMemcachedStore(servers...)
	case "dynamodb":
		ctx := context.Background()
		opts := []func(*awsconfig.LoadOptions) error{}
		if config.DynamoRegion != "" {
			opts = append(opts, awsconfig.WithRegion(config.DynamoRegion))
		}
		awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
		if err != nil {
			return nil
		}
		table := config.DynamoTable
		if table == "" {
			table = "nimbus-cache"
		}
		store = NewDynamoDBStore(awsCfg, table)
	case "cloudflare":
		if config.CloudflareAccountID == "" || config.CloudflareNamespaceID == "" || config.CloudflareAPIToken == "" {
			return nil
		}
		store = NewCloudflareKVStore(config.CloudflareAccountID, config.CloudflareNamespaceID, config.CloudflareAPIToken)
	default:
		store = NewMemoryStore()
	}

	SetGlobal(store)
	return store
}

// SetGlobal sets the global cache store.
func SetGlobal(s Store) {
	globalMu.Lock()
	defer globalMu.Unlock()
	globalStore = s
}

// GetGlobal returns the global cache store.
func GetGlobal() Store {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return globalStore
}
