package notification

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// ── Database Channel ────────────────────────────────────────────

// DatabaseNotification is for notifications that support the database channel.
type DatabaseNotification interface {
	Notification
	// ToDatabase returns the data map to store in the notifications table.
	// Return nil to skip the database channel.
	ToDatabase() map[string]any
}

// DBNotification represents a stored notification record.
type DBNotification struct {
	ID             string     `gorm:"primaryKey;size:36" json:"id"`
	Type           string     `gorm:"size:255;index" json:"type"`
	NotifiableID   string     `gorm:"size:255;index:idx_notifiable" json:"notifiable_id"`
	NotifiableType string     `gorm:"size:255;index:idx_notifiable" json:"notifiable_type"`
	Data           string     `gorm:"type:text" json:"data"` // JSON-encoded
	ReadAt         *time.Time `json:"read_at"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// TableName returns the table name for the notification model.
func (DBNotification) TableName() string { return "notifications" }

// DatabaseChannel stores notifications in the database.
type DatabaseChannel struct {
	db *gorm.DB
}

// NewDatabaseChannel creates a database notification channel.
func NewDatabaseChannel(db *gorm.DB) *DatabaseChannel {
	return &DatabaseChannel{db: db}
}

// Migrate creates the notifications table.
func (c *DatabaseChannel) Migrate() error {
	return c.db.AutoMigrate(&DBNotification{})
}

// Send stores the notification in the database.
func (c *DatabaseChannel) Send(ctx context.Context, notifiableID, notifiableType, notificationType string, n DatabaseNotification) error {
	data := n.ToDatabase()
	if data == nil {
		return nil
	}

	// Encode data as JSON string.
	encoded, err := encodeJSON(data)
	if err != nil {
		return fmt.Errorf("notification/database: encode: %w", err)
	}

	record := DBNotification{
		ID:             generateUUID(),
		Type:           notificationType,
		NotifiableID:   notifiableID,
		NotifiableType: notifiableType,
		Data:           encoded,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}

	return c.db.WithContext(ctx).Create(&record).Error
}

// Unread returns unread notifications for a notifiable.
func (c *DatabaseChannel) Unread(ctx context.Context, notifiableID, notifiableType string) ([]DBNotification, error) {
	var records []DBNotification
	err := c.db.WithContext(ctx).
		Where("notifiable_id = ? AND notifiable_type = ? AND read_at IS NULL", notifiableID, notifiableType).
		Order("created_at DESC").
		Find(&records).Error
	return records, err
}

// All returns all notifications for a notifiable.
func (c *DatabaseChannel) All(ctx context.Context, notifiableID, notifiableType string) ([]DBNotification, error) {
	var records []DBNotification
	err := c.db.WithContext(ctx).
		Where("notifiable_id = ? AND notifiable_type = ?", notifiableID, notifiableType).
		Order("created_at DESC").
		Find(&records).Error
	return records, err
}

// MarkAsRead marks a notification as read.
func (c *DatabaseChannel) MarkAsRead(ctx context.Context, id string) error {
	now := time.Now().UTC()
	return c.db.WithContext(ctx).
		Model(&DBNotification{}).
		Where("id = ?", id).
		Update("read_at", &now).Error
}

// MarkAllAsRead marks all notifications for a notifiable as read.
func (c *DatabaseChannel) MarkAllAsRead(ctx context.Context, notifiableID, notifiableType string) error {
	now := time.Now().UTC()
	return c.db.WithContext(ctx).
		Model(&DBNotification{}).
		Where("notifiable_id = ? AND notifiable_type = ? AND read_at IS NULL", notifiableID, notifiableType).
		Update("read_at", &now).Error
}

// Delete removes a notification by ID.
func (c *DatabaseChannel) Delete(ctx context.Context, id string) error {
	return c.db.WithContext(ctx).Delete(&DBNotification{}, "id = ?", id).Error
}

// DeleteAll removes all notifications for a notifiable.
func (c *DatabaseChannel) DeleteAll(ctx context.Context, notifiableID, notifiableType string) error {
	return c.db.WithContext(ctx).Delete(&DBNotification{}, "notifiable_id = ? AND notifiable_type = ?", notifiableID, notifiableType).Error
}
