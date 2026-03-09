/*
|--------------------------------------------------------------------------
| Database Queue Adapter
|--------------------------------------------------------------------------
|
| Uses SQL database (Postgres, MySQL, SQLite) for job persistence.
| Supports delayed jobs via run_at. Use when Redis is not available.
|
*/

package queue

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// QueueJob is the database model for jobs.
type QueueJob struct {
	ID        string    `gorm:"primaryKey;size:36"`
	Queue     string    `gorm:"index;size:64;not null"`
	Payload   []byte    `gorm:"type:text;not null"` // JSON of JobPayload
	RunAt     time.Time `gorm:"index;not null"`
	Status    string    `gorm:"size:16;default:pending"` // pending, processing, failed, done
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (QueueJob) TableName() string { return "queue_jobs" }

// DatabaseAdapter uses a SQL database for job storage.
type DatabaseAdapter struct {
	db *gorm.DB
}

// NewDatabaseAdapter creates a database adapter.
func NewDatabaseAdapter(db *gorm.DB) *DatabaseAdapter {
	return &DatabaseAdapter{db: db}
}

// EnsureTable creates the queue_jobs table if not exists.
func (d *DatabaseAdapter) EnsureTable(ctx context.Context) error {
	return d.db.WithContext(ctx).AutoMigrate(&QueueJob{})
}

// Push adds a job to the queue.
func (d *DatabaseAdapter) Push(ctx context.Context, payload *JobPayload) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	j := &QueueJob{
		ID:      payload.ID,
		Queue:   payload.Queue,
		Payload: data,
		RunAt:   payload.RunAt,
		Status:  "pending",
	}
	return d.db.WithContext(ctx).Create(j).Error
}

// Pop blocks until a job is available. Uses polling with SELECT FOR UPDATE SKIP LOCKED (Postgres/MySQL).
func (d *DatabaseAdapter) Pop(ctx context.Context, queue string) (*JobPayload, error) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			var j QueueJob
			err := d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				q := tx.Where("queue = ? AND status = ? AND run_at <= ?", queue, "pending", time.Now()).
					Order("run_at ASC")
				if tx.Dialector.Name() == "postgres" || tx.Dialector.Name() == "mysql" {
					q = q.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
				}
				if err := q.First(&j).Error; err != nil {
					return err
				}
				return tx.Model(&j).Update("status", "processing").Error
			})
			if err == gorm.ErrRecordNotFound {
				continue
			}
			if err != nil {
				return nil, err
			}
			var p JobPayload
			if err := json.Unmarshal(j.Payload, &p); err != nil {
				_ = d.db.WithContext(ctx).Delete(&j).Error
				continue
			}
			return &p, nil
		}
	}
}

// Len returns the number of pending jobs.
func (d *DatabaseAdapter) Len(ctx context.Context, queue string) (int, error) {
	var n int64
	err := d.db.WithContext(ctx).Model(&QueueJob{}).
		Where("queue = ? AND status = ?", queue, "pending").
		Count(&n).Error
	return int(n), err
}

var _ Adapter = (*DatabaseAdapter)(nil)
