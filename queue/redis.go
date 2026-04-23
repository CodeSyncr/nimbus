package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/CodeSyncr/nimbus/redis"
)

const redisQueuePrefix = "nimbus:queue:"
const redisDelayedPrefix = "nimbus:queue:delayed:"
const redisProcessingPrefix = "nimbus:queue:processing:"
const redisInFlightPrefix = "nimbus:queue:inflight:"

// RedisAdapter uses Redis lists for job storage. Supports delayed jobs via sorted sets.
type RedisAdapter struct {
	client            *redis.Client
	visibilityTimeout time.Duration
}

// NewRedisAdapter creates a Redis adapter. Pass a configured redis.Client.
func NewRedisAdapter(client *redis.Client) *RedisAdapter {
	return &RedisAdapter{
		client:            client,
		visibilityTimeout: 60 * time.Second,
	}
}

// NewRedisAdapterFromURL creates adapter from REDIS_URL (e.g. redis://localhost:6379).
func NewRedisAdapterFromURL(url string) (*RedisAdapter, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	return &RedisAdapter{
		client:            redis.NewClient(opt),
		visibilityTimeout: 60 * time.Second,
	}, nil
}

// SetVisibilityTimeout sets the in-flight lease before jobs are reclaimed.
func (r *RedisAdapter) SetVisibilityTimeout(v time.Duration) {
	if v > 0 {
		r.visibilityTimeout = v
	}
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
	processingKey := redisProcessingPrefix + queue
	inFlightKey := redisInFlightPrefix + queue

	for {
		// Reclaim expired in-flight jobs back to the main queue.
		now := time.Now().Unix()
		expired, err := r.client.ZRangeByScore(ctx, inFlightKey, &redis.ZRangeBy{
			Min: "-inf",
			Max: fmt.Sprintf("%d", now),
		}).Result()
		if err != nil && err != redis.Nil {
			return nil, err
		}
		for _, raw := range expired {
			_ = r.client.LRem(ctx, processingKey, 1, raw).Err()
			_ = r.client.RPush(ctx, key, raw).Err()
			_ = r.client.ZRem(ctx, inFlightKey, raw).Err()
		}
		if len(expired) > 0 {
			notifyReclaimed(queue, len(expired))
		}

		// Move ready delayed jobs to main queue
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
		raw, err := r.client.BRPopLPush(ctx, key, processingKey, 2*time.Second).Result()
		if err != nil {
			if err == redis.Nil {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				default:
					continue
				}
			}
			return nil, err
		}
		var p JobPayload
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			_ = r.client.LRem(ctx, processingKey, 1, raw).Err()
			continue // skip malformed
		}
		if p.Meta == nil {
			p.Meta = make(map[string]interface{})
		}
		p.Meta["redis_processing_key"] = processingKey
		p.Meta["redis_inflight_key"] = inFlightKey
		p.Meta["redis_raw_payload"] = raw
		_ = r.client.ZAdd(ctx, inFlightKey, redis.Z{
			Score:  float64(time.Now().Add(r.visibilityTimeout).Unix()),
			Member: raw,
		}).Err()
		return &p, nil
	}
}

// Len returns the number of pending jobs.
func (r *RedisAdapter) Len(ctx context.Context, queue string) (int, error) {
	n, err := r.client.LLen(ctx, redisQueuePrefix+queue).Result()
	return int(n), err
}

// Complete acknowledges and removes a processed in-flight Redis message.
func (r *RedisAdapter) Complete(ctx context.Context, payload *JobPayload) error {
	if payload == nil || payload.Meta == nil {
		return nil
	}
	processingKey, _ := payload.Meta["redis_processing_key"].(string)
	inFlightKey, _ := payload.Meta["redis_inflight_key"].(string)
	raw, _ := payload.Meta["redis_raw_payload"].(string)
	if processingKey == "" || inFlightKey == "" || raw == "" {
		return nil
	}
	if err := r.client.LRem(ctx, processingKey, 1, raw).Err(); err != nil {
		return err
	}
	return r.client.ZRem(ctx, inFlightKey, raw).Err()
}

var _ Adapter = (*RedisAdapter)(nil)
var _ CompletableAdapter = (*RedisAdapter)(nil)
