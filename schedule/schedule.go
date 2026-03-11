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
	"log"
	"sync"
	"time"
)

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
}

// New creates a new Scheduler.
func New() *Scheduler {
	return &Scheduler{}
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
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[schedule] panic in task %q: %v", e.name, r)
		}
	}()
	if err := e.task(ctx); err != nil {
		log.Printf("[schedule] task %q error: %v", e.name, err)
	}
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
