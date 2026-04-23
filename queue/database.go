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

	"github.com/CodeSyncr/nimbus/lucid"
	lucidclause "github.com/CodeSyncr/nimbus/lucid/clause"
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
	db            *lucid.DB
	leaseDuration time.Duration
}

// NewDatabaseAdapter creates a database adapter.
func NewDatabaseAdapter(db *lucid.DB) *DatabaseAdapter {
	return &DatabaseAdapter{
		db:            db,
		leaseDuration: 2 * time.Minute,
	}
}

// SetLeaseDuration sets how long a processing job can remain unacked before reclaim.
func (d *DatabaseAdapter) SetLeaseDuration(v time.Duration) {
	if v > 0 {
		d.leaseDuration = v
	}
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
			now := time.Now()
			res := d.db.WithContext(ctx).
				Model(&QueueJob{}).
				Where("queue = ? AND status = ? AND updated_at <= ?", queue, "processing", now.Add(-d.leaseDuration)).
				Update("status", "pending")
			if res.Error == nil && res.RowsAffected > 0 {
				notifyReclaimed(queue, int(res.RowsAffected))
			}

			var j QueueJob
			err := d.db.WithContext(ctx).Transaction(func(tx *lucid.DB) error {
				q := tx.Where("queue = ? AND status = ? AND run_at <= ?", queue, "pending", time.Now()).
					Order("run_at ASC")
				if tx.Dialector.Name() == "postgres" || tx.Dialector.Name() == "mysql" {
					q = q.Clauses(lucidclause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
				}
				if err := q.First(&j).Error; err != nil {
					return err
				}
				return tx.Model(&j).Update("status", "processing").Error
			})
			if err == lucid.ErrRecordNotFound {
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

// Complete marks a processing job as done after successful or terminal handling.
func (d *DatabaseAdapter) Complete(ctx context.Context, payload *JobPayload) error {
	if payload == nil || payload.ID == "" {
		return nil
	}
	return d.db.WithContext(ctx).
		Model(&QueueJob{}).
		Where("id = ?", payload.ID).
		Update("status", "done").Error
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
var _ CompletableAdapter = (*DatabaseAdapter)(nil)
