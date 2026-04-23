package queue

import (
	"context"
	"sync"
)

var (
	afterBatchMu  sync.RWMutex
	afterBatchFns []func(context.Context, *Batch)
)

// AfterBatch registers a callback invoked after a batch finishes all jobs
// (after Then / Finally hooks on the batch itself). Useful for observability
// (e.g. Telescope). Multiple subscribers are allowed.
func AfterBatch(fn func(context.Context, *Batch)) {
	if fn == nil {
		return
	}
	afterBatchMu.Lock()
	defer afterBatchMu.Unlock()
	afterBatchFns = append(afterBatchFns, fn)
}

func runAfterBatchHooks(ctx context.Context, b *Batch) {
	afterBatchMu.RLock()
	fns := make([]func(context.Context, *Batch), len(afterBatchFns))
	copy(fns, afterBatchFns)
	afterBatchMu.RUnlock()
	for _, fn := range fns {
		fn(ctx, b)
	}
}
