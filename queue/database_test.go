package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/CodeSyncr/nimbus/lucid"
	"gorm.io/driver/sqlite"
)

func openQueueTestDB(t *testing.T) *lucid.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "queue-test.db")
	db, err := lucid.Open(sqlite.Open(dbPath), &lucid.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	return db
}

func TestDatabaseAdapterReclaimsStaleProcessingJob(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	db := openQueueTestDB(t)
	adapter := NewDatabaseAdapter(db)
	adapter.SetLeaseDuration(100 * time.Millisecond)
	if err := adapter.EnsureTable(ctx); err != nil {
		t.Fatalf("ensure queue table: %v", err)
	}

	payload := &JobPayload{
		ID:         "job-1",
		JobName:    "TestJob",
		Queue:      "default",
		Payload:    []byte(`{}`),
		Attempts:   0,
		MaxRetries: 1,
		RunAt:      time.Now().Add(-time.Second),
	}
	raw, _ := json.Marshal(payload)
	staleTime := time.Now().Add(-time.Second)
	row := &QueueJob{
		ID:        payload.ID,
		Queue:     payload.Queue,
		Payload:   raw,
		RunAt:     payload.RunAt,
		Status:    "processing",
		CreatedAt: staleTime,
		UpdatedAt: staleTime,
	}
	if err := db.WithContext(ctx).Create(row).Error; err != nil {
		t.Fatalf("seed stale processing row: %v", err)
	}

	popped, err := adapter.Pop(ctx, "default")
	if err != nil {
		t.Fatalf("pop reclaimed job: %v", err)
	}
	if popped == nil || popped.ID != payload.ID {
		t.Fatalf("expected reclaimed job %q, got %+v", payload.ID, popped)
	}

	if popped.Attempts != 1 {
		t.Fatalf("reclaimed delivery should count the lost attempt, got attempts=%d", popped.Attempts)
	}

	if err := adapter.Complete(ctx, popped); err != nil {
		t.Fatalf("complete reclaimed job: %v", err)
	}
	var n int64
	db.WithContext(ctx).Model(&QueueJob{}).Where("id = ?", payload.ID).Count(&n)
	if n != 0 {
		t.Fatalf("expected completed job row to be deleted, %d rows left", n)
	}
}

func newTestDBAdapter(t *testing.T) (*DatabaseAdapter, *lucid.DB) {
	t.Helper()
	db := openQueueTestDB(t)
	a := NewDatabaseAdapter(db)
	a.SetPollInterval(10 * time.Millisecond)
	if err := a.EnsureTable(context.Background()); err != nil {
		t.Fatal(err)
	}
	return a, db
}

func TestDatabaseAdapterRetryReusesRowAndLateAckKeepsIt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a, db := newTestDBAdapter(t)

	if err := a.Push(ctx, testPayload("job-1", 0)); err != nil {
		t.Fatal(err)
	}
	p, err := a.Pop(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	// What Manager.Process does on failure: requeue, then ack the delivery.
	if err := a.Push(ctx, requeueCopy(p, 0)); err != nil {
		t.Fatalf("retry push of an existing job ID failed: %v", err)
	}
	if err := a.Complete(ctx, p); err != nil {
		t.Fatal(err)
	}
	var n int64
	db.WithContext(ctx).Model(&QueueJob{}).Where("id = ? AND status = ?", "job-1", "pending").Count(&n)
	if n != 1 {
		t.Fatalf("retry row should survive the ack of the previous delivery")
	}
	again, err := a.Pop(ctx, "default")
	if err != nil || again.ID != "job-1" {
		t.Fatalf("retry not delivered: %v %v", again, err)
	}
}

func TestDatabaseAdapterExtendLease(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a, _ := newTestDBAdapter(t)
	a.SetLeaseDuration(time.Second)

	_ = a.Push(ctx, testPayload("job-1", 0))
	p, err := a.Pop(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ExtendLease(ctx, p); err != nil {
		t.Fatalf("extend lease: %v", err)
	}
	_ = a.Complete(ctx, p)
	if err := a.ExtendLease(ctx, p); err != ErrLeaseLost {
		t.Fatalf("extend after complete = %v, want ErrLeaseLost", err)
	}
}

func TestDatabaseAdapterDeliversEachJobOnce(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a, _ := newTestDBAdapter(t)

	const jobs = 30
	for i := 0; i < jobs; i++ {
		_ = a.Push(ctx, testPayload(fmt.Sprintf("job-%d", i), 0))
	}
	var mu sync.Mutex
	seen := map[string]int{}
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				mu.Lock()
				done := len(seen) == jobs
				mu.Unlock()
				if done {
					return
				}
				popCtx, c := context.WithTimeout(ctx, 200*time.Millisecond)
				p, err := a.Pop(popCtx, "default")
				c()
				if err != nil || p == nil {
					continue
				}
				mu.Lock()
				seen[p.ID]++
				mu.Unlock()
				_ = a.Complete(ctx, p)
			}
		}()
	}
	wg.Wait()
	for id, n := range seen {
		if n != 1 {
			t.Errorf("job %s delivered %d times", id, n)
		}
	}
}
