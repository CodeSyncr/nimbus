package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const redisQueuePrefix = "nimbus:queue:"
const redisDelayedPrefix = "nimbus:queue:delayed:"

// RedisAdapter uses Redis lists for job storage. Supports delayed jobs via sorted sets.
type RedisAdapter struct {
	client *redis.Client
}

// NewRedisAdapter creates a Redis adapter. Pass a configured redis.Client.
func NewRedisAdapter(client *redis.Client) *RedisAdapter {
	return &RedisAdapter{client: client}
}

// NewRedisAdapterFromURL creates adapter from REDIS_URL (e.g. redis://localhost:6379).
func NewRedisAdapterFromURL(url string) (*RedisAdapter, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	return &RedisAdapter{client: redis.NewClient(opt)}, nil
}

// Push adds a job to the queue.
func (r *RedisAdapter) Push(ctx context.Context, payload *JobPayload) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	key := redisQueuePrefix + payload.Queue
	if payload.Delay > 0 {
		score := float64(time.Now().Add(payload.Delay).Unix())
		return r.client.ZAdd(ctx, redisDelayedPrefix+payload.Queue, redis.Z{Score: score, Member: data}).Err()
	}
	return r.client.RPush(ctx, key, data).Err()
}

// Pop blocks until a job is available. Processes delayed jobs when ready.
func (r *RedisAdapter) Pop(ctx context.Context, queue string) (*JobPayload, error) {
	key := redisQueuePrefix + queue
	delayedKey := redisDelayedPrefix + queue

	for {
		// Move ready delayed jobs to main queue
		now := time.Now().Unix()
		vals, err := r.client.ZRangeByScore(ctx, delayedKey, &redis.ZRangeBy{
			Min: "-inf",
			Max: fmt.Sprintf("%d", now+1),
		}).Result()
		if err != nil && err != redis.Nil {
			return nil, err
		}
		for _, v := range vals {
			_ = r.client.ZRem(ctx, delayedKey, v).Err()
			_ = r.client.RPush(ctx, key, v).Err()
		}

		// Block until job available (2s timeout to recheck delayed)
		result, err := r.client.BLPop(ctx, 2*time.Second, key).Result()
		if err != nil {
			return nil, err
		}
		if len(result) < 2 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
				continue
			}
		}
		var p JobPayload
		if err := json.Unmarshal([]byte(result[1]), &p); err != nil {
			continue // skip malformed
		}
		return &p, nil
	}
}

// Len returns the number of pending jobs.
func (r *RedisAdapter) Len(ctx context.Context, queue string) (int, error) {
	n, err := r.client.LLen(ctx, redisQueuePrefix+queue).Result()
	return int(n), err
}

var _ Adapter = (*RedisAdapter)(nil)
