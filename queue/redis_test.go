package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CodeSyncr/nimbus/redis"
	"github.com/alicebob/miniredis/v2"
)

// newTestRedis returns a client backed by miniredis, or by a real server
// when NIMBUS_TEST_REDIS_URL is set (the race tests are more convincing
// against real Redis, which runs scripts and commands on separate threads
// of the client).
func newTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	if url := os.Getenv("NIMBUS_TEST_REDIS_URL"); url != "" {
		opt, err := redis.ParseURL(url)
		if err != nil {
			t.Fatalf("parse NIMBUS_TEST_REDIS_URL: %v", err)
		}
		c := redis.NewClient(opt)
		if err := c.FlushDB(context.Background()).Err(); err != nil {
			t.Fatalf("flush test redis: %v", err)
		}
		t.Cleanup(func() { _ = c.Close() })
		return c
	}
	mr := miniredis.RunT(t)
	c := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func jsonMarshal(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

func testPayload(id string, delay time.Duration) *JobPayload {
	return &JobPayload{
		ID:         id,
		JobName:    "testJob",
		Queue:      "default",
		Payload:    []byte(`{}`),
		MaxRetries: 3,
		Delay:      delay,
		RunAt:      time.Now().Add(delay),
	}
}

// drain runs workers Pop+Complete loops until every expected job has been
// delivered and the queue has stayed quiet for a moment, returning how
// many times each job ID was delivered.
func drain(t *testing.T, a *RedisAdapter, workers, expected int, hold time.Duration) map[string]int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var (
		mu       sync.Mutex
		seen     = map[string]int{}
		distinct int32
	)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				p, err := a.Pop(ctx, "default")
				if err != nil || p == nil {
					return
				}
				mu.Lock()
				seen[p.ID]++
				if seen[p.ID] == 1 {
					atomic.AddInt32(&distinct, 1)
				}
				mu.Unlock()
				if hold > 0 {
					time.Sleep(hold)
				}
				_ = a.Complete(ctx, p)
			}
		}()
	}
	for atomic.LoadInt32(&distinct) < int32(expected) && ctx.Err() == nil {
		time.Sleep(10 * time.Millisecond)
	}
	// Give stragglers a chance to show up as duplicates.
	time.Sleep(300 * time.Millisecond)
	cancel()
	wg.Wait()
	return seen
}

func TestRedisAdapterDelayedJobsDeliveredOnce(t *testing.T) {
	c := newTestRedis(t)
	a := NewRedisAdapter(c)
	ctx := context.Background()

	const jobs, workers = 200, 16
	for i := 0; i < jobs; i++ {
		if err := a.Push(ctx, testPayload(fmt.Sprintf("job-%d", i), time.Millisecond)); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(20 * time.Millisecond)

	seen := drain(t, a, workers, jobs, 0)
	if len(seen) != jobs {
		t.Fatalf("delivered %d distinct jobs, want %d", len(seen), jobs)
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("job %s delivered %d times", id, n)
		}
	}
}

