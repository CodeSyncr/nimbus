package cache

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

const redisPrefix = "nimbus:cache:"

// RedisStore uses Redis for distributed caching.
type RedisStore struct {
	client *redis.Client
	prefix string
}

// NewRedisStore creates a Redis cache store.
func NewRedisStore(client *redis.Client) *RedisStore {
	return &RedisStore{client: client, prefix: redisPrefix}
}

// NewRedisStoreWithPrefix creates a Redis store with a custom key prefix.
func NewRedisStoreWithPrefix(client *redis.Client, prefix string) *RedisStore {
	return &RedisStore{client: client, prefix: prefix}
}

// Set stores a value. Values are JSON-serialized.
func (r *RedisStore) Set(key string, value any, ttl time.Duration) error {
	ctx := context.Background()
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	k := r.prefix + key
	if ttl > 0 {
		return r.client.Set(ctx, k, data, ttl).Err()
	}
	return r.client.Set(ctx, k, data, 0).Err()
}

// Get returns the value and true if found.
func (r *RedisStore) Get(key string) (any, bool) {
	ctx := context.Background()
	data, err := r.client.Get(ctx, r.prefix+key).Bytes()
	if err == redis.Nil {
		return nil, false
	}
	if err != nil {
		return nil, false
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, false
	}
	return v, true
}

// Delete removes a key.
func (r *RedisStore) Delete(key string) error {
	return r.client.Del(context.Background(), r.prefix+key).Err()
}

// Remember returns the cached value or calls fn, stores the result, and returns it.
func (r *RedisStore) Remember(key string, ttl time.Duration, fn func() (any, error)) (any, error) {
	if v, ok := r.Get(key); ok {
		return v, nil
	}
	v, err := fn()
	if err != nil {
		return nil, err
	}
	_ = r.Set(key, v, ttl)
	return v, nil
}

// InvalidatePrefix deletes all keys with the given prefix using SCAN.
func (r *RedisStore) InvalidatePrefix(prefix string) error {
	ctx := context.Background()
	pattern := r.prefix + prefix + "*"
	iter := r.client.Scan(ctx, 0, pattern, 100).Iterator()
	for iter.Next(ctx) {
		_ = r.client.Del(ctx, iter.Val()).Err()
	}
	return iter.Err()
}

var _ Store = (*RedisStore)(nil)
var _ PrefixInvalidator = (*RedisStore)(nil)
