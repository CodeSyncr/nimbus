package redis

import (
	"sync"

	goredis "github.com/redis/go-redis/v9"
)

// Client is Nimbus's Redis client type.
type Client = goredis.Client

// PubSub is a Redis Pub/Sub subscription handle.
type PubSub = goredis.PubSub

// Options configures Redis client initialization.
type Options = goredis.Options

// Z is a sorted-set member payload.
type Z = goredis.Z

// ZRangeBy represents score range bounds for sorted-set queries.
type ZRangeBy = goredis.ZRangeBy

// Nil is returned by Redis operations when a key/member does not exist.
var Nil = goredis.Nil

// Hook lets callers observe Redis commands (via go-redis hooks).
type Hook = goredis.Hook

var (
	hookMu        sync.RWMutex
	hookFactories []func(opt *Options) Hook
)

// RegisterHook registers a hook factory that will be attached to every client
// created via NewClient.
func RegisterHook(factory func(opt *Options) Hook) {
	if factory == nil {
		return
	}
	hookMu.Lock()
	defer hookMu.Unlock()
	hookFactories = append(hookFactories, factory)
}

// NewClient creates a Redis client.
func NewClient(opt *Options) *Client {
	c := goredis.NewClient(opt)

	hookMu.RLock()
	fns := make([]func(opt *Options) Hook, len(hookFactories))
	copy(fns, hookFactories)
	hookMu.RUnlock()

	for _, fn := range fns {
		if h := fn(opt); h != nil {
			c.AddHook(h)
		}
	}

	return c
}

// ParseURL parses a redis:// URL into client options.
func ParseURL(url string) (*Options, error) {
	return goredis.ParseURL(url)
}
