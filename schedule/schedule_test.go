package schedule

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
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


func TestStartPicksRedisLockerFromEnv(t *testing.T) {
	mr := miniredis.RunT(t)
	t.Setenv("REDIS_URL", "redis://"+mr.Addr())
	t.Setenv("SCHEDULE_LOCK", "")
	SetDefaultLocker(nil)

	var runs int32
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Two "instances" with no WithLocker call.
	for i := 0; i < 2; i++ {
		s := New().Every(100*time.Millisecond, "tick", func(context.Context) error {
			atomic.AddInt32(&runs, 1)
			return nil
		})
		s.Start(ctx)
		if s.locker == nil {
			t.Fatal("Start should pick a Redis locker from REDIS_URL")
		}
	}
	time.Sleep(450 * time.Millisecond)
	cancel()
	// ~4 ticks; unlocked it would be ~8.
	if n := atomic.LoadInt32(&runs); n < 2 || n > 5 {
		t.Fatalf("task ran %d times across two instances, want one run per tick", n)
	}
}

func TestStartRespectsLockOptOutAndExplicitLocker(t *testing.T) {
	t.Setenv("REDIS_URL", "redis://127.0.0.1:1")
	t.Setenv("SCHEDULE_LOCK", "off")
	s := New().Every(time.Hour, "x", func(context.Context) error { return nil })
	s.Start(context.Background())
	s.Stop()
	if s.locker != nil {
		t.Fatal("SCHEDULE_LOCK=off should leave tasks unlocked")
	}

	t.Setenv("SCHEDULE_LOCK", "")
	own := &testLocker{}
	s = New().WithLocker(own).Every(time.Hour, "x", func(context.Context) error { return nil })
	s.Start(context.Background())
	s.Stop()
	if s.locker != own {
		t.Fatal("Start replaced an explicitly configured locker")
	}
}
