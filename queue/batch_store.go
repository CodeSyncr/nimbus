/*
|--------------------------------------------------------------------------
| Batch Store
|--------------------------------------------------------------------------
|
| Durable batches keep their progress here so any worker can record a
| job's outcome and whichever worker finishes the last job runs the
| batch callbacks. Boot installs a Redis or database store to match the
| queue driver.
|
*/

package queue

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/CodeSyncr/nimbus/lucid"
	lucidclause "github.com/CodeSyncr/nimbus/lucid/clause"
	"github.com/CodeSyncr/nimbus/redis"
)

// maxBatchErrors caps how many job errors a batch keeps.
const maxBatchErrors = 50

// ErrBatchNotFound is returned when a batch ID is unknown (or expired).
var ErrBatchNotFound = errors.New("queue: batch not found")

// BatchState is a batch's stored progress.
type BatchState struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Total      int       `json:"total"`
	Pending    int       `json:"pending"`
	Failed     int       `json:"failed"`
	Errors     []string  `json:"errors,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	FinishedAt time.Time `json:"finished_at,omitzero"`
}

// BatchUpdate is the result of recording one job's outcome.
type BatchUpdate struct {
	State BatchState
	// Counted is false when this job's outcome was already recorded (the
	// job was delivered twice), so callbacks must not run again.
	Counted bool
	// Finished is true for exactly one caller: the one that recorded the
	// batch's last outstanding job.
	Finished bool
}

// BatchStore persists batch progress.
type BatchStore interface {
	Create(ctx context.Context, state BatchState) error
	// Record counts jobID as done (failed when jobErr is non-nil). Recording
	// the same job twice is a no-op.
	Record(ctx context.Context, batchID, jobID string, jobErr error) (BatchUpdate, error)
	Find(ctx context.Context, batchID string) (*BatchState, error)
}

var (
	batchStoreMu sync.RWMutex
	batchStore   BatchStore = NewMemoryBatchStore()
)

// SetBatchStore sets where durable batches keep progress. Boot calls it for
// the redis and database drivers.
func SetBatchStore(s BatchStore) {
	if s == nil {
		s = NewMemoryBatchStore()
	}
	batchStoreMu.Lock()
	batchStore = s
	batchStoreMu.Unlock()
}

// GetBatchStore returns the store durable batches use.
func GetBatchStore() BatchStore {
	batchStoreMu.RLock()
	defer batchStoreMu.RUnlock()
	return batchStore
}

// FindBatch looks up a dispatched batch's progress by ID.
func FindBatch(ctx context.Context, id string) (*BatchState, error) {
	return GetBatchStore().Find(ctx, id)
}

// ── Memory ──────────────────────────────────────────────────────────

// MemoryBatchStore keeps batches in this process. Batches finished more
// than an hour ago are dropped.
type MemoryBatchStore struct {
	mu      sync.Mutex
	batches map[string]*memoryBatch
}

type memoryBatch struct {
	state BatchState
	done  map[string]bool
}

// NewMemoryBatchStore returns a process-local BatchStore.
func NewMemoryBatchStore() *MemoryBatchStore {
	return &MemoryBatchStore{batches: make(map[string]*memoryBatch)}
}

func (s *MemoryBatchStore) Create(_ context.Context, state BatchState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-time.Hour)
	for id, b := range s.batches {
		if !b.state.FinishedAt.IsZero() && b.state.FinishedAt.Before(cutoff) {
			delete(s.batches, id)
		}
	}
	s.batches[state.ID] = &memoryBatch{state: state, done: make(map[string]bool)}
	return nil
}

func (s *MemoryBatchStore) Record(_ context.Context, batchID, jobID string, jobErr error) (BatchUpdate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.batches[batchID]
	if !ok {
		return BatchUpdate{}, ErrBatchNotFound
	}
	if b.done[jobID] {
		return BatchUpdate{State: b.state.clone()}, nil
	}
	b.done[jobID] = true
	b.state.Pending--
	if jobErr != nil {
		b.state.Failed++
		if len(b.state.Errors) < maxBatchErrors {
			b.state.Errors = append(b.state.Errors, jobErr.Error())
		}
	}
	finished := false
	if b.state.Pending <= 0 && b.state.FinishedAt.IsZero() {
		b.state.FinishedAt = time.Now()
		finished = true
	}
	return BatchUpdate{State: b.state.clone(), Counted: true, Finished: finished}, nil
}

func (s *MemoryBatchStore) Find(_ context.Context, batchID string) (*BatchState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.batches[batchID]
	if !ok {
		return nil, ErrBatchNotFound
	}
	st := b.state.clone()
	return &st, nil
}

func (s BatchState) clone() BatchState {
	s.Errors = append([]string(nil), s.Errors...)
	return s
}

// ── Redis ───────────────────────────────────────────────────────────

const redisBatchPrefix = "nimbus:batch:"

// recordBatchScript counts one job against a batch exactly once.
//
// KEYS: batch hash, recorded job set, error list
// ARGV: job id, error message (” on success), finished_at, ttl seconds, max errors
var recordBatchScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then
  return -1
end
if redis.call('SADD', KEYS[2], ARGV[1]) == 0 then
  return 0
end
local pending = redis.call('HINCRBY', KEYS[1], 'pending', -1)
if ARGV[2] ~= '' then
  redis.call('HINCRBY', KEYS[1], 'failed', 1)
  if redis.call('LLEN', KEYS[3]) < tonumber(ARGV[5]) then
    redis.call('RPUSH', KEYS[3], ARGV[2])
  end
end
local ttl = tonumber(ARGV[4])
redis.call('EXPIRE', KEYS[1], ttl)
redis.call('EXPIRE', KEYS[2], ttl)
redis.call('EXPIRE', KEYS[3], ttl)
if pending <= 0 and redis.call('HSETNX', KEYS[1], 'finished_at', ARGV[3]) == 1 then
  return 2
end
return 1
`)

