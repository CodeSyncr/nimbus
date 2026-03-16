package cache

import (
	"errors"
	"sync"
	"time"
)

// ErrLockNotAcquired is returned when a lock cannot be acquired.
var ErrLockNotAcquired = errors.New("cache: lock not acquired")

// Lock provides a simple distributed-friendly mutual exclusion lock backed
// by the cache store. It prevents thundering herd / cache stampede by ensuring
// only one goroutine (or one process, for distributed stores like Redis)
// rebuilds a cache entry at a time.
//
// Usage:
//
//	lock := cache.NewLock(store, "lock:rebuild-reports", 30*time.Second)
//	acquired, err := lock.Acquire()
//	if err != nil || !acquired {
//	    return // another worker is rebuilding
//	}
//	defer lock.Release()
//	// ... rebuild expensive data ...
type Lock struct {
	store Store
	key   string
	ttl   time.Duration
	owner string
}

// NewLock creates a cache lock with the given key and TTL.
func NewLock(store Store, key string, ttl time.Duration) *Lock {
	return &Lock{
		store: store,
		key:   "nimbus:lock:" + key,
		ttl:   ttl,
		owner: randomOwner(),
	}
}

// Acquire attempts to acquire the lock. Returns true if successful.
func (l *Lock) Acquire() (bool, error) {
	// Check if lock already exists.
	if _, exists := l.store.Get(l.key); exists {
		return false, nil
	}
	// Set the lock with TTL.
	err := l.store.Set(l.key, l.owner, l.ttl)
	if err != nil {
		return false, err
	}
	return true, nil
}

// Release releases the lock (only if we are the owner).
func (l *Lock) Release() error {
	v, exists := l.store.Get(l.key)
	if !exists {
		return nil
	}
	if owner, ok := v.(string); ok && owner == l.owner {
		return l.store.Delete(l.key)
	}
	return nil
}

// Block attempts to acquire the lock, retrying at the given interval until
// the timeout is reached. Returns true if the lock was acquired.
func (l *Lock) Block(timeout time.Duration, interval time.Duration) (bool, error) {
	if interval <= 0 {
		interval = 100 * time.Millisecond
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		acquired, err := l.Acquire()
		if err != nil {
			return false, err
		}
		if acquired {
			return true, nil
		}
		time.Sleep(interval)
	}
	return false, nil
}

// AtomicLock acquires a lock, calls fn, then releases the lock.
// If the lock cannot be acquired, returns ErrLockNotAcquired.
//
//	err := cache.AtomicLock(store, "expensive-query", 30*time.Second, func() error {
//	    // rebuild cache
//	    return nil
//	})
func AtomicLock(store Store, key string, ttl time.Duration, fn func() error) error {
	lock := NewLock(store, key, ttl)
	acquired, err := lock.Acquire()
	if err != nil {
		return err
	}
	if !acquired {
		return ErrLockNotAcquired
	}
	defer lock.Release()
	return fn()
}

// ---------- owner generation ------------------------------------------------

var (
	ownerCounter uint64
	ownerMu      sync.Mutex
)

func randomOwner() string {
	ownerMu.Lock()
	defer ownerMu.Unlock()
	ownerCounter++
	return time.Now().Format("20060102150405") + "-" + uintToStr(ownerCounter)
}

func uintToStr(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf) - 1
	for n > 0 {
		buf[i] = byte('0' + n%10)
		n /= 10
		i--
	}
	return string(buf[i+1:])
}
