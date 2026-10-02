package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/CodeSyncr/nimbus/lucid"
	lucidclause "github.com/CodeSyncr/nimbus/lucid/clause"
	"github.com/CodeSyncr/nimbus/redis"
)

// ---------------------------------------------------------------------------
// Leases
// ---------------------------------------------------------------------------

// Leaser is implemented by stores that several engine instances can share.
// An engine holds a lease on every run it executes and renews it while the
// run is in progress; when an instance dies, its leases lapse and Resume on
// another instance picks the runs up.
type Leaser interface {
	// Claim leases runID to owner if nobody holds a live lease on it.
	Claim(ctx context.Context, runID, owner string, ttl time.Duration) (bool, error)
	// Renew extends owner's lease. It returns false if the lease was lost.
	Renew(ctx context.Context, runID, owner string, ttl time.Duration) (bool, error)
	// Release drops owner's lease.
	Release(ctx context.Context, runID, owner string) error
	// Unfinished returns runs that are pending, running or paused.
	Unfinished(ctx context.Context, limit int) ([]*RunInstance, error)
}

func finished(s RunStatus) bool {
	return s == RunCompleted || s == RunFailed || s == RunCancelled
}

// memory leases, so the MemoryStore works with the same engine code.

type memoryLease struct {
	owner string
	exp   time.Time
}

type memoryLeases struct {
	mu     sync.Mutex
	leases map[string]memoryLease
}

func (m *memoryLeases) claim(runID, owner string, ttl time.Duration) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.leases == nil {
		m.leases = map[string]memoryLease{}
	}
	if l, ok := m.leases[runID]; ok && l.owner != owner && time.Now().Before(l.exp) {
		return false
	}
	m.leases[runID] = memoryLease{owner: owner, exp: time.Now().Add(ttl)}
	return true
}

func (m *memoryLeases) renew(runID, owner string, ttl time.Duration) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.leases[runID]
	if !ok || l.owner != owner {
		return false
	}
	m.leases[runID] = memoryLease{owner: owner, exp: time.Now().Add(ttl)}
	return true
}

func (m *memoryLeases) release(runID, owner string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if l, ok := m.leases[runID]; ok && l.owner == owner {
		delete(m.leases, runID)
	}
}

// ---------------------------------------------------------------------------
// Redis Store
// ---------------------------------------------------------------------------

var renewLeaseScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('PEXPIRE', KEYS[1], ARGV[2])
end
return 0
`)

var releaseLeaseScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('DEL', KEYS[1])
end
return 0
`)

// RedisStore keeps workflow runs in Redis so every instance sees them.
// Finished runs expire after Retention (default 30 days).
type RedisStore struct {
	client    *redis.Client
	prefix    string
	Retention time.Duration
}

// NewRedisStore returns a Redis-backed Store.
func NewRedisStore(client *redis.Client) *RedisStore {
	return &RedisStore{client: client, prefix: "nimbus:workflow:", Retention: 30 * 24 * time.Hour}
}

func (s *RedisStore) runKey(id string) string   { return s.prefix + "run:" + id }
func (s *RedisStore) leaseKey(id string) string { return s.prefix + "lease:" + id }
func (s *RedisStore) indexKey(wf string) string { return s.prefix + "runs:" + wf }
func (s *RedisStore) allKey() string            { return s.prefix + "runs" }
func (s *RedisStore) unfinishedKey() string     { return s.prefix + "unfinished" }

func (s *RedisStore) Save(ctx context.Context, run *RunInstance) error {
	data, err := json.Marshal(run)
	if err != nil {
		return err
	}
	score := float64(run.CreatedAt.UnixMilli())
	_, err = s.client.TxPipelined(ctx, func(p redis.Pipeliner) error {
		if finished(run.Status) {
			p.Set(ctx, s.runKey(run.ID), data, s.Retention)
			p.SRem(ctx, s.unfinishedKey(), run.ID)
		} else {
			p.Set(ctx, s.runKey(run.ID), data, 0)
			p.SAdd(ctx, s.unfinishedKey(), run.ID)
		}
		p.ZAdd(ctx, s.indexKey(run.Workflow), redis.Z{Score: score, Member: run.ID})
		p.ZAdd(ctx, s.allKey(), redis.Z{Score: score, Member: run.ID})
		return nil
	})
	return err
}

