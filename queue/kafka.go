/*
|--------------------------------------------------------------------------
| Kafka Queue Adapter
|--------------------------------------------------------------------------
|
| Uses Apache Kafka for job persistence. Set KAFKA_BROKERS (comma-separated)
| and KAFKA_TOPIC. Supports consumer groups for distributed workers.
|
*/

package queue

import (
	"context"
	"encoding/json"

	"github.com/segmentio/kafka-go"
)

// KafkaAdapter uses Kafka for job storage.
type KafkaAdapter struct {
	writer  *kafka.Writer
	reader  *kafka.Reader
	topic   string
	groupID string
}

// KafkaConfig holds Kafka adapter configuration.
type KafkaConfig struct {
	Brokers  []string // e.g. []string{"localhost:9092"}
	Topic    string   // queue topic
	GroupID  string   // consumer group for workers
	MinBytes int      // min bytes to fetch (default 1)
	MaxBytes int      // max bytes to fetch (default 1e6)
}

// NewKafkaAdapter creates a Kafka adapter.
func NewKafkaAdapter(cfg KafkaConfig) *KafkaAdapter {
	if cfg.GroupID == "" {
		cfg.GroupID = "nimbus-queue"
	}
	if cfg.MinBytes == 0 {
		cfg.MinBytes = 1
	}
	if cfg.MaxBytes == 0 {
		cfg.MaxBytes = 1e6
	}
	w := &kafka.Writer{
		Addr:     kafka.TCP(cfg.Brokers...),
		Topic:    cfg.Topic,
		Balancer: &kafka.LeastBytes{},
	}
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:  cfg.Brokers,
		Topic:    cfg.Topic,
		GroupID:  cfg.GroupID,
		MinBytes: cfg.MinBytes,
		MaxBytes: cfg.MaxBytes,
	})
	return &KafkaAdapter{
		writer:  w,
		reader:  r,
		topic:   cfg.Topic,
		groupID: cfg.GroupID,
	}
}

// Push adds a job to the queue.
func (k *KafkaAdapter) Push(ctx context.Context, payload *JobPayload) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return k.writer.WriteMessages(ctx, kafka.Message{
		Key:   []byte(payload.Queue),
		Value: data,
	})
}

// Pop blocks until a job is available.
func (k *KafkaAdapter) Pop(ctx context.Context, queue string) (*JobPayload, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			msg, err := k.reader.ReadMessage(ctx)
			if err != nil {
				return nil, err
			}
			var p JobPayload
			if err := json.Unmarshal(msg.Value, &p); err != nil {
				continue
			}
			return &p, nil
		}
	}
}

// Len returns 0 (Kafka doesn't provide simple count).
func (k *KafkaAdapter) Len(ctx context.Context, queue string) (int, error) {
	return 0, nil
}

// Close closes the writer and reader.
func (k *KafkaAdapter) Close() error {
	_ = k.writer.Close()
	return k.reader.Close()
}

var _ Adapter = (*KafkaAdapter)(nil)
