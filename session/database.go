package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// DatabaseStore stores sessions in a database table.
// Create the table with: CREATE TABLE sessions (id VARCHAR(64) PRIMARY KEY, payload TEXT, expires_at DATETIME);
type DatabaseStore struct {
	db    *gorm.DB
	table string
}

// SessionRecord is the DB model for sessions.
type SessionRecord struct {
	ID        string    `gorm:"primaryKey;size:64"`
	Payload   string    `gorm:"type:text"`
	ExpiresAt time.Time `gorm:"index"`
}

// NewDatabaseStore creates a database-backed session store.
func NewDatabaseStore(db *gorm.DB, table string) *DatabaseStore {
	if table == "" {
		table = "sessions"
	}
	return &DatabaseStore{db: db, table: table}
}

// EnsureTable creates the sessions table if it doesn't exist.
func (s *DatabaseStore) EnsureTable() error {
	return s.db.Table(s.table).AutoMigrate(&SessionRecord{})
}

func (s *DatabaseStore) Get(ctx context.Context, id string) (map[string]any, error) {
	if id == "" {
		return nil, nil
	}
	var rec SessionRecord
	err := s.db.WithContext(ctx).Table(s.table).Where("id = ? AND expires_at > ?", id, time.Now()).First(&rec).Error
	if err != nil {
		return nil, nil
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(rec.Payload), &data); err != nil {
		return nil, nil
	}
	return data, nil
}

func (s *DatabaseStore) Set(ctx context.Context, id string, data map[string]any, maxAge time.Duration) (string, error) {
	if id == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		id = hex.EncodeToString(b)
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	rec := SessionRecord{
		ID:        id,
		Payload:   string(payload),
		ExpiresAt: time.Now().Add(maxAge),
	}
	err = s.db.WithContext(ctx).Table(s.table).Save(&rec).Error
	if err != nil {
		return "", err
	}
	return id, nil
}

func (s *DatabaseStore) Destroy(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	return s.db.WithContext(ctx).Table(s.table).Where("id = ?", id).Delete(&SessionRecord{}).Error
}