func (s *RedisStore) Load(ctx context.Context, id string) (*RunInstance, error) {
	data, err := s.client.Get(ctx, s.runKey(id)).Bytes()
	if err == redis.Nil {
		return nil, fmt.Errorf("workflow run %q not found", id)
	}
	if err != nil {
		return nil, err
	}
	var run RunInstance
	if err := json.Unmarshal(data, &run); err != nil {
		return nil, err
	}
	return &run, nil
}

// List returns runs newest first.
func (s *RedisStore) List(ctx context.Context, workflow string, limit int) ([]*RunInstance, error) {
	key := s.allKey()
	if workflow != "" {
		key = s.indexKey(workflow)
	}
	stop := int64(-1)
	if limit > 0 {
		stop = int64(limit) - 1
	}
	ids, err := s.client.ZRevRange(ctx, key, 0, stop).Result()
	if err != nil {
		return nil, err
	}
	return s.loadMany(ctx, ids, func(id string) {
		// Expired run: drop it from the indexes.
		s.client.ZRem(ctx, key, id)
	})
}

func (s *RedisStore) loadMany(ctx context.Context, ids []string, missing func(id string)) ([]*RunInstance, error) {
	out := make([]*RunInstance, 0, len(ids))
	for _, id := range ids {
		run, err := s.Load(ctx, id)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				missing(id)
				continue
			}
			return nil, err
		}
		out = append(out, run)
	}
	return out, nil
}

func (s *RedisStore) Delete(ctx context.Context, id string) error {
	run, err := s.Load(ctx, id)
	if err != nil {
		return nil
	}
	_, err = s.client.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.Del(ctx, s.runKey(id), s.leaseKey(id))
		p.ZRem(ctx, s.indexKey(run.Workflow), id)
		p.ZRem(ctx, s.allKey(), id)
		p.SRem(ctx, s.unfinishedKey(), id)
		return nil
	})
	return err
}

func (s *RedisStore) signalKey(runID, event string) string {
	return s.prefix + "signals:" + runID + ":" + event
}

func (s *RedisStore) PushSignal(ctx context.Context, runID, event string, data Payload) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	key := s.signalKey(runID, event)
	_, err = s.client.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.RPush(ctx, key, raw)
		p.Expire(ctx, key, s.Retention)
		return nil
	})
	return err
}

