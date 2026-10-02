package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/CodeSyncr/nimbus/lucid"
	"github.com/CodeSyncr/nimbus/redis"
	"github.com/alicebob/miniredis/v2"
	"gorm.io/driver/sqlite"
)

func TestRunAllOK(t *testing.T) {
	c := New()
	c.Add("a", func(context.Context) error { return nil })
	c.Add("b", func(ctx context.Context) error {
		if _, ok := ctx.Deadline(); !ok {
			return errors.New("checks should run with a deadline")
		}
		return nil
	})
	r := c.Run(context.Background())
	if r.Status != "ok" || r.Checks["a"] != "ok" || r.Checks["b"] != "ok" {
		t.Fatalf("Run = %+v", r)
	}
}

func TestRunDegradedAndHandlerStatus(t *testing.T) {
	c := New()
	c.Add("ok", func(context.Context) error { return nil })
	c.Add("broken", func(context.Context) error { return errors.New("down") })

	rec := httptest.NewRecorder()
	c.Handler()(rec, httptest.NewRequest("GET", "/health", nil))
	if rec.Code != 503 {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	var r Result
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	if r.Status != "degraded" || r.Checks["broken"] != "down" || r.Checks["ok"] != "ok" {
		t.Fatalf("body = %+v", r)
	}
}

func TestHandlerOK(t *testing.T) {
	rec := httptest.NewRecorder()
	New().Handler()(rec, httptest.NewRequest("GET", "/health", nil))
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("status=%d content-type=%q", rec.Code, rec.Header().Get("Content-Type"))
	}
}

func TestDBAndRedisChecks(t *testing.T) {
	db, err := lucid.Open(sqlite.Open(filepath.Join(t.TempDir(), "h.db")), &lucid.Config{})
	if err != nil {
		t.Fatal(err)
	}
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	c := New()
	c.DB(db)
	c.Redis(rdb)
	if r := c.Run(context.Background()); r.Status != "ok" {
		t.Fatalf("Run = %+v", r)
	}
	mr.Close()
	if r := c.Run(context.Background()); r.Status != "degraded" || r.Checks["redis"] == "ok" {
		t.Fatalf("Run with Redis down = %+v", r)
	}
}
