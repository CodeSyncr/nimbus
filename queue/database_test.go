package queue

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"github.com/CodeSyncr/nimbus/lucid"
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

	if err := adapter.Complete(ctx, popped); err != nil {
		t.Fatalf("complete reclaimed job: %v", err)
	}
	var status string
	if err := db.WithContext(ctx).Model(&QueueJob{}).Select("status").Where("id = ?", payload.ID).Scan(&status).Error; err != nil {
		t.Fatalf("load status: %v", err)
	}
	if status != "done" {
		t.Fatalf("expected status done, got %s", status)
	}
}
