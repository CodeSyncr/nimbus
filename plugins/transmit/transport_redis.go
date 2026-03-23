/*
|--------------------------------------------------------------------------
| Transmit Redis Transport
|--------------------------------------------------------------------------
|
| Uses Redis Pub/Sub to sync broadcasts across instances.
| Env: TRANSMIT_TRANSPORT=redis, REDIS_URL, TRANSMIT_REDIS_CHANNEL (default transmit::broadcast)
|
*/

package transmit

import (
	"context"
	"encoding/json"
	"os"
	"sync"

	"github.com/redis/go-redis/v9"
)

const defaultRedisChannel = "transmit::broadcast"

// RedisTransport uses Redis Pub/Sub for multi-instance sync.
type RedisTransport struct {
	client  *redis.Client
	channel string
	pubsub  *redis.PubSub
	mu      sync.Mutex
}

// RedisTransportConfig configures the Redis transport.
type RedisTransportConfig struct {
	URL     string // redis://localhost:6379
	Channel string // default transmit::broadcast
}

// NewRedisTransport creates a Redis transport.
func NewRedisTransport(cfg RedisTransportConfig) (*RedisTransport, error) {
	url := cfg.URL
	if url == "" {
		url = os.Getenv("REDIS_URL")
	}
	if url == "" {
		url = "redis://localhost:6379"
	}
	channel := cfg.Channel
	if channel == "" {
		channel = os.Getenv("TRANSMIT_REDIS_CHANNEL")
	}
	if channel == "" {
		channel = defaultRedisChannel
	}
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	return &RedisTransport{
		client:  redis.NewClient(opt),
		channel: channel,
	}, nil
}

type transportMessage struct {
	Channel     string   `json:"channel"`
	Payload     any      `json:"payload"`
	ExcludeUIDs []string `json:"exclude_uids,omitempty"`
}

// Publish publishes to Redis; all subscribers (including self) receive.
func (t *RedisTransport) Publish(ctx context.Context, channel string, payload any, excludeUIDs []string) error {
	msg := transportMessage{Channel: channel, Payload: payload, ExcludeUIDs: excludeUIDs}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return t.client.Publish(ctx, t.channel, data).Err()
}

// Subscribe subscribes to the Redis channel and invokes onMessage for each broadcast.
func (t *RedisTransport) Subscribe(ctx context.Context, onMessage func(channel string, payload any, excludeUIDs []string)) error {
	t.mu.Lock()
	t.pubsub = t.client.Subscribe(ctx, t.channel)
	t.mu.Unlock()
	ch := t.pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-ch:
			if !ok {
				return nil
			}
			var tm transportMessage
			if err := json.Unmarshal([]byte(msg.Payload), &tm); err != nil {
				continue
			}
			onMessage(tm.Channel, tm.Payload, tm.ExcludeUIDs)
		}
	}
}

// Close closes the pubsub and client.
func (t *RedisTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.pubsub != nil {
		_ = t.pubsub.Close()
		t.pubsub = nil
	}
	return t.client.Close()
}
