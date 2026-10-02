/*
|--------------------------------------------------------------------------
| Queue Locks
|--------------------------------------------------------------------------
|
| Unique jobs and WithoutOverlapping need locks every worker and web
| instance can see. Boot installs a Redis or database Locker to match the
| queue driver; the in-memory default only coordinates one process.
|
*/

package queue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/CodeSyncr/nimbus/lucid"
	lucidclause "github.com/CodeSyncr/nimbus/lucid/clause"
	"github.com/CodeSyncr/nimbus/redis"
)

// Locker hands out expiring exclusive locks.
type Locker interface {
	// Acquire takes key for ttl. ok is false when someone else holds it.
	// The returned token releases the lock.
	Acquire(ctx context.Context, key string, ttl time.Duration) (token string, ok bool, err error)
	// Release drops key if token still holds it. An empty token drops the
	// lock whoever holds it.
	Release(ctx context.Context, key, token string) error
}

var (
	lockerMu      sync.RWMutex
	defaultLocker Locker = NewMemoryLocker()
)

// SetLocker sets the Locker used for unique jobs and WithoutOverlapping.
// Boot calls it for the redis and database drivers.
func SetLocker(l Locker) {
	if l == nil {
		l = NewMemoryLocker()
	}
	lockerMu.Lock()
	defaultLocker = l
	lockerMu.Unlock()
}

// GetLocker returns the Locker used for unique jobs and WithoutOverlapping.
func GetLocker() Locker {
	lockerMu.RLock()
	defer lockerMu.RUnlock()
	return defaultLocker
}

func newLockToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ── Memory ──────────────────────────────────────────────────────────

// MemoryLocker keeps locks in this process only.
type MemoryLocker struct {
	mu    sync.Mutex
	locks map[string]memoryLock
}

type memoryLock struct {
	token string
	exp   time.Time
}

// NewMemoryLocker returns a process-local Locker.
func NewMemoryLocker() *MemoryLocker {
	return &MemoryLocker{locks: make(map[string]memoryLock)}
}

func (m *MemoryLocker) Acquire(_ context.Context, key string, ttl time.Duration) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if l, ok := m.locks[key]; ok && now.Before(l.exp) {
		return "", false, nil
	}
	// Drop expired locks as we go so the map cannot grow without bound.
	if len(m.locks) > 1024 {
		for k, l := range m.locks {
			if !now.Before(l.exp) {
				delete(m.locks, k)
			}
		}
	}
	token := newLockToken()
	m.locks[key] = memoryLock{token: token, exp: now.Add(ttl)}
	return token, true, nil
}

func (m *MemoryLocker) Release(_ context.Context, key, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if l, ok := m.locks[key]; ok && (token == "" || l.token == token) {
		delete(m.locks, key)
	}
	return nil
}

// ── Redis ───────────────────────────────────────────────────────────

var releaseLockScript = redis.NewScript(`
if ARGV[1] == '' or redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('DEL', KEYS[1])
end
return 0
`)

// RedisLocker uses SET NX PX, so locks are shared by every process using
// the same Redis.
type RedisLocker struct {
	client *redis.Client
}

// NewRedisLocker returns a Redis-backed Locker.
func NewRedisLocker(client *redis.Client) *RedisLocker {
	return &RedisLocker{client: client}
}

func (r *RedisLocker) Acquire(ctx context.Context, key string, ttl time.Duration) (string, bool, error) {
	token := newLockToken()
	ok, err := r.client.SetNX(ctx, key, token, ttl).Result()
	if err != nil || !ok {
		return "", false, err
	}
	return token, true, nil
}

func (r *RedisLocker) Release(ctx context.Context, key, token string) error {
	return releaseLockScript.Run(ctx, r.client, []string{key}, token).Err()
}

// ── Database ────────────────────────────────────────────────────────

// QueueLock is the database model for DatabaseLocker.
type QueueLock struct {
	Key       string    `gorm:"column:lock_key;primaryKey;size:191"`
	Token     string    `gorm:"size:32;not null"`
	ExpiresAt time.Time `gorm:"index;not null"`
}

func (QueueLock) TableName() string { return "queue_locks" }

// DatabaseLocker stores locks as rows keyed by lock name, so the primary
// key decides who wins.
type DatabaseLocker struct {
	db *lucid.DB
}

// NewDatabaseLocker returns a database-backed Locker. Call EnsureTable once.
func NewDatabaseLocker(db *lucid.DB) *DatabaseLocker {
	return &DatabaseLocker{db: db}
}

// EnsureTable creates the queue_locks table if it does not exist.
func (d *DatabaseLocker) EnsureTable(ctx context.Context) error {
	return d.db.WithContext(ctx).AutoMigrate(&QueueLock{})
}

func (d *DatabaseLocker) Acquire(ctx context.Context, key string, ttl time.Duration) (string, bool, error) {
	db := d.db.WithContext(ctx)
	now := time.Now()
	if err := db.Where("lock_key = ? AND expires_at <= ?", key, now).Delete(&QueueLock{}).Error; err != nil {
		return "", false, err
	}
	token := newLockToken()
	res := db.Clauses(lucidclause.OnConflict{DoNothing: true}).
		Create(&QueueLock{Key: key, Token: token, ExpiresAt: now.Add(ttl)})
	if res.Error != nil {
		if isDuplicateKey(res.Error) {
			return "", false, nil
		}
		return "", false, res.Error
	}
	return token, res.RowsAffected == 1, nil
}

func (d *DatabaseLocker) Release(ctx context.Context, key, token string) error {
	q := d.db.WithContext(ctx).Where("lock_key = ?", key)
	if token != "" {
		q = q.Where("token = ?", token)
	}
	return q.Delete(&QueueLock{}).Error
}

func isDuplicateKey(err error) bool {
	if errors.Is(err, lucid.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate") || strings.Contains(msg, "unique constraint")
}

// ScheduleLocker adapts a Locker to schedule.Locker, so scheduled tasks
// lock through the same backend as the queue.
type ScheduleLocker struct{ Locker Locker }

// TryLock implements schedule.Locker.
func (s ScheduleLocker) TryLock(ctx context.Context, key string, ttl time.Duration) (func(), bool, error) {
	token, ok, err := s.Locker.Acquire(ctx, key, ttl)
	if err != nil || !ok {
		return nil, false, err
	}
	return func() { _ = s.Locker.Release(context.WithoutCancel(ctx), key, token) }, true, nil
}

var (
	_ Locker = (*MemoryLocker)(nil)
	_ Locker = (*RedisLocker)(nil)
	_ Locker = (*DatabaseLocker)(nil)
)
