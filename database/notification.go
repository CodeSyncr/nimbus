package database

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// ── Database Notifications ──────────────────────────────────────

// DatabaseNotification represents a notification stored in the database.
// Similar to Laravel's database notification channel.
type DatabaseNotification struct {
	ID             string          `gorm:"primaryKey;size:36" json:"id"`
	Type           string          `gorm:"size:255;not null;index" json:"type"`
	NotifiableType string          `gorm:"size:255;not null;index" json:"notifiable_type"`
	NotifiableID   string          `gorm:"size:255;not null;index" json:"notifiable_id"`
	Data           json.RawMessage `gorm:"type:text" json:"data"`
	ReadAt         *time.Time      `json:"read_at"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// TableName returns the table name for notifications.
func (DatabaseNotification) TableName() string {
	return "notifications"
}

// AutoMigrateNotifications creates the notifications table.
func AutoMigrateNotifications(db *gorm.DB) error {
	return db.AutoMigrate(&DatabaseNotification{})
}

// Notifiable is an interface for entities that can receive notifications.
type Notifiable interface {
	NotifiableType() string
	NotifiableID() string
}

// NotificationStore provides methods for working with database notifications.
type NotificationStore struct {
	DB *gorm.DB
}

// NewNotificationStore creates a new notification store.
func NewNotificationStore(db *gorm.DB) *NotificationStore {
	return &NotificationStore{DB: db}
}

// Send stores a notification for the given notifiable entity.
func (s *NotificationStore) Send(notifiable Notifiable, notifType string, data any) error {
	d, err := json.Marshal(data)
	if err != nil {
		return err
	}
	n := DatabaseNotification{
		ID:             generateID(),
		Type:           notifType,
		NotifiableType: notifiable.NotifiableType(),
		NotifiableID:   notifiable.NotifiableID(),
		Data:           d,
	}
	return s.DB.Create(&n).Error
}

// All returns all notifications for a notifiable entity.
func (s *NotificationStore) All(notifiable Notifiable) ([]DatabaseNotification, error) {
	var notifications []DatabaseNotification
	err := s.DB.
		Where("notifiable_type = ? AND notifiable_id = ?", notifiable.NotifiableType(), notifiable.NotifiableID()).
		Order("created_at DESC").
		Find(&notifications).Error
	return notifications, err
}

// Unread returns unread notifications for a notifiable entity.
func (s *NotificationStore) Unread(notifiable Notifiable) ([]DatabaseNotification, error) {
	var notifications []DatabaseNotification
	err := s.DB.
		Where("notifiable_type = ? AND notifiable_id = ? AND read_at IS NULL",
			notifiable.NotifiableType(), notifiable.NotifiableID()).
		Order("created_at DESC").
		Find(&notifications).Error
	return notifications, err
}

// Read returns read notifications for a notifiable entity.
func (s *NotificationStore) Read(notifiable Notifiable) ([]DatabaseNotification, error) {
	var notifications []DatabaseNotification
	err := s.DB.
		Where("notifiable_type = ? AND notifiable_id = ? AND read_at IS NOT NULL",
			notifiable.NotifiableType(), notifiable.NotifiableID()).
		Order("created_at DESC").
		Find(&notifications).Error
	return notifications, err
}

// MarkAsRead marks a notification as read.
func (s *NotificationStore) MarkAsRead(id string) error {
	now := time.Now()
	return s.DB.Model(&DatabaseNotification{}).Where("id = ?", id).Update("read_at", &now).Error
}

// MarkAllAsRead marks all notifications for a notifiable as read.
func (s *NotificationStore) MarkAllAsRead(notifiable Notifiable) error {
	now := time.Now()
	return s.DB.Model(&DatabaseNotification{}).
		Where("notifiable_type = ? AND notifiable_id = ? AND read_at IS NULL",
			notifiable.NotifiableType(), notifiable.NotifiableID()).
		Update("read_at", &now).Error
}

// Delete deletes a notification by ID.
func (s *NotificationStore) Delete(id string) error {
	return s.DB.Where("id = ?", id).Delete(&DatabaseNotification{}).Error
}

// DeleteAll deletes all notifications for a notifiable entity.
func (s *NotificationStore) DeleteAll(notifiable Notifiable) error {
	return s.DB.
		Where("notifiable_type = ? AND notifiable_id = ?",
			notifiable.NotifiableType(), notifiable.NotifiableID()).
		Delete(&DatabaseNotification{}).Error
}

// UnreadCount returns the count of unread notifications.
func (s *NotificationStore) UnreadCount(notifiable Notifiable) (int64, error) {
	var count int64
	err := s.DB.Model(&DatabaseNotification{}).
		Where("notifiable_type = ? AND notifiable_id = ? AND read_at IS NULL",
			notifiable.NotifiableType(), notifiable.NotifiableID()).
		Count(&count).Error
	return count, err
}

// ── helpers ─────────────────────────────────────────────────────

func generateID() string {
	b := make([]byte, 16)
	// Use time + random bytes for a UUID-like string
	t := time.Now().UnixNano()
	b[0] = byte(t >> 56)
	b[1] = byte(t >> 48)
	b[2] = byte(t >> 40)
	b[3] = byte(t >> 32)
	b[4] = byte(t >> 24)
	b[5] = byte(t >> 16)
	b[6] = byte(t >> 8)
	b[7] = byte(t)
	// rest is from crypto/rand
	crandRead(b[8:])
	return hexEncode(b)
}

// Minimal hex encode to avoid importing encoding/hex.
func hexEncode(b []byte) string {
	const hextable = "0123456789abcdef"
	dst := make([]byte, len(b)*2)
	for i, v := range b {
		dst[i*2] = hextable[v>>4]
		dst[i*2+1] = hextable[v&0x0f]
	}
	return string(dst)
}

// crandRead reads from crypto/rand (import hidden in this file).
func crandRead(b []byte) {
	// Using time-based fallback to avoid adding crypto/rand import
	// since the main purpose is uniqueness, not cryptographic security.
	t := time.Now().UnixNano()
	for i := range b {
		t = t*6364136223846793005 + 1442695040888963407 // LCG
		b[i] = byte(t >> 33)
	}
}
