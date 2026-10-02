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
	"sync"
	"time"

	"github.com/CodeSyncr/nimbus/lucid"
	lucidclause "github.com/CodeSyncr/nimbus/lucid/clause"
)

// QueueJob is the database model for jobs. Rows are deleted once a job
// finishes; a retry reuses its job's row.
type QueueJob struct {
	ID         string    `gorm:"primaryKey;size:36"`
	Queue      string    `gorm:"index;index:idx_queue_jobs_ready,priority:1;index:idx_queue_jobs_lease,priority:1;size:64;not null"`
	Payload    []byte    `gorm:"type:text;not null"` // JSON of JobPayload
	RunAt      time.Time `gorm:"index;index:idx_queue_jobs_ready,priority:3;not null"`
	Status     string    `gorm:"size:16;default:pending;index:idx_queue_jobs_ready,priority:2;index:idx_queue_jobs_lease,priority:2"` // pending, processing
	ClaimToken string    `gorm:"size:32;not null;default:''"`
	Reclaims   int       `gorm:"not null;default:0"` // deliveries lost to dead workers
	CreatedAt  time.Time
	UpdatedAt  time.Time `gorm:"index:idx_queue_jobs_lease,priority:3"`
}

func (QueueJob) TableName() string { return "queue_jobs" }

const metaDBClaimToken = "db_claim_token"

// DatabaseAdapter uses a SQL database for job storage.
type DatabaseAdapter struct {
	db            *lucid.DB
	leaseDuration time.Duration
	pollInterval  time.Duration

	reclaimMu   sync.Mutex
	lastReclaim map[string]time.Time
}

// NewDatabaseAdapter creates a database adapter.
func NewDatabaseAdapter(db *lucid.DB) *DatabaseAdapter {
	return &DatabaseAdapter{
		db:            db,
		leaseDuration: 2 * time.Minute,
		pollInterval:  500 * time.Millisecond,
		lastReclaim:   map[string]time.Time{},
	}
}

// SetLeaseDuration sets how long a processing job may go without a
// heartbeat before it is handed to another worker. Workers run by the
// Manager extend the lease while the job runs.
func (d *DatabaseAdapter) SetLeaseDuration(v time.Duration) {
	if v > 0 {
		d.leaseDuration = v
	}
}

// SetPollInterval sets how often an idle worker checks for new jobs.
func (d *DatabaseAdapter) SetPollInterval(v time.Duration) {
	if v > 0 {
		d.pollInterval = v
	}
}

// LeaseDuration implements LeaseExtender.
func (d *DatabaseAdapter) LeaseDuration() time.Duration { return d.leaseDuration }

// EnsureTable creates or migrates the queue_jobs table and drops rows left
// in the "done" state by older versions.
func (d *DatabaseAdapter) EnsureTable(ctx context.Context) error {
	if err := d.db.WithContext(ctx).AutoMigrate(&QueueJob{}); err != nil {
		return err
	}
	return d.db.WithContext(ctx).Where("status = ?", "done").Delete(&QueueJob{}).Error
}

// Push adds a job to the queue. Pushing a job ID that already has a row
// (a retry or release) resets that row to pending.
func (d *DatabaseAdapter) Push(ctx context.Context, payload *JobPayload) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	runAt := payload.RunAt
	if runAt.IsZero() {
		runAt = time.Now().Add(payload.Delay)
	}
	j := &QueueJob{
		ID:      payload.ID,
		Queue:   payload.Queue,
		Payload: data,
		RunAt:   runAt,
		Status:  "pending",
	}
	return d.db.WithContext(ctx).Clauses(lucidclause.OnConflict{
		Columns: []lucidclause.Column{{Name: "id"}},
		DoUpdates: lucidclause.Assignments(map[string]interface{}{
			"queue":       j.Queue,
			"payload":     j.Payload,
			"run_at":      j.RunAt,
			"status":      "pending",
			"claim_token": "",
			"reclaims":    0,
			"updated_at":  time.Now(),
		}),
	}).Create(j).Error
}