// RedisBatchStore keeps batch progress in Redis. Batches expire TTL after
// their last update.
type RedisBatchStore struct {
	client *redis.Client
	TTL    time.Duration
}

// NewRedisBatchStore returns a Redis-backed BatchStore that keeps batches
// for 7 days after their last update.
func NewRedisBatchStore(client *redis.Client) *RedisBatchStore {
	return &RedisBatchStore{client: client, TTL: 7 * 24 * time.Hour}
}

func (s *RedisBatchStore) keys(id string) []string {
	return []string{redisBatchPrefix + id, redisBatchPrefix + id + ":jobs", redisBatchPrefix + id + ":errors"}
}

func (s *RedisBatchStore) Create(ctx context.Context, state BatchState) error {
	k := s.keys(state.ID)
	_, err := s.client.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.HSet(ctx, k[0], map[string]interface{}{
			"name":       state.Name,
			"total":      state.Total,
			"pending":    state.Pending,
			"failed":     state.Failed,
			"created_at": state.CreatedAt.UTC().Format(time.RFC3339Nano),
		})
		p.Expire(ctx, k[0], s.TTL)
		return nil
	})
	return err
}

func (s *RedisBatchStore) Record(ctx context.Context, batchID, jobID string, jobErr error) (BatchUpdate, error) {
	msg := ""
	if jobErr != nil {
		msg = jobErr.Error()
		if msg == "" {
			msg = "job failed"
		}
	}
	res, err := recordBatchScript.Run(ctx, s.client, s.keys(batchID),
		jobID, msg, time.Now().UTC().Format(time.RFC3339Nano), int(s.TTL.Seconds()), maxBatchErrors,
	).Int()
	if err != nil {
		return BatchUpdate{}, err
	}
	if res < 0 {
		return BatchUpdate{}, ErrBatchNotFound
	}
	st, err := s.Find(ctx, batchID)
	if err != nil {
		return BatchUpdate{}, err
	}
	return BatchUpdate{State: *st, Counted: res >= 1, Finished: res == 2}, nil
}

func (s *RedisBatchStore) Find(ctx context.Context, batchID string) (*BatchState, error) {
	k := s.keys(batchID)
	h, err := s.client.HGetAll(ctx, k[0]).Result()
	if err != nil {
		return nil, err
	}
	if len(h) == 0 {
		return nil, ErrBatchNotFound
	}
	errs, err := s.client.LRange(ctx, k[2], 0, -1).Result()
	if err != nil && err != redis.Nil {
		return nil, err
	}
	st := &BatchState{ID: batchID, Name: h["name"], Errors: errs}
	st.Total, _ = strconv.Atoi(h["total"])
	st.Pending, _ = strconv.Atoi(h["pending"])
	st.Failed, _ = strconv.Atoi(h["failed"])
	st.CreatedAt, _ = time.Parse(time.RFC3339Nano, h["created_at"])
	st.FinishedAt, _ = time.Parse(time.RFC3339Nano, h["finished_at"])
	return st, nil
}

// ── Database ────────────────────────────────────────────────────────

// QueueBatch is the database model for DatabaseBatchStore.
type QueueBatch struct {
	ID         string `gorm:"primaryKey;size:36"`
	Name       string `gorm:"size:191"`
	Total      int    `gorm:"not null"`
	Pending    int    `gorm:"not null"`
	Failed     int    `gorm:"not null;default:0"`
	Errors     string `gorm:"type:text"` // JSON array
	CreatedAt  time.Time
	FinishedAt *time.Time `gorm:"index"`
}

func (QueueBatch) TableName() string { return "queue_batches" }

