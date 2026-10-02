package workflow

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CodeSyncr/nimbus/lucid"
	"github.com/CodeSyncr/nimbus/redis"
	"github.com/alicebob/miniredis/v2"
	"gorm.io/driver/sqlite"
)

type durableStore interface {
	Store
	Leaser
}

// advance lets time pass for lease expiry; miniredis only expires keys
// when told to.
var advance = time.Sleep

func stores(t *testing.T) map[string]func() durableStore {
	return map[string]func() durableStore{
		"memory": func() durableStore { advance = time.Sleep; return NewMemoryStore() },
		"redis": func() durableStore {
			mr := miniredis.RunT(t)
			advance = func(d time.Duration) { mr.FastForward(d) }
			c := redis.NewClient(&redis.Options{Addr: mr.Addr()})
			t.Cleanup(func() { _ = c.Close() })
			return NewRedisStore(c)
		},
		"database": func() durableStore {
			advance = time.Sleep
			db, err := lucid.Open(sqlite.Open(filepath.Join(t.TempDir(), "wf.db")), &lucid.Config{})
			if err != nil {
				t.Fatal(err)
			}
			s := NewDatabaseStore(db)
			if err := s.EnsureTable(context.Background()); err != nil {
				t.Fatal(err)
			}
			return s
		},
	}
}

func TestStoresSaveLoadListDelete(t *testing.T) {
	for name, mk := range stores(t) {
		t.Run(name, func(t *testing.T) {
			s := mk()
			ctx := context.Background()
			base := time.Now().Add(-time.Hour)
			for i, id := range []string{"a", "b", "c"} {
				wf := "onboard"
				if id == "c" {
					wf = "billing"
				}
				run := &RunInstance{ID: id, Workflow: wf, Status: RunRunning, Payload: Payload{"n": float64(i)},
					Steps: map[string]*StepInstance{"s": {Name: "s", Status: StatusPending}}, CreatedAt: base.Add(time.Duration(i) * time.Minute)}
				if err := s.Save(ctx, run); err != nil {
					t.Fatal(err)
				}
			}
			got, err := s.Load(ctx, "b")
			if err != nil || got.Workflow != "onboard" || got.Payload["n"] != float64(1) {
				t.Fatalf("Load = %+v, %v", got, err)
			}
			// Saving again updates in place.
			got.Status = RunCompleted
			if err := s.Save(ctx, got); err != nil {
				t.Fatal(err)
			}
			if again, _ := s.Load(ctx, "b"); again.Status != RunCompleted {
				t.Fatalf("status after update = %s", again.Status)
			}
			list, err := s.List(ctx, "onboard", 10)
			if err != nil || len(list) != 2 {
				t.Fatalf("List(onboard) = %d runs, %v", len(list), err)
			}
			unfinished, err := s.Unfinished(ctx, 0)
			if err != nil || len(unfinished) != 2 {
				t.Fatalf("Unfinished = %d runs, %v", len(unfinished), err)
			}
			if err := s.Delete(ctx, "a"); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Load(ctx, "a"); err == nil {
				t.Fatal("deleted run still loads")
			}
		})
	}
}

func TestStoresLeases(t *testing.T) {
	for name, mk := range stores(t) {
		t.Run(name, func(t *testing.T) {
			s := mk()
			ctx := context.Background()
			_ = s.Save(ctx, &RunInstance{ID: "r", Workflow: "w", Status: RunRunning, CreatedAt: time.Now()})
			if ok, _ := s.Claim(ctx, "r", "one", 100*time.Millisecond); !ok {
				t.Fatal("first claim failed")
			}
			if ok, _ := s.Claim(ctx, "r", "two", time.Second); ok {
				t.Fatal("second instance claimed a live lease")
			}
			if ok, _ := s.Renew(ctx, "r", "two", time.Second); ok {
				t.Fatal("non-owner renewed the lease")
			}
			if ok, _ := s.Renew(ctx, "r", "one", 100*time.Millisecond); !ok {
				t.Fatal("owner could not renew")
			}
			advance(150 * time.Millisecond)
			if ok, _ := s.Claim(ctx, "r", "two", time.Second); !ok {
				t.Fatal("expired lease could not be taken over")
			}
			_ = s.Release(ctx, "r", "two")
			if ok, _ := s.Claim(ctx, "r", "three", time.Second); !ok {
				t.Fatal("released lease could not be claimed")
			}
		})
	}
}