func TestRedisAdapterImmediateJobsFIFOAndOnce(t *testing.T) {
	c := newTestRedis(t)
	a := NewRedisAdapter(c)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if err := a.Push(ctx, testPayload(fmt.Sprintf("job-%d", i), 0)); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 5; i++ {
		p, err := a.Pop(ctx, "default")
		if err != nil {
			t.Fatal(err)
		}
		if want := fmt.Sprintf("job-%d", i); p.ID != want {
			t.Fatalf("pop %d = %s, want %s (FIFO)", i, p.ID, want)
		}
		if err := a.Complete(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	if n, _ := a.Len(ctx, "default"); n != 0 {
		t.Fatalf("Len = %d after draining", n)
	}
}

func TestRedisAdapterReclaimsExpiredLeaseOnceAndCountsAttempt(t *testing.T) {
	c := newTestRedis(t)
	a := NewRedisAdapter(c)
	a.SetVisibilityTimeout(50 * time.Millisecond)
	ctx := context.Background()

	if err := a.Push(ctx, testPayload("job-1", 0)); err != nil {
		t.Fatal(err)
	}
	p, err := a.Pop(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	if p.Attempts != 0 {
		t.Fatalf("first delivery attempts = %d", p.Attempts)
	}
	// Simulate a crashed worker: never Complete, let the lease lapse.
	time.Sleep(100 * time.Millisecond)

	seen := drain(t, a, 8, 1, 0)
	if seen["job-1"] != 1 {
		t.Fatalf("reclaimed job delivered %d times, want 1", seen["job-1"])
	}

	// The reclaimed delivery should carry the lost attempt.
	if err := a.Push(ctx, testPayload("job-2", 0)); err != nil {
		t.Fatal(err)
	}
	p, _ = a.Pop(ctx, "default")
	time.Sleep(100 * time.Millisecond)
	p2, err := a.Pop(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	if p2.ID != p.ID || p2.Attempts != 1 {
		t.Fatalf("reclaimed delivery = %s attempts %d, want %s attempts 1", p2.ID, p2.Attempts, p.ID)
	}
	if err := a.Complete(ctx, p2); err != nil {
		t.Fatal(err)
	}
	if n := c.HLen(ctx, redisReclaimsPrefix+"default").Val(); n != 0 {
		t.Fatalf("reclaim counter not cleared on complete: %d entries", n)
	}
}

func TestRedisAdapterExtendLeaseKeepsLongJobLeased(t *testing.T) {
	c := newTestRedis(t)
	a := NewRedisAdapter(c)
	a.SetVisibilityTimeout(80 * time.Millisecond)
	ctx := context.Background()

	if err := a.Push(ctx, testPayload("long", 0)); err != nil {
		t.Fatal(err)
	}
	p, err := a.Pop(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	// Heartbeat for well past the visibility timeout.
	for i := 0; i < 6; i++ {
		time.Sleep(40 * time.Millisecond)
		if err := a.ExtendLease(ctx, p); err != nil {
			t.Fatalf("extend lease: %v", err)
		}
	}
	popCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	if again, _ := a.Pop(popCtx, "default"); again != nil {
		t.Fatalf("job redelivered while its lease was being extended")
	}
	if err := a.Complete(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := a.ExtendLease(ctx, p); err != ErrLeaseLost {
		t.Fatalf("extend after complete = %v, want ErrLeaseLost", err)
	}
}

func TestRedisAdapterLateCompleteDoesNotDropNewLease(t *testing.T) {
	c := newTestRedis(t)
	a := NewRedisAdapter(c)
	a.SetVisibilityTimeout(50 * time.Millisecond)
	ctx := context.Background()

	_ = a.Push(ctx, testPayload("job-1", 0))
	first, _ := a.Pop(ctx, "default")
	time.Sleep(100 * time.Millisecond)
	second, err := a.Pop(ctx, "default")
	if err != nil || second == nil {
		t.Fatalf("expected reclaimed delivery, got %v", err)
	}
	// The first (slow) worker finishes late. It must not ack the second lease.
	_ = a.Complete(ctx, first)
	if n := c.ZCard(ctx, redisInFlightPrefix+"default").Val(); n != 1 {
		t.Fatalf("in-flight leases = %d, want 1 (second worker's)", n)
	}
	_ = a.Complete(ctx, second)
	if n := c.ZCard(ctx, redisInFlightPrefix+"default").Val(); n != 0 {
		t.Fatalf("in-flight leases = %d after completing", n)
	}
}

func TestRedisAdapterMigratesLegacySecondScores(t *testing.T) {
	c := newTestRedis(t)
	a := NewRedisAdapter(c)
	ctx := context.Background()

	// A delayed job written by an older version (integer second score) that
	// is not due yet must not be promoted early.
	future := testPayload("future", 0)
	raw, _ := jsonMarshal(future)
	c.ZAdd(ctx, redisDelayedPrefix+"default", redis.Z{Score: float64(time.Now().Add(time.Hour).Unix()), Member: raw})

	popCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	if p, _ := a.Pop(popCtx, "default"); p != nil {
		t.Fatalf("future legacy job delivered early")
	}

	// A legacy in-flight entry (member is the raw payload) is still reclaimed.
	legacy := testPayload("legacy", 0)
	raw, _ = jsonMarshal(legacy)
	c.ZAdd(ctx, redisInFlightPrefix+"default", redis.Z{Score: float64(time.Now().Add(-time.Second).Unix()), Member: raw})
	c.RPush(ctx, redisProcessingPrefix+"default", raw)
	p, err := a.Pop(ctx, "default")
	if err != nil || p == nil || p.ID != "legacy" {
		t.Fatalf("legacy in-flight job not reclaimed: %v %v", p, err)
	}
	if n := c.LLen(ctx, redisProcessingPrefix+"default").Val(); n != 0 {
		t.Fatalf("legacy processing list not cleaned: %d", n)
	}
}