// QueueBatchJob records which jobs a batch has already counted.
type QueueBatchJob struct {
	BatchID string `gorm:"primaryKey;size:36"`
	JobID   string `gorm:"primaryKey;size:64"`
}

func (QueueBatchJob) TableName() string { return "queue_batch_jobs" }

// DatabaseBatchStore keeps batch progress in SQL tables.
type DatabaseBatchStore struct {
	db *lucid.DB
}

// NewDatabaseBatchStore returns a database-backed BatchStore. Call EnsureTable once.
func NewDatabaseBatchStore(db *lucid.DB) *DatabaseBatchStore {
	return &DatabaseBatchStore{db: db}
}

// EnsureTable creates the queue_batches and queue_batch_jobs tables.
func (s *DatabaseBatchStore) EnsureTable(ctx context.Context) error {
	return s.db.WithContext(ctx).AutoMigrate(&QueueBatch{}, &QueueBatchJob{})
}

func (s *DatabaseBatchStore) Create(ctx context.Context, state BatchState) error {
	return s.db.WithContext(ctx).Create(&QueueBatch{
		ID:        state.ID,
		Name:      state.Name,
		Total:     state.Total,
		Pending:   state.Pending,
		Failed:    state.Failed,
		Errors:    "[]",
		CreatedAt: state.CreatedAt,
	}).Error
}

func (s *DatabaseBatchStore) Record(ctx context.Context, batchID, jobID string, jobErr error) (BatchUpdate, error) {
	var upd BatchUpdate
	err := s.db.WithContext(ctx).Transaction(func(tx *lucid.DB) error {
		ins := tx.Clauses(lucidclause.OnConflict{DoNothing: true}).Create(&QueueBatchJob{BatchID: batchID, JobID: jobID})
		if ins.Error != nil && !isDuplicateKey(ins.Error) {
			return ins.Error
		}
		if ins.Error != nil || ins.RowsAffected == 0 {
			st, err := s.find(tx, batchID)
			if err != nil {
				return err
			}
			upd = BatchUpdate{State: *st}
			return nil
		}
		// The UPDATE takes the batch row lock, so workers recording jobs of
		// one batch serialize here and exactly one sees pending reach 0.
		failedInc := 0
		if jobErr != nil {
			failedInc = 1
		}
		res := tx.Model(&QueueBatch{}).Where("id = ?", batchID).Updates(map[string]interface{}{
			"pending": lucidclause.Expr{SQL: "pending - 1"},
			"failed":  lucidclause.Expr{SQL: "failed + ?", Vars: []interface{}{failedInc}},
		})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrBatchNotFound
		}
		var row QueueBatch
		if err := tx.Where("id = ?", batchID).First(&row).Error; err != nil {
			return err
		}
		changes := map[string]interface{}{}
		if jobErr != nil {
			var errs []string
			_ = json.Unmarshal([]byte(row.Errors), &errs)
			if len(errs) < maxBatchErrors {
				errs = append(errs, jobErr.Error())
				b, _ := json.Marshal(errs)
				changes["errors"] = string(b)
				row.Errors = string(b)
			}
		}
		if row.Pending <= 0 && row.FinishedAt == nil {
			now := time.Now()
			changes["finished_at"] = now
			row.FinishedAt = &now
			upd.Finished = true
		}
		if len(changes) > 0 {
			if err := tx.Model(&QueueBatch{}).Where("id = ?", batchID).Updates(changes).Error; err != nil {
				return err
			}
		}
		upd.Counted = true
		upd.State = row.state()
		return nil
	})
	return upd, err
}

func (s *DatabaseBatchStore) Find(ctx context.Context, batchID string) (*BatchState, error) {
	return s.find(s.db.WithContext(ctx), batchID)
}

func (s *DatabaseBatchStore) find(db *lucid.DB, batchID string) (*BatchState, error) {
	var row QueueBatch
	if err := db.Where("id = ?", batchID).First(&row).Error; err != nil {
		if errors.Is(err, lucid.ErrRecordNotFound) {
			return nil, ErrBatchNotFound
		}
		return nil, err
	}
	st := row.state()
	return &st, nil
}

func (r QueueBatch) state() BatchState {
	st := BatchState{
		ID:        r.ID,
		Name:      r.Name,
		Total:     r.Total,
		Pending:   r.Pending,
		Failed:    r.Failed,
		CreatedAt: r.CreatedAt,
	}
	_ = json.Unmarshal([]byte(r.Errors), &st.Errors)
	if r.FinishedAt != nil {
		st.FinishedAt = *r.FinishedAt
	}
	return st
}

var (
	_ BatchStore = (*MemoryBatchStore)(nil)
	_ BatchStore = (*RedisBatchStore)(nil)
	_ BatchStore = (*DatabaseBatchStore)(nil)
)
