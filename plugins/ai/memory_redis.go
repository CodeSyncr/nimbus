/*
|--------------------------------------------------------------------------
| AI SDK — Redis & Database Memory Backends
|--------------------------------------------------------------------------
|
| Production-grade memory backends for agent conversation persistence.
|
|   // Redis memory
|   rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
|   agent.WithMemory(ai.RedisMemory(rdb), "session:user123")
|
|   // Database memory (GORM)
|   agent.WithMemory(ai.DatabaseMemory(db), "session:user123")
|
*/

package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// Redis memory backend
// ---------------------------------------------------------------------------

type redisMemory struct {
	client *redis.Client
	prefix string
	ttl    time.Duration
}

// RedisMemoryOption configures the Redis memory backend.
type RedisMemoryOption func(*redisMemory)

// WithRedisPrefix sets the key prefix (default "ai:memory:").
func WithRedisPrefix(prefix string) RedisMemoryOption {
	return func(m *redisMemory) { m.prefix = prefix }
}

// WithRedisTTL sets per-key expiration in Redis.
func WithRedisTTL(ttl time.Duration) RedisMemoryOption {
	return func(m *redisMemory) { m.ttl = ttl }
}

// RedisMemory creates a Redis-backed memory store. Conversation history
// is stored as JSON in Redis keys with configurable TTL and prefix.
//
//	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
//	mem := ai.RedisMemory(rdb, ai.WithRedisTTL(24*time.Hour))
//	agent.WithMemory(mem, "session:user123")
func RedisMemory(client *redis.Client, opts ...RedisMemoryOption) Memory {
	m := &redisMemory{
		client: client,
		prefix: "ai:memory:",
		ttl:    0, // no expiration by default
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

func (m *redisMemory) key(k string) string {
	return m.prefix + k
}

func (m *redisMemory) Load(ctx context.Context, key string) ([]Message, error) {
	data, err := m.client.Get(ctx, m.key(key)).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ai: redis memory load: %w", err)
	}

	var msgs []Message
	if err := json.Unmarshal(data, &msgs); err != nil {
		return nil, fmt.Errorf("ai: redis memory unmarshal: %w", err)
	}
	return msgs, nil
}

func (m *redisMemory) Save(ctx context.Context, key string, messages []Message) error {
	data, err := json.Marshal(messages)
	if err != nil {
		return fmt.Errorf("ai: redis memory marshal: %w", err)
	}

	if m.ttl > 0 {
		return m.client.Set(ctx, m.key(key), data, m.ttl).Err()
	}
	return m.client.Set(ctx, m.key(key), data, 0).Err()
}

func (m *redisMemory) Clear(ctx context.Context, key string) error {
	return m.client.Del(ctx, m.key(key)).Err()
}

// ---------------------------------------------------------------------------
// Database memory backend (GORM)
// ---------------------------------------------------------------------------

// ConversationRecord is the GORM model for storing conversation history.
type ConversationRecord struct {
	ID        uint      `gorm:"primarykey"`
	Key       string    `gorm:"index;size:255;not null"`
	Messages  string    `gorm:"type:text;not null"` // JSON-encoded []Message
	CreatedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

// TableName returns the database table name.
func (ConversationRecord) TableName() string {
	return "ai_conversations"
}

type databaseMemory struct {
	db          *gorm.DB
	autoMigrate bool
	migrated    bool
}

// DatabaseMemoryOption configures the database memory backend.
type DatabaseMemoryOption func(*databaseMemory)

// WithAutoMigrate enables automatic table creation (default: true).
func WithAutoMigrate(enabled bool) DatabaseMemoryOption {
	return func(m *databaseMemory) { m.autoMigrate = enabled }
}

// DatabaseMemory creates a database-backed memory store using GORM.
// Conversation history is persisted in an `ai_conversations` table.
//
//	mem := ai.DatabaseMemory(db)
//	agent.WithMemory(mem, "session:user123")
func DatabaseMemory(db *gorm.DB, opts ...DatabaseMemoryOption) Memory {
	m := &databaseMemory{
		db:          db,
		autoMigrate: true,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

func (m *databaseMemory) ensureMigrated() error {
	if m.migrated || !m.autoMigrate {
		return nil
	}
	m.migrated = true
	return m.db.AutoMigrate(&ConversationRecord{})
}

func (m *databaseMemory) Load(_ context.Context, key string) ([]Message, error) {
	if err := m.ensureMigrated(); err != nil {
		return nil, fmt.Errorf("ai: db memory migrate: %w", err)
	}

	var record ConversationRecord
	result := m.db.Where("key = ?", key).First(&record)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("ai: db memory load: %w", result.Error)
	}

	var msgs []Message
	if err := json.Unmarshal([]byte(record.Messages), &msgs); err != nil {
		return nil, fmt.Errorf("ai: db memory unmarshal: %w", err)
	}
	return msgs, nil
}

func (m *databaseMemory) Save(_ context.Context, key string, messages []Message) error {
	if err := m.ensureMigrated(); err != nil {
		return fmt.Errorf("ai: db memory migrate: %w", err)
	}

	data, err := json.Marshal(messages)
	if err != nil {
		return fmt.Errorf("ai: db memory marshal: %w", err)
	}

	var record ConversationRecord
	result := m.db.Where("key = ?", key).First(&record)
	if result.Error == gorm.ErrRecordNotFound {
		record = ConversationRecord{
			Key:      key,
			Messages: string(data),
		}
		return m.db.Create(&record).Error
	}
	if result.Error != nil {
		return fmt.Errorf("ai: db memory load for save: %w", result.Error)
	}

	record.Messages = string(data)
	return m.db.Save(&record).Error
}

func (m *databaseMemory) Clear(_ context.Context, key string) error {
	if err := m.ensureMigrated(); err != nil {
		return fmt.Errorf("ai: db memory migrate: %w", err)
	}
	return m.db.Where("key = ?", key).Delete(&ConversationRecord{}).Error
}
