package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

const redisSessionPrefix = "nimbus:session:"

// RedisStore stores sessions in Redis. Suitable for multi-instance deployments.
// Values are JSON-encoded maps with a TTL based on maxAge.
type RedisStore struct {
	client *redis.Client
	prefix string
}

// NewRedisStore creates a Redis-backed session store with the default prefix.
func NewRedisStore(client *redis.Client) *RedisStore {
	return &RedisStore{client: client, prefix: redisSessionPrefix}
}

// NewRedisStoreWithPrefix creates a Redis-backed session store with a custom key prefix.
func NewRedisStoreWithPrefix(client *redis.Client, prefix string) *RedisStore {
	if prefix == "" {
		prefix = redisSessionPrefix
	}
	return &RedisStore{client: client, prefix: prefix}
}

func (s *RedisStore) key(id string) string {
	return s.prefix + id
}

// Get retrieves session data by ID. Returns nil map if not found or on error.
func (s *RedisStore) Get(ctx context.Context, id string) (map[string]any, error) {
	if id == "" {
		return nil, nil
	}
	data, err := s.client.Get(ctx, s.key(id)).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, nil
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, nil
	}
	return out, nil
}

// Set stores session data with the given maxAge. If id is empty, a new ID is generated.
func (s *RedisStore) Set(ctx context.Context, id string, data map[string]any, maxAge time.Duration) (string, error) {
	if id == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		id = hex.EncodeToString(b)
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	ttl := maxAge
	if ttl <= 0 {
		// default to 24h if maxAge is not positive
		ttl = 24 * time.Hour
	}
	if err := s.client.Set(ctx, s.key(id), payload, ttl).Err(); err != nil {
		return "", err
	}
	return id, nil
}

// Destroy removes the session.
func (s *RedisStore) Destroy(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	return s.client.Del(ctx, s.key(id)).Err()
}

var _ Store = (*RedisStore)(nil)
