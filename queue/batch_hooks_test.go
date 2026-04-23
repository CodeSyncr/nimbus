package queue

import (
	"context"
	"sync/atomic"
	"testing"
)

type nopJob struct{}

func (nopJob) Handle(context.Context) error { return nil }

func TestAfterBatchRunsAfterDispatch(t *testing.T) {
	t.Cleanup(func() {
		afterBatchMu.Lock()
		afterBatchFns = nil
		afterBatchMu.Unlock()
	})

	var runs int32
	AfterBatch(func(context.Context, *Batch) {
		atomic.AddInt32(&runs, 1)
	})

	b := NewBatch(nopJob{}, nopJob{})
	if err := b.Dispatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&runs) != 1 {
		t.Fatalf("expected 1 after-batch hook run, got %d", runs)
	}
}
