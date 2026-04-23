package schedule

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type testLocker struct {
	mu    sync.Mutex
	locks map[string]struct{}
}

func (l *testLocker) TryLock(_ context.Context, key string, _ time.Duration) (func(), bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.locks == nil {
		l.locks = make(map[string]struct{})
	}
	if _, ok := l.locks[key]; ok {
		return nil, false, nil
	}
	l.locks[key] = struct{}{}
	return func() {
		l.mu.Lock()
		delete(l.locks, key)
		l.mu.Unlock()
	}, true, nil
}

func TestSchedulerExecuteUsesDistributedLock(t *testing.T) {
	locker := &testLocker{}
	var runs int32

	s1 := New().WithLocker(locker)
	s2 := New().WithLocker(locker)
	e := entry{
		name:     "nightly",
		interval: time.Minute,
		task: func(context.Context) error {
			atomic.AddInt32(&runs, 1)
			return nil
		},
	}

	// Same task + same time bucket: only one should execute.
	s1.execute(context.Background(), e)
	s2.execute(context.Background(), e)

	if got := atomic.LoadInt32(&runs); got != 1 {
		t.Fatalf("expected exactly one execution, got %d", got)
	}
}

