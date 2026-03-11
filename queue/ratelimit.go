/*
|--------------------------------------------------------------------------
| Queue Rate Limiting
|--------------------------------------------------------------------------
|
| Wraps an adapter to limit job processing rate per queue.
| Use for APIs with rate limits (e.g. email providers, external APIs).
|
*/

package queue

import (
	"context"

	"golang.org/x/time/rate"
)

// RateLimitConfig configures rate limiting per queue.
type RateLimitConfig struct {
	// Limit is the max number of jobs per second (e.g. 10 = 10 jobs/sec).
	Limit float64
	// Burst allows short bursts above the limit.
	Burst int
}

// RateLimitAdapter wraps an adapter with rate limiting.
type RateLimitAdapter struct {
	inner  Adapter
	limit  *rate.Limiter
	perSec float64
}

// NewRateLimitAdapter wraps an adapter with a rate limiter.
// limitPerSec: max jobs per second (e.g. 10)
// burst: max burst size (e.g. 20)
func NewRateLimitAdapter(inner Adapter, limitPerSec float64, burst int) *RateLimitAdapter {
	if burst <= 0 {
		burst = 1
	}
	return &RateLimitAdapter{
		inner:  inner,
		limit:  rate.NewLimiter(rate.Limit(limitPerSec), burst),
		perSec: limitPerSec,
	}
}

// Push delegates to inner (no rate limit on push).
func (r *RateLimitAdapter) Push(ctx context.Context, payload *JobPayload) error {
	return r.inner.Push(ctx, payload)
}

// Pop waits for rate limit before returning a job.
func (r *RateLimitAdapter) Pop(ctx context.Context, queue string) (*JobPayload, error) {
	if err := r.limit.Wait(ctx); err != nil {
		return nil, err
	}
	return r.inner.Pop(ctx, queue)
}

// Len delegates to inner.
func (r *RateLimitAdapter) Len(ctx context.Context, queue string) (int, error) {
	return r.inner.Len(ctx, queue)
}

// Complete delegates if inner supports it.
func (r *RateLimitAdapter) Complete(ctx context.Context, payload *JobPayload) error {
	if ca, ok := r.inner.(CompletableAdapter); ok {
		return ca.Complete(ctx, payload)
	}
	return nil
}

var _ Adapter = (*RateLimitAdapter)(nil)
