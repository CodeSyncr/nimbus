package workflow

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func waitStatus(t *testing.T, s Store, runID string, want RunStatus) *RunInstance {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		run, err := s.Load(context.Background(), runID)
		if err == nil && run.Status == want {
			return run
		}
		if time.Now().After(deadline) {
			t.Fatalf("run %s never reached %s (last: %+v, %v)", runID, want, run, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestParallelStepsShareThePayloadSafely(t *testing.T) {
	e := NewEngine(nil)
	e.Register(Define("fan-out", func(r *Run) {
		for i := 0; i < 8; i++ {
			key := fmt.Sprintf("k%d", i)
			r.Step(key, func(_ context.Context, p Payload) (Payload, error) {
				_ = len(p) // reads the payload while siblings write theirs
				for j := 0; j < 100; j++ {
					p[key+"-scratch"] = j // writes to its own copy only
				}
				return Payload{key: true}, nil
			}).Parallel()
		}
	}))
	run, err := e.DispatchSync(context.Background(), "fan-out", Payload{"seed": 1})
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != RunCompleted {
		t.Fatalf("status = %s", run.Status)
	}
	for i := 0; i < 8; i++ {
		if run.Payload[fmt.Sprintf("k%d", i)] != true {
			t.Fatalf("output of step k%d missing: %v", i, run.Payload)
		}
	}
	if _, leaked := run.Payload["k0-scratch"]; leaked {
		t.Fatal("a step's in-place payload edits leaked into the run")
	}
}

func TestCancelStopsRunningRun(t *testing.T) {
	s := NewMemoryStore()
	e := NewEngine(s)
	var after int32
	started := make(chan struct{})
	e.Register(Define("long", func(r *Run) {
		r.Step("slow", func(ctx context.Context, _ Payload) (Payload, error) {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		})
		r.Step("next", func(context.Context, Payload) (Payload, error) {
			atomic.AddInt32(&after, 1)
			return nil, nil
		}).After("slow")
	}))
	id, err := e.Dispatch("long", Payload{})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if err := e.Cancel(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	run := waitStatus(t, s, id, RunCancelled)
	time.Sleep(50 * time.Millisecond)
	run, _ = s.Load(context.Background(), id)
	if run.Status != RunCancelled {
		t.Fatalf("cancellation was overwritten: %s", run.Status)
	}
	if run.Steps["slow"].Status != StatusCancelled || run.Steps["next"].Status != StatusCancelled {
		t.Fatalf("steps = slow:%s next:%s", run.Steps["slow"].Status, run.Steps["next"].Status)
	}
	if after != 0 {
		t.Fatal("a step after the cancellation ran")
	}
}

func TestCancelFromAnotherInstance(t *testing.T) {
	for name, mk := range stores(t) {
		t.Run(name, func(t *testing.T) {
			s := mk()
			started := make(chan struct{})
			def := Define("long", func(r *Run) {
				r.Step("slow", func(ctx context.Context, _ Payload) (Payload, error) {
					close(started)
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					case <-time.After(5 * time.Second):
						return nil, nil
					}
				})
			})
			runner, other := NewEngine(s), NewEngine(s)
			runner.SetPollInterval(20 * time.Millisecond)
			runner.Register(def)
			other.Register(def)

			id, err := runner.Dispatch("long", Payload{})
			if err != nil {
				t.Fatal(err)
			}
			<-started
			start := time.Now()
			if err := other.Cancel(context.Background(), id); err != nil {
				t.Fatal(err)
			}
			// The runner must notice, stop the step, and keep the status.
			deadline := time.Now().Add(3 * time.Second)
			for runner.execution(id) != nil {
				if time.Now().After(deadline) {
					t.Fatal("running instance did not stop the cancelled run")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if time.Since(start) > 2*time.Second {
				t.Fatal("cancellation took the full step duration")
			}
			waitStatus(t, s, id, RunCancelled)
		})
	}
}

func TestSignalReachesRunOnAnotherInstance(t *testing.T) {
	for name, mk := range stores(t) {
		t.Run(name, func(t *testing.T) {
			s := mk()
			def := Define("approve", func(r *Run) {
				r.Step("wait", nil).WaitForEvent("approved", 5*time.Second)
				r.Step("done", func(_ context.Context, p Payload) (Payload, error) {
					return Payload{"by": p["approver"]}, nil
				}).After("wait")
			})
			runner, other := NewEngine(s), NewEngine(s)
			runner.SetPollInterval(20 * time.Millisecond)
			runner.Register(def)
			other.Register(def)

			id, err := runner.Dispatch("approve", Payload{})
			if err != nil {
				t.Fatal(err)
			}
			waitStatus(t, s, id, RunPaused)
			if err := other.Signal(id, "approved", Payload{"approver": "ana"}); err != nil {
				t.Fatalf("signal via another instance: %v", err)
			}
			run := waitStatus(t, s, id, RunCompleted)
			if run.Payload["by"] != "ana" {
				t.Fatalf("payload = %v", run.Payload)
			}
		})
	}
}

func TestSignalSentBeforeTheStepWaits(t *testing.T) {
	s := NewMemoryStore()
	e := NewEngine(s)
	e.SetPollInterval(20 * time.Millisecond)
	release := make(chan struct{})
	e.Register(Define("early", func(r *Run) {
		r.Step("prepare", func(context.Context, Payload) (Payload, error) {
			<-release
			return nil, nil
		})
		r.Step("wait", nil).WaitForEvent("go", 5*time.Second).After("prepare")
	}))
	id, _ := e.Dispatch("early", Payload{})
	time.Sleep(20 * time.Millisecond)
	if err := e.Signal(id, "go", Payload{"early": true}); err != nil {
		t.Fatalf("signal before the step waits: %v", err)
	}
	close(release)
	run := waitStatus(t, s, id, RunCompleted)
	if run.Payload["early"] != true {
		t.Fatalf("payload = %v", run.Payload)
	}
	if err := e.Signal(id, "go", nil); err == nil {
		t.Fatal("signal to a finished run should fail")
	}
	if err := e.Signal("no-such-run", "go", nil); err == nil {
		t.Fatal("signal to an unknown run should fail")
	}
}
