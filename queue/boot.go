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
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/CodeSyncr/nimbus/database"
)

// BootConfig configures queue boot. Pass nil for env-based config.
type BootConfig struct {
	Driver          string // sync, redis, database, sqs, kafka
	RedisURL        string
	// RedisVisibilityTimeout controls Redis in-flight lease timeout.
	RedisVisibilityTimeout time.Duration
	// DatabaseLeaseDuration controls how long processing DB jobs are leased before reclaim.
	DatabaseLeaseDuration time.Duration
	SQSQueueURL     string
	KafkaBrokers    string
	KafkaTopic      string
	KafkaGroupID    string
	RateLimitPerSec float64
	RateLimitBurst  int
	Strict          bool
	RegisterJobs    func()
}

// Boot initializes the queue manager from config/env and sets it globally.
func Boot(cfg *BootConfig) *Manager {
	m, err := BootWithError(cfg)
	if err != nil {
		log.Printf("[queue] boot failed: %v", err)
		return nil
	}
	return m
}

// BootWithError initializes the queue manager from config/env and returns an
// explicit error when configuration/adapter setup fails.
func BootWithError(cfg *BootConfig) (*Manager, error) {
	config := BootConfig{Driver: "sync"}
	if cfg != nil {
		config = *cfg
	}
	if v := os.Getenv("QUEUE_BOOT_STRICT"); v != "" {
		config.Strict = strings.EqualFold(v, "1") || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
	}
	if d := os.Getenv("QUEUE_DRIVER"); d != "" {
		config.Driver = d
	}
	if url := os.Getenv("REDIS_URL"); url != "" {
		config.RedisURL = url
	}
	if v := os.Getenv("QUEUE_REDIS_VISIBILITY_TIMEOUT_SECONDS"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			config.RedisVisibilityTimeout = time.Duration(secs) * time.Second
		}
	}
	if v := os.Getenv("QUEUE_DB_LEASE_SECONDS"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			config.DatabaseLeaseDuration = time.Duration(secs) * time.Second
		}
	}

	var adapter Adapter
	switch config.Driver {
	case "redis":
		if config.RedisURL == "" {
			config.RedisURL = "redis://localhost:6379"
		}
		a, err := NewRedisAdapterFromURL(config.RedisURL)
		if err != nil {
			return nil, fmt.Errorf("queue: redis adapter: %w", err)
		}
		if config.RedisVisibilityTimeout > 0 {
			a.SetVisibilityTimeout(config.RedisVisibilityTimeout)
		}
		adapter = a
	case "database":
		db := database.Get()
		if db == nil {
			return nil, fmt.Errorf("queue: database driver selected but no database connection is available")
		}
		da := NewDatabaseAdapter(db)
		if config.DatabaseLeaseDuration > 0 {
			da.SetLeaseDuration(config.DatabaseLeaseDuration)
		}
		if err := da.EnsureTable(context.Background()); err != nil {
			return nil, fmt.Errorf("queue: ensure table: %w", err)
		}
		adapter = da
	case "sqs":
		if config.SQSQueueURL == "" {
			config.SQSQueueURL = os.Getenv("SQS_QUEUE_URL")
		}
		if config.SQSQueueURL == "" {
			return nil, fmt.Errorf("queue: sqs driver selected but SQS_QUEUE_URL is empty")
		}
		a, err := NewSQSAdapter(context.Background(), config.SQSQueueURL)
		if err != nil {
			return nil, fmt.Errorf("queue: sqs adapter: %w", err)
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
		if config.Strict {
			return nil, fmt.Errorf("queue: unknown driver %q", config.Driver)
		}
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
	return m, nil
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
	backoff := 100 * time.Millisecond
	for {
		select {
		case <-ctx.Done():
			return
		default:
			if err := m.Process(ctx, queueName); err != nil {
				if ctx.Err() != nil {
					return
				}
				log.Printf("[queue] worker process error (queue=%s): %v", queueName, err)
				time.Sleep(backoff)
				if backoff < 3*time.Second {
					backoff *= 2
					if backoff > 3*time.Second {
						backoff = 3 * time.Second
					}
				}
				continue
			}
			backoff = 100 * time.Millisecond
		}
		time.Sleep(100 * time.Millisecond)
	}
}