func (s *RedisStore) PopSignal(ctx context.Context, runID, event string) (Payload, bool, error) {
	raw, err := s.client.LPop(ctx, s.signalKey(runID, event)).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var data Payload
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func (s *RedisStore) Claim(ctx context.Context, runID, owner string, ttl time.Duration) (bool, error) {
	return s.client.SetNX(ctx, s.leaseKey(runID), owner, ttl).Result()
}

func (s *RedisStore) Renew(ctx context.Context, runID, owner string, ttl time.Duration) (bool, error) {
	n, err := renewLeaseScript.Run(ctx, s.client, []string{s.leaseKey(runID)}, owner, ttl.Milliseconds()).Int()
	return n == 1, err
}

func (s *RedisStore) Release(ctx context.Context, runID, owner string) error {
	return releaseLeaseScript.Run(ctx, s.client, []string{s.leaseKey(runID)}, owner).Err()
}

func (s *RedisStore) Unfinished(ctx context.Context, limit int) ([]*RunInstance, error) {
	var ids []string
	var err error
	if limit > 0 {
		ids, err = s.client.SRandMemberN(ctx, s.unfinishedKey(), int64(limit)).Result()
	} else {
		ids, err = s.client.SMembers(ctx, s.unfinishedKey()).Result()
	}
	if err != nil {
		return nil, err
	}
	runs, err := s.loadMany(ctx, ids, func(id string) { s.client.SRem(ctx, s.unfinishedKey(), id) })
	if err != nil {
		return nil, err
	}
	out := runs[:0]
	for _, r := range runs {
		if !finished(r.Status) {
			out = append(out, r)
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Database Store
// ---------------------------------------------------------------------------

// WorkflowRun is the database model for DatabaseStore.
type WorkflowRun struct {
	ID         string    `gorm:"primaryKey;size:36"`
	Workflow   string    `gorm:"size:191;not null;index:idx_workflow_runs_list,priority:1"`
	Status     string    `gorm:"size:16;not null;index"`
	Data       string    `gorm:"type:text;not null"` // JSON of RunInstance
	LeaseOwner string    `gorm:"size:64;not null;default:''"`
	LeaseUntil time.Time `gorm:"not null"`
	CreatedAt  time.Time `gorm:"index:idx_workflow_runs_list,priority:2"`
	UpdatedAt  time.Time
}

func (WorkflowRun) TableName() string { return "workflow_runs" }

// WorkflowSignal is a signal waiting for a step (DatabaseStore).
type WorkflowSignal struct {
	ID        uint   `gorm:"primaryKey;autoIncrement"`
	RunID     string `gorm:"size:36;not null;index:idx_workflow_signals_wait,priority:1"`
	Event     string `gorm:"size:191;not null;index:idx_workflow_signals_wait,priority:2"`
	Data      string `gorm:"type:text;not null"`
	CreatedAt time.Time
}

func (WorkflowSignal) TableName() string { return "workflow_signals" }

// DatabaseStore keeps workflow runs in a SQL table so every instance sees
// them and they survive restarts.
type DatabaseStore struct {
	db *lucid.DB
}

// NewDatabaseStore returns a database-backed Store. Call EnsureTable once.
func NewDatabaseStore(db *lucid.DB) *DatabaseStore {
	return &DatabaseStore{db: db}
}

// EnsureTable creates or migrates the workflow_runs table.
func (s *DatabaseStore) EnsureTable(ctx context.Context) error {
	return s.db.WithContext(ctx).AutoMigrate(&WorkflowRun{}, &WorkflowSignal{})
}

func (s *DatabaseStore) Save(ctx context.Context, run *RunInstance) error {
	data, err := json.Marshal(run)
	if err != nil {
		return err
	}
	row := &WorkflowRun{
		ID:        run.ID,
		Workflow:  run.Workflow,
		Status:    string(run.Status),
		Data:      string(data),
		CreatedAt: run.CreatedAt,
	}
	// Leave lease columns alone on update: they belong to Claim/Renew.
	return s.db.WithContext(ctx).Clauses(lucidclause.OnConflict{
		Columns:   []lucidclause.Column{{Name: "id"}},
		DoUpdates: lucidclause.AssignmentColumns([]string{"status", "data", "updated_at"}),
	}).Create(row).Error
}

func (s *DatabaseStore) Load(ctx context.Context, id string) (*RunInstance, error) {
	var row WorkflowRun
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, lucid.ErrRecordNotFound) {
			return nil, fmt.Errorf("workflow run %q not found", id)
		}
		return nil, err
	}
	return row.run()
}

// List returns runs newest first.
func (s *DatabaseStore) List(ctx context.Context, workflow string, limit int) ([]*RunInstance, error) {
	q := s.db.WithContext(ctx).Order("created_at DESC")
	if workflow != "" {
		q = q.Where("workflow = ?", workflow)
	}
	if limit > 0 {
		q = q.Limit(limit)
	}
	var rows []WorkflowRun
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return runsFromRows(rows)
}

func (s *DatabaseStore) Delete(ctx context.Context, id string) error {
	if err := s.db.WithContext(ctx).Delete(&WorkflowSignal{}, "run_id = ?", id).Error; err != nil {
		return err
	}
	return s.db.WithContext(ctx).Delete(&WorkflowRun{}, "id = ?", id).Error
}

func (s *DatabaseStore) PushSignal(ctx context.Context, runID, event string, data Payload) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Create(&WorkflowSignal{RunID: runID, Event: event, Data: string(raw)}).Error
}

// PopSignal takes the oldest signal. The DELETE decides who gets it when
// several instances poll at once.
func (s *DatabaseStore) PopSignal(ctx context.Context, runID, event string) (Payload, bool, error) {
	for attempt := 0; attempt < 3; attempt++ {
		var sig WorkflowSignal
		res := s.db.WithContext(ctx).Where("run_id = ? AND event = ?", runID, event).
			Order("id ASC").Limit(1).Find(&sig)
		if res.Error != nil {
			return nil, false, res.Error
		}
		if res.RowsAffected == 0 {
			return nil, false, nil
		}
		del := s.db.WithContext(ctx).Delete(&WorkflowSignal{}, "id = ?", sig.ID)
		if del.Error != nil {
			return nil, false, del.Error
		}
		if del.RowsAffected == 0 {
			continue // another instance took it; try the next one
		}
		var data Payload
		if err := json.Unmarshal([]byte(sig.Data), &data); err != nil {
			return nil, false, err
		}
		return data, true, nil
	}
	return nil, false, nil
}

func (s *DatabaseStore) Claim(ctx context.Context, runID, owner string, ttl time.Duration) (bool, error) {
	now := time.Now()
	res := s.db.WithContext(ctx).Model(&WorkflowRun{}).
		Where("id = ? AND (lease_owner = '' OR lease_owner = ? OR lease_until < ?)", runID, owner, now).
		Updates(map[string]interface{}{"lease_owner": owner, "lease_until": now.Add(ttl)})
	return res.RowsAffected == 1, res.Error
}

func (s *DatabaseStore) Renew(ctx context.Context, runID, owner string, ttl time.Duration) (bool, error) {
	res := s.db.WithContext(ctx).Model(&WorkflowRun{}).
		Where("id = ? AND lease_owner = ?", runID, owner).
		Update("lease_until", time.Now().Add(ttl))
	return res.RowsAffected == 1, res.Error
}

func (s *DatabaseStore) Release(ctx context.Context, runID, owner string) error {
	return s.db.WithContext(ctx).Model(&WorkflowRun{}).
		Where("id = ? AND lease_owner = ?", runID, owner).
		Updates(map[string]interface{}{"lease_owner": "", "lease_until": time.Time{}}).Error
}

func (s *DatabaseStore) Unfinished(ctx context.Context, limit int) ([]*RunInstance, error) {
	q := s.db.WithContext(ctx).
		Where("status IN ?", []string{string(RunPending), string(RunRunning), string(RunPaused)}).
		Order("created_at ASC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	var rows []WorkflowRun
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return runsFromRows(rows)
}

func (r WorkflowRun) run() (*RunInstance, error) {
	var run RunInstance
	if err := json.Unmarshal([]byte(r.Data), &run); err != nil {
		return nil, fmt.Errorf("workflow run %q: %w", r.ID, err)
	}
	return &run, nil
}

func runsFromRows(rows []WorkflowRun) ([]*RunInstance, error) {
	out := make([]*RunInstance, 0, len(rows))
	for _, row := range rows {
		run, err := row.run()
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, nil
}

var (
	_ Store  = (*RedisStore)(nil)
	_ Leaser = (*RedisStore)(nil)
	_ Store  = (*DatabaseStore)(nil)
	_ Leaser = (*DatabaseStore)(nil)
	_ Leaser = (*MemoryStore)(nil)

	_ SignalStore = (*MemoryStore)(nil)
	_ SignalStore = (*RedisStore)(nil)
	_ SignalStore = (*DatabaseStore)(nil)
)