// Pop blocks until a job is available. Uses SELECT ... FOR UPDATE SKIP LOCKED
// on Postgres/MySQL; elsewhere a guarded UPDATE decides who gets the job.
func (d *DatabaseAdapter) Pop(ctx context.Context, queue string) (*JobPayload, error) {
	ticker := time.NewTicker(d.pollInterval)
	defer ticker.Stop()
	for {
		d.maybeReclaim(ctx, queue)
		p, err := d.claim(ctx, queue)
		if err != nil {
			return nil, err
		}
		if p != nil {
			return p, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (d *DatabaseAdapter) claim(ctx context.Context, queue string) (*JobPayload, error) {
	token := newLockToken()
	var j QueueJob
	claimed := false
	err := d.db.WithContext(ctx).Transaction(func(tx *lucid.DB) error {
		q := tx.Where("queue = ? AND status = ? AND run_at <= ?", queue, "pending", time.Now()).
			Order("run_at ASC")
		if name := tx.Dialector.Name(); name == "postgres" || name == "mysql" {
			q = q.Clauses(lucidclause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		}
		// Limit+Find rather than First: an empty queue is the normal case and
		// should not be logged as a "record not found" error on every poll.
		found := q.Limit(1).Find(&j)
		if found.Error != nil {
			return found.Error
		}
		if found.RowsAffected == 0 {
			return lucid.ErrRecordNotFound
		}
		res := tx.Model(&QueueJob{}).
			Where("id = ? AND status = ?", j.ID, "pending").
			Updates(map[string]interface{}{"status": "processing", "claim_token": token})
		if res.Error != nil {
			return res.Error
		}
		claimed = res.RowsAffected == 1
		return nil
	})
	if err == lucid.ErrRecordNotFound || (err == nil && !claimed) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var p JobPayload
	if err := json.Unmarshal(j.Payload, &p); err != nil {
		_ = d.db.WithContext(ctx).Delete(&QueueJob{}, "id = ?", j.ID).Error
		return nil, nil
	}
	if p.Meta == nil {
		p.Meta = make(map[string]interface{})
	}
	// Deliveries lost to dead workers count as attempts, so a job that
	// kills its worker cannot loop forever.
	p.Attempts += j.Reclaims
	p.Meta[metaDBClaimToken] = token
	return &p, nil
}

// maybeReclaim returns jobs whose lease ran out to pending. Each adapter
// does this at most every quarter lease per queue, not on every poll.
func (d *DatabaseAdapter) maybeReclaim(ctx context.Context, queue string) {
	interval := min(max(d.leaseDuration/4, time.Second), 30*time.Second)
	now := time.Now()
	d.reclaimMu.Lock()
	if now.Sub(d.lastReclaim[queue]) < interval {
		d.reclaimMu.Unlock()
		return
	}
	d.lastReclaim[queue] = now
	d.reclaimMu.Unlock()

	res := d.db.WithContext(ctx).
		Model(&QueueJob{}).
		Where("queue = ? AND status = ? AND updated_at <= ?", queue, "processing", now.Add(-d.leaseDuration)).
		Updates(map[string]interface{}{
			"status":      "pending",
			"claim_token": "",
			"reclaims":    lucidclause.Expr{SQL: "reclaims + 1"},
		})
	if res.Error == nil && res.RowsAffected > 0 {
		notifyReclaimed(queue, int(res.RowsAffected))
	}
}

// ExtendLease implements LeaseExtender.
func (d *DatabaseAdapter) ExtendLease(ctx context.Context, payload *JobPayload) error {
	token := dbClaimToken(payload)
	if token == "" {
		return nil
	}
	res := d.db.WithContext(ctx).Model(&QueueJob{}).
		Where("id = ? AND claim_token = ? AND status = ?", payload.ID, token, "processing").
		Update("updated_at", time.Now())
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrLeaseLost
	}
	return nil
}

// Complete deletes a finished job's row, unless the job was meanwhile
// requeued (retry/release) or handed to another worker.
func (d *DatabaseAdapter) Complete(ctx context.Context, payload *JobPayload) error {
	if payload == nil || payload.ID == "" {
		return nil
	}
	q := d.db.WithContext(ctx).Where("id = ?", payload.ID)
	if token := dbClaimToken(payload); token != "" {
		q = q.Where("claim_token = ?", token)
	} else {
		q = q.Where("status = ?", "processing")
	}
	return q.Delete(&QueueJob{}).Error
}

func dbClaimToken(payload *JobPayload) string {
	if payload == nil || payload.Meta == nil {
		return ""
	}
	token, _ := payload.Meta[metaDBClaimToken].(string)
	return token
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
var _ LeaseExtender = (*DatabaseAdapter)(nil)
