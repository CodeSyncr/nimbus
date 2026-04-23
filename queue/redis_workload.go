package queue

import (
	"context"
	"fmt"

	"github.com/CodeSyncr/nimbus/redis"
)

// RedisQueueWorkload holds Redis list/set sizes for one logical queue (Redis driver key layout).
type RedisQueueWorkload struct {
	Name        string `json:"name"`
	Pending     int64  `json:"pending"`     // jobs waiting in the main list
	Delayed     int64  `json:"delayed"`     // jobs in delayed sorted set
	Processing  int64  `json:"processing"`  // jobs leased to workers
	InFlight    int64  `json:"in_flight"`   // in-flight lease tracking (sorted set members)
}

// RedisQueueWorkloads returns live depth metrics from Redis for the given queue names.
// Uses the same key prefixes as RedisAdapter. Pass nil or empty names to probe only "default".
func RedisQueueWorkloads(ctx context.Context, c *redis.Client, names []string) ([]RedisQueueWorkload, error) {
	if c == nil {
		return nil, fmt.Errorf("queue: redis client is nil")
	}
	if len(names) == 0 {
		names = []string{"default"}
	}
	out := make([]RedisQueueWorkload, 0, len(names))
	for _, q := range names {
		if q == "" {
			q = "default"
		}
		key := redisQueuePrefix + q
		delayedKey := redisDelayedPrefix + q
		processingKey := redisProcessingPrefix + q
		inFlightKey := redisInFlightPrefix + q

		pending, err := c.LLen(ctx, key).Result()
		if err != nil && err != redis.Nil {
			return nil, err
		}
		delayed, err := c.ZCard(ctx, delayedKey).Result()
		if err != nil && err != redis.Nil {
			return nil, err
		}
		processing, err := c.LLen(ctx, processingKey).Result()
		if err != nil && err != redis.Nil {
			return nil, err
		}
		inFlight, err := c.ZCard(ctx, inFlightKey).Result()
		if err != nil && err != redis.Nil {
			return nil, err
		}
		out = append(out, RedisQueueWorkload{
			Name:       q,
			Pending:    pending,
			Delayed:    delayed,
			Processing: processing,
			InFlight:   inFlight,
		})
	}
	return out, nil
}
