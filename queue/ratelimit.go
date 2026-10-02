/*
|--------------------------------------------------------------------------
| Queue Rate Limiting
|--------------------------------------------------------------------------
|
| Wraps an adapter to limit job processing rate per queue.
| Use for APIs with rate limits (e.g. email providers, external APIs).
|
| The limit is per queue. With the Redis driver, Boot uses a limiter kept
| in Redis so the limit holds across every worker and instance; the
| in-process limiter only limits the workers of one process.
|
*/

package queue

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/CodeSyncr/nimbus/redis"
	"golang.org/x/time/rate"
)

// RateLimitConfig configures rate limiting per queue.
type RateLimitConfig struct {
	// Limit is the max number of jobs per second (e.g. 10 = 10 jobs/sec).
	Limit float64
	// Burst allows short bursts above the limit.
	Burst int
}

// JobLimiter blocks until a job may be taken from queue.
type JobLimiter interface {
	Wait(ctx context.Context, queue string) error
}

// RateLimitAdapter wraps an adapter with rate limiting.
type RateLimitAdapter struct {
	inner   Adapter
	limiter JobLimiter
	perSec  float64
}

// NewRateLimitAdapter wraps an adapter with an in-process rate limiter.
// limitPerSec: max jobs per second per queue (e.g. 10)
// burst: max burst size (e.g. 20)
func NewRateLimitAdapter(inner Adapter, limitPerSec float64, burst int) *RateLimitAdapter {
	return NewRateLimitAdapterWith(inner, NewLocalJobLimiter(limitPerSec, burst), limitPerSec)
}

// NewRateLimitAdapterWith wraps an adapter with the given limiter, e.g. a
// RedisJobLimiter shared by every worker.
func NewRateLimitAdapterWith(inner Adapter, limiter JobLimiter, limitPerSec float64) *RateLimitAdapter {
	return &RateLimitAdapter{inner: inner, limiter: limiter, perSec: limitPerSec}
}

// Push delegates to inner (no rate limit on push).
func (r *RateLimitAdapter) Push(ctx context.Context, payload *JobPayload) error {
	return r.inner.Push(ctx, payload)
}

// Pop waits for rate limit before returning a job.
func (r *RateLimitAdapter) Pop(ctx context.Context, queue string) (*JobPayload, error) {
	if err := r.limiter.Wait(ctx, queue); err != nil {
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

// LeaseDuration delegates if inner leases jobs; zero disables heartbeats.
func (r *RateLimitAdapter) LeaseDuration() time.Duration {
	if le, ok := r.inner.(LeaseExtender); ok {
		return le.LeaseDuration()
	}
	return 0
}

// ExtendLease delegates if inner leases jobs.
func (r *RateLimitAdapter) ExtendLease(ctx context.Context, payload *JobPayload) error {
	if le, ok := r.inner.(LeaseExtender); ok {
		return le.ExtendLease(ctx, payload)
	}
	return nil
}

// Inner returns the wrapped adapter.
func (r *RateLimitAdapter) Inner() Adapter { return r.inner }

// LocalJobLimiter rate-limits each queue within this process.
type LocalJobLimiter struct {
	limit rate.Limit
	burst int
	mu    sync.Mutex
	by    map[string]*rate.Limiter
}

// NewLocalJobLimiter returns an in-process JobLimiter.
func NewLocalJobLimiter(limitPerSec float64, burst int) *LocalJobLimiter {
	if burst <= 0 {
		burst = 1
	}
	return &LocalJobLimiter{limit: rate.Limit(limitPerSec), burst: burst, by: map[string]*rate.Limiter{}}
}

func (l *LocalJobLimiter) Wait(ctx context.Context, queue string) error {
	l.mu.Lock()
	lim, ok := l.by[queue]
	if !ok {
		lim = rate.NewLimiter(l.limit, l.burst)
		l.by[queue] = lim
	}
	l.mu.Unlock()
	return lim.Wait(ctx)
}

// gcraScript is a generic cell rate algorithm limiter. It stores one
// timestamp per key: the theoretical arrival time of the next job.
//
// KEYS: limiter key
// ARGV: now (ms), emission interval (ms per job), burst
// Returns 0 when allowed, otherwise milliseconds to wait.
var gcraScript = redis.NewScript(`
local now = tonumber(ARGV[1])
local interval = tonumber(ARGV[2])
local burst = tonumber(ARGV[3])
local tat = tonumber(redis.call('GET', KEYS[1]) or now)
if tat < now then
  tat = now
end
local new_tat = tat + interval
local allow_at = new_tat - burst * interval
if now < allow_at then
  return math.ceil(allow_at - now)
end
redis.call('SET', KEYS[1], tostring(new_tat), 'PX', math.ceil(new_tat - now) + 1000)
return 0
`)

// RedisJobLimiter rate-limits each queue across every worker that shares
// the Redis server.
type RedisJobLimiter struct {
	client   *redis.Client
	interval float64 // ms per job
	burst    int
}

// NewRedisJobLimiter returns a JobLimiter kept in Redis.
func NewRedisJobLimiter(client *redis.Client, limitPerSec float64, burst int) *RedisJobLimiter {
	if burst <= 0 {
		burst = 1
	}
	return &RedisJobLimiter{client: client, interval: 1000 / limitPerSec, burst: burst}
}

func (l *RedisJobLimiter) Wait(ctx context.Context, queue string) error {
	key := "nimbus:queue:ratelimit:" + queue
	for {
		wait, err := gcraScript.Run(ctx, l.client, []string{key},
			time.Now().UnixMilli(),
			strconv.FormatFloat(l.interval, 'f', 3, 64),
			l.burst,
		).Int64()
		if err != nil {
			return err
		}
		if wait <= 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(wait) * time.Millisecond):
		}
	}
}

var _ Adapter = (*RateLimitAdapter)(nil)
var _ CompletableAdapter = (*RateLimitAdapter)(nil)
var _ LeaseExtender = (*RateLimitAdapter)(nil)
