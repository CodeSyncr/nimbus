package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/CodeSyncr/nimbus/lucid"
)

// DatabaseStore stores sessions in a database table.
// Create the table with: CREATE TABLE sessions (id VARCHAR(64) PRIMARY KEY, payload TEXT, expires_at DATETIME);
type DatabaseStore struct {
	db    *lucid.DB
	table string
}

// SessionRecord is the DB model for sessions.
type SessionRecord struct {
	ID        string    `gorm:"primaryKey;size:64"`
	Payload   string    `gorm:"type:text"`
	ExpiresAt time.Time `gorm:"index"`
}

// NewDatabaseStore creates a database-backed session store.
func NewDatabaseStore(db *lucid.DB, table string) *DatabaseStore {
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
	if errors.Is(err, lucid.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
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

func (s *DatabaseStore) Update(ctx context.Context, id string, data map[string]any, maxAge time.Duration) (string, error) {
	if err := s.Replace(ctx, id, id, data, maxAge); err != nil {
		return "", err
	}
	return id, nil
}

// Replace performs one conditional UPDATE, never an upsert after revocation.
func (s *DatabaseStore) Replace(ctx context.Context, oldID, newID string, data map[string]any, maxAge time.Duration) error {
	if oldID == "" || newID == "" {
		return ErrExpired
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	now := time.Now()
	result := s.db.WithContext(ctx).Table(s.table).Where("id = ? AND expires_at > ?", oldID, now).Updates(map[string]any{"id": newID, "payload": string(payload), "expires_at": now.Add(maxAge)})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrExpired
	}
	return nil
}

func (s *DatabaseStore) Rotate(ctx context.Context, id string, data map[string]any, maxAge time.Duration) (string, error) {
	return rotateRecord(ctx, s, id, data, maxAge)
}
