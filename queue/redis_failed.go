/*
|--------------------------------------------------------------------------
| Redis Failed Job Store (Horizon)
|--------------------------------------------------------------------------
*/

package queue

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

const redisFailedPrefix = "nimbus:horizon:failed:"
const redisFailedListKey = "nimbus:horizon:failed:list"

// RedisFailedStore stores failed jobs in Redis for Horizon dashboard.
type RedisFailedStore struct {
	client *redis.Client
}

// NewRedisFailedStore creates a failed job store using the given Redis client.
func NewRedisFailedStore(client *redis.Client) *RedisFailedStore {
	return &RedisFailedStore{client: client}
}

// Push adds a failed job to the store.
func (r *RedisFailedStore) Push(ctx context.Context, payload *JobPayload, errMsg string) error {
	rec := FailedJobRecord{
		ID:         payload.ID,
		UUID:       payload.ID,
		Queue:      payload.Queue,
		JobName:    payload.JobName,
		Payload:    payload.Payload,
		Exception:  errMsg,
		FailedAt:   time.Now().UTC(),
		Attempts:   payload.Attempts,
		MaxRetries: payload.MaxRetries,
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	key := redisFailedPrefix + payload.ID
	pipe := r.client.Pipeline()
	pipe.Set(ctx, key, data, 0)
	pipe.RPush(ctx, redisFailedListKey, payload.ID)
	_, err = pipe.Exec(ctx)
	return err
}

// List returns all failed job records (newest last).
func (r *RedisFailedStore) List(ctx context.Context) ([]FailedJobRecord, error) {
	ids, err := r.client.LRange(ctx, redisFailedListKey, 0, -1).Result()
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	var out []FailedJobRecord
	for _, id := range ids {
		key := redisFailedPrefix + id
		data, err := r.client.Get(ctx, key).Bytes()
		if err != nil {
			continue
		}
		var rec FailedJobRecord
		if json.Unmarshal(data, &rec) != nil {
			continue
		}
		out = append(out, rec)
	}
	return out, nil
}

// Get returns one record by ID.
func (r *RedisFailedStore) Get(ctx context.Context, id string) (*FailedJobRecord, error) {
	data, err := r.client.Get(ctx, redisFailedPrefix+id).Bytes()
	if err != nil {
		return nil, err
	}
	var rec FailedJobRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

// Forget removes a single failed job.
func (r *RedisFailedStore) Forget(ctx context.Context, id string) error {
	key := redisFailedPrefix + id
	pipe := r.client.Pipeline()
	pipe.Del(ctx, key)
	pipe.LRem(ctx, redisFailedListKey, 0, id)
	_, err := pipe.Exec(ctx)
	return err
}

// ForgetAll removes all failed jobs.
func (r *RedisFailedStore) ForgetAll(ctx context.Context) error {
	ids, err := r.client.LRange(ctx, redisFailedListKey, 0, -1).Result()
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	pipe := r.client.Pipeline()
	for _, id := range ids {
		pipe.Del(ctx, redisFailedPrefix+id)
	}
	pipe.Del(ctx, redisFailedListKey)
	_, err = pipe.Exec(ctx)
	return err
}

// Retry re-enqueues the job and removes it from the failed store.
func (r *RedisFailedStore) Retry(ctx context.Context, id string, enqueue func(ctx context.Context, payload *JobPayload) error) error {
	rec, err := r.Get(ctx, id)
	if err != nil {
		return err
	}
	maxRetries := rec.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}
	payload := &JobPayload{
		ID:         rec.ID,
		JobName:    rec.JobName,
		Queue:      rec.Queue,
		Payload:    rec.Payload,
		Attempts:   0,
		MaxRetries: maxRetries,
		RunAt:      time.Now(),
	}
	if err := enqueue(ctx, payload); err != nil {
		return err
	}
	return r.Forget(ctx, id)
}

var _ FailedJobStore = (*RedisFailedStore)(nil)