func TestResumeContinuesInterruptedRunOnce(t *testing.T) {
	for name, mk := range stores(t) {
		t.Run(name, func(t *testing.T) {
			s := mk()
			ctx := context.Background()
			var firstRuns, secondRuns, thirdRuns int32
			def := Define("onboard", func(r *Run) {
				r.Step("first", func(context.Context, Payload) (Payload, error) {
					atomic.AddInt32(&firstRuns, 1)
					return nil, nil
				})
				r.Step("second", func(context.Context, Payload) (Payload, error) {
					atomic.AddInt32(&secondRuns, 1)
					return Payload{"second": "done"}, nil
				}).After("first")
				r.Step("third", func(context.Context, Payload) (Payload, error) {
					atomic.AddInt32(&thirdRuns, 1)
					return nil, nil
				}).After("second")
			})

			// State left by an instance that died while running "second".
			_ = s.Save(ctx, &RunInstance{
				ID: "run-1", Workflow: "onboard", Status: RunRunning, Payload: Payload{},
				Steps: map[string]*StepInstance{
					"first":  {Name: "first", Status: StatusCompleted},
					"second": {Name: "second", Status: StatusRunning},
					"third":  {Name: "third", Status: StatusPending},
				},
				CreatedAt: time.Now(),
			})

			engines := []*Engine{NewEngine(s), NewEngine(s)}
			var wg sync.WaitGroup
			var taken int32
			for _, e := range engines {
				e.Register(def)
				wg.Add(1)
				go func(e *Engine) {
					defer wg.Done()
					n, err := e.Resume(ctx)
					if err != nil {
						t.Error(err)
					}
					atomic.AddInt32(&taken, int32(n))
				}(e)
			}
			wg.Wait()
			if taken != 1 {
				t.Fatalf("%d engines resumed the run, want 1", taken)
			}
			deadline := time.Now().Add(3 * time.Second)
			for {
				run, err := s.Load(ctx, "run-1")
				if err == nil && run.Status == RunCompleted {
					if run.Payload["second"] != "done" {
						t.Fatalf("payload = %+v", run.Payload)
					}
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("run did not complete: %+v", run)
				}
				time.Sleep(10 * time.Millisecond)
			}
			if firstRuns != 0 || secondRuns != 1 || thirdRuns != 1 {
				t.Fatalf("runs first=%d second=%d third=%d, want 0/1/1", firstRuns, secondRuns, thirdRuns)
			}
		})
	}
}

func TestResumeLeavesLiveRunsAlone(t *testing.T) {
	s := NewMemoryStore()
	block := make(chan struct{})
	def := Define("slow", func(r *Run) {
		r.Step("wait", func(context.Context, Payload) (Payload, error) {
			<-block
			return nil, nil
		})
	})
	owner := NewEngine(s)
	owner.Register(def)
	other := NewEngine(s)
	other.Register(def)

	if _, err := owner.Dispatch("slow", Payload{}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if n, _ := other.Resume(context.Background()); n != 0 {
		t.Fatalf("another engine resumed a run that is still running")
	}
	close(block)
}

func TestDispatchSyncFailureAndCondition(t *testing.T) {
	e := NewEngine(nil)
	e.Register(Define("w", func(r *Run) {
		r.Step("skip", func(context.Context, Payload) (Payload, error) { return nil, nil }).
			When(func(Payload) bool { return false })
		r.Step("boom", func(context.Context, Payload) (Payload, error) { return nil, errors.New("nope") })
		r.Step("after", func(context.Context, Payload) (Payload, error) { return nil, nil }).After("boom")
	}))
	run, err := e.DispatchSync(context.Background(), "w", Payload{})
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != RunFailed || run.Steps["skip"].Status != StatusSkipped || run.Steps["after"].Status != StatusCancelled {
		t.Fatalf("unexpected run state: status=%s skip=%s after=%s", run.Status, run.Steps["skip"].Status, run.Steps["after"].Status)
	}
}
