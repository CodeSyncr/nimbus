package database

import (
	"sync"
	"testing"
	"time"

	"github.com/CodeSyncr/nimbus/lucid"
	"gorm.io/driver/sqlite"
)

func TestPoolConfigFromFields(t *testing.T) {
	p := PoolConfigFromFields(10, 3, "5m", "1m")
	if p.MaxOpenConns != 10 || p.MaxIdleConns != 3 {
		t.Fatalf("open/idle: %+v", p)
	}
	if p.ConnMaxLifetime != 5*time.Minute || p.ConnMaxIdleTime != time.Minute {
		t.Fatalf("durations: %+v", p)
	}

	invalid := PoolConfigFromFields(0, 0, "not-a-duration", "x")
	if invalid.ConnMaxLifetime != 0 || invalid.ConnMaxIdleTime != 0 {
		t.Fatalf("invalid strings should be ignored: %+v", invalid)
	}
}

func TestApplyPool(t *testing.T) {
	db, err := lucid.Open(sqlite.Open("file::memory:?cache=shared"), &lucid.Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		s, _ := db.DB()
		if s != nil {
			_ = s.Close()
		}
	}()

	if err := ApplyPool(db, PoolConfig{
		MaxOpenConns:    7,
		MaxIdleConns:    2,
		ConnMaxLifetime: time.Minute,
		ConnMaxIdleTime: 30 * time.Second,
	}); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if sqlDB.Stats().MaxOpenConnections != 7 {
		t.Fatalf("MaxOpenConnections = %d", sqlDB.Stats().MaxOpenConnections)
	}
}

// With no pool settings, connections opened for concurrent queries stay in
// the pool for the next requests instead of being closed (Go keeps 2).
func TestApplyPoolDefaultKeepsConnectionsIdle(t *testing.T) {
	db := openTestDB(t)
	if err := ApplyPool(db, PoolConfig{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			tx, err := sqlDB.Begin() // holds a connection until Commit
			if err != nil {
				t.Error(err)
				return
			}
			time.Sleep(50 * time.Millisecond)
			_ = tx.Commit()
		}()
	}
	close(start)
	wg.Wait()
	if idle := sqlDB.Stats().Idle; idle < 10 {
		t.Fatalf("idle connections after 10 concurrent queries = %d, want 10", idle)
	}
}
