package schedule

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/CodeSyncr/nimbus/redis"
)

// RedisLocker uses Redis SETNX + TTL for distributed task locking.
type RedisLocker struct {
	client *redis.Client
}

// NewRedisLocker creates a Redis-backed scheduler locker.
func NewRedisLocker(client *redis.Client) *RedisLocker {
	return &RedisLocker{client: client}
}

// TryLock attempts to acquire a lock for key with ttl.
func (l *RedisLocker) TryLock(ctx context.Context, key string, ttl time.Duration) (func(), bool, error) {
	if l == nil || l.client == nil {
		return nil, false, errors.New("schedule: redis locker is not configured")
	}
	token, err := randomToken()
	if err != nil {
		return nil, false, err
	}
	ok, err := l.client.SetNX(ctx, key, token, ttl).Result()
	if err != nil {
		return nil, false, err
	}
	if !ok {
		return nil, false, nil
	}

	unlock := func() {
		const script = `
if redis.call("get", KEYS[1]) == ARGV[1] then
	return redis.call("del", KEYS[1])
else
	return 0
end
`
		_ = l.client.Eval(ctx, script, []string{key}, token).Err()
	}
	return unlock, true, nil
}

func randomToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

