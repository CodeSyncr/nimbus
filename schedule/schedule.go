// Package schedule provides a lightweight cron-style task scheduler for Nimbus.
//
// Usage:
//
//	s := schedule.New()
//	s.Every(5*time.Minute, "cleanup-temp", func(ctx context.Context) error {
//	    return os.RemoveAll("/tmp/nimbus-cache")
//	})
//	s.Daily("03:00", "send-reports", func(ctx context.Context) error {
//	    return reportService.SendDailyDigest(ctx)
//	})
//	s.Start(ctx)        // non-blocking, runs in background
//	defer s.Stop()
package schedule

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

var (
	taskRunMu   sync.Mutex
	taskRunHooks []func(name, expression, status string, duration time.Duration, output string)
)

// OnTaskRun registers a hook after each scheduled task attempt (Telescope, metrics).
func OnTaskRun(fn func(name, expression, status string, duration time.Duration, output string)) {
	if fn == nil {
		return
	}
	taskRunMu.Lock()
	defer taskRunMu.Unlock()
	taskRunHooks = append(taskRunHooks, fn)
}

func notifyTaskRun(name, expression, status string, duration time.Duration, output string) {
	taskRunMu.Lock()
	hooks := append([]func(string, string, string, time.Duration, string){}, taskRunHooks...)
	taskRunMu.Unlock()
	for _, h := range hooks {
		h(name, expression, status, duration, output)
	}
}

// Task is a scheduled function.
type Task func(ctx context.Context) error

// entry holds a single scheduled task.
type entry struct {
	name     string
	task     Task
	interval time.Duration
	at       string // "HH:MM" for daily tasks, empty for interval-based
}

// Scheduler manages periodic tasks.
type Scheduler struct {
	mu      sync.Mutex
	entries []entry
	cancel  context.CancelFunc
	running bool
	locker  Locker
}

// Locker coordinates schedule execution across multiple app instances.
// When configured, each task tick tries to acquire a distributed lock and
// runs only on the lock holder.
type Locker interface {
	TryLock(ctx context.Context, key string, ttl time.Duration) (unlock func(), acquired bool, err error)
}

// New creates a new Scheduler.
func New() *Scheduler {
	return &Scheduler{}
}

// WithLocker enables distributed lock coordination for scheduled task runs.
// Use this in multi-instance deployments to avoid duplicate task execution.
func (s *Scheduler) WithLocker(locker Locker) *Scheduler {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.locker = locker
	return s
}

// Every registers a task that runs at a fixed interval.
//
//	s.Every(30*time.Second, "ping-check", pingHandler)
func (s *Scheduler) Every(interval time.Duration, name string, fn Task) *Scheduler {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, entry{name: name, task: fn, interval: interval})
	return s
}

// EveryMinute registers a task that runs once per minute.
func (s *Scheduler) EveryMinute(name string, fn Task) *Scheduler {
	return s.Every(time.Minute, name, fn)
}

// EveryFiveMinutes registers a task that runs every 5 minutes.
func (s *Scheduler) EveryFiveMinutes(name string, fn Task) *Scheduler {
	return s.Every(5*time.Minute, name, fn)
}

// Hourly registers a task that runs once per hour.
func (s *Scheduler) Hourly(name string, fn Task) *Scheduler {
	return s.Every(time.Hour, name, fn)
}

// Daily registers a task that runs once per day at the given time (HH:MM in local timezone).
//
//	s.Daily("03:00", "nightly-cleanup", cleanupHandler)
func (s *Scheduler) Daily(at string, name string, fn Task) *Scheduler {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, entry{name: name, task: fn, at: at, interval: 24 * time.Hour})
	return s
}

// Start begins running all scheduled tasks in the background.
// Call Stop or cancel the parent context to halt.
func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	childCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	entries := make([]entry, len(s.entries))
	copy(entries, s.entries)
	s.mu.Unlock()

	for _, e := range entries {
		go s.runEntry(childCtx, e)
	}
	log.Printf("[schedule] started %d task(s)", len(entries))
}

// Stop halts all scheduled tasks.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
	s.running = false
}

// Count returns the number of registered tasks.
func (s *Scheduler) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

func (s *Scheduler) runEntry(ctx context.Context, e entry) {
	// For daily tasks, compute initial delay until the target time.
	var initialDelay time.Duration
	if e.at != "" {
		initialDelay = untilNext(e.at)
	}

	if initialDelay > 0 {
		select {
		case <-ctx.Done():
			return
		case <-time.After(initialDelay):
		}
		s.execute(ctx, e)
	}

	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.execute(ctx, e)
		}
	}
}

func (s *Scheduler) execute(ctx context.Context, e entry) {
	if s.locker != nil {
		lockTTL := e.interval + 5*time.Second
		if lockTTL < 10*time.Second {
			lockTTL = 10 * time.Second
		}
		// Use a time bucket in the lock key so only one instance can execute
		// this task for the current interval window.
		bucketSize := int64(e.interval.Seconds())
		if bucketSize <= 0 {
			bucketSize = 60
		}
		bucket := time.Now().Unix() / bucketSize
		lockKey := fmt.Sprintf("nimbus:schedule:%s:%d", e.name, bucket)
		_, acquired, err := s.locker.TryLock(ctx, lockKey, lockTTL)
		if err != nil {
			log.Printf("[schedule] lock error for task %q: %v", e.name, err)
			return
		}
		if !acquired {
			return
		}
	}

	start := time.Now()
	var out string
	var runErr error
	var panicked any
	defer func() {
		dur := time.Since(start)
		status := "ok"
		if panicked != nil {
			status = "panic"
			out = fmt.Sprint(panicked)
		} else if runErr != nil {
			status = "failed"
			out = runErr.Error()
		}
		notifyTaskRun(e.name, scheduleExpr(e), status, dur, out)
	}()
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = r
				log.Printf("[schedule] panic in task %q: %v", e.name, r)
			}
		}()
		runErr = e.task(ctx)
		if runErr != nil {
			log.Printf("[schedule] task %q error: %v", e.name, runErr)
		}
	}()
}

func scheduleExpr(e entry) string {
	if e.at != "" {
		return "daily@" + e.at
	}
	return fmt.Sprintf("every %s", e.interval)
}

// untilNext computes the duration until the next occurrence of "HH:MM" today
// or tomorrow if the time has already passed.
func untilNext(hhmm string) time.Duration {
	now := time.Now()
	t, err := time.Parse("15:04", hhmm)
	if err != nil {
		return 0
	}
	target := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, now.Location())
	if target.Before(now) {
		target = target.Add(24 * time.Hour)
	}
	return target.Sub(now)
}
