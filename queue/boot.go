/*
|--------------------------------------------------------------------------
| Queue Boot
|--------------------------------------------------------------------------
|
| Boot initializes the queue from env. Call from app's bin.Boot() after
| database.Connect(). Queue is core—no plugin needed.
|
*/

package queue

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/CodeSyncr/nimbus/database"
)

// BootConfig configures queue boot. Pass nil for env-based config.
type BootConfig struct {
	Driver          string // sync, redis, database, sqs, kafka
	RedisURL        string
	SQSQueueURL     string
	KafkaBrokers    string
	KafkaTopic      string
	KafkaGroupID    string
	RateLimitPerSec float64
	RateLimitBurst  int
	RegisterJobs    func()
}

// Boot initializes the queue manager from config/env and sets it globally.
func Boot(cfg *BootConfig) *Manager {
	config := BootConfig{Driver: "sync"}
	if cfg != nil {
		config = *cfg
	}
	if d := os.Getenv("QUEUE_DRIVER"); d != "" {
		config.Driver = d
	}
	if url := os.Getenv("REDIS_URL"); url != "" {
		config.RedisURL = url
	}

	var adapter Adapter
	switch config.Driver {
	case "redis":
		if config.RedisURL == "" {
			config.RedisURL = "redis://localhost:6379"
		}
		a, err := NewRedisAdapterFromURL(config.RedisURL)
		if err != nil {
			return nil
		}
		adapter = a
	case "database":
		db := database.Get()
		if db == nil {
			return nil
		}
		da := NewDatabaseAdapter(db)
		_ = da.EnsureTable(context.Background())
		adapter = da
	case "sqs":
		if config.SQSQueueURL == "" {
			config.SQSQueueURL = os.Getenv("SQS_QUEUE_URL")
		}
		if config.SQSQueueURL == "" {
			return nil
		}
		a, err := NewSQSAdapter(context.Background(), config.SQSQueueURL)
		if err != nil {
			return nil
		}
		adapter = a
	case "kafka":
		brokers := config.KafkaBrokers
		if brokers == "" {
			brokers = os.Getenv("KAFKA_BROKERS")
		}
		if brokers != "" {
			topic := config.KafkaTopic
			if topic == "" {
				topic = os.Getenv("KAFKA_TOPIC")
			}
			if topic == "" {
				topic = "nimbus-queue"
			}
			groupID := config.KafkaGroupID
			if groupID == "" {
				groupID = os.Getenv("KAFKA_GROUP_ID")
			}
			if groupID == "" {
				groupID = "nimbus-queue"
			}
			adapter = NewKafkaAdapter(KafkaConfig{
				Brokers: strings.Split(brokers, ","),
				Topic:   topic,
				GroupID: groupID,
			})
		}
	default:
		adapter = nil // Sync
	}
	if adapter != nil && config.RateLimitPerSec > 0 {
		burst := config.RateLimitBurst
		if burst <= 0 {
			burst = 10
		}
		adapter = NewRateLimitAdapter(adapter, config.RateLimitPerSec, burst)
	}
	m := NewManager(adapter)
	SetGlobal(m)
	if config.RegisterJobs != nil {
		config.RegisterJobs()
	}
	return m
}

// RunWorker runs the queue worker loop. Call from queue:work command.
func RunWorker(ctx context.Context, queueName string) {
	m := GetGlobal()
	if m == nil {
		return
	}
	if queueName == "" {
		queueName = "default"
	}
	for {
		select {
		case <-ctx.Done():
			return
		default:
			_ = m.Process(ctx, queueName)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
