/*
|--------------------------------------------------------------------------
| Queue Scheduler (Cron Jobs)
|--------------------------------------------------------------------------
|
| Schedules recurring jobs using cron expressions. Run alongside the worker.
|
|   scheduler := queue.NewScheduler(manager)
|   scheduler.Cron("0 0 * * *", &jobs.DailyReport{})  // midnight daily
|   scheduler.Every(5*time.Minute, &jobs.SyncInventory{})
|   scheduler.Start(ctx)
|
*/

package queue

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// Scheduler runs jobs on a schedule.
type Scheduler struct {
	manager *Manager
	cron    *cron.Cron
	mu      sync.Mutex
}

// NewScheduler creates a scheduler that dispatches to the given manager.
func NewScheduler(m *Manager) *Scheduler {
	return &Scheduler{
		manager: m,
		cron:    cron.New(), // 5-field: min hour day month dow
	}
}

// Cron adds a job with a cron expression.
// Examples: "0 0 * * *" = midnight daily, "*/5 * * * *" = every 5 min.
func (s *Scheduler) Cron(expr string, job Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.cron.AddFunc(expr, func() {
		_ = s.manager.Dispatch(job).Dispatch(context.Background())
	})
	return err
}

// Every adds a job that runs at a fixed interval.
func (s *Scheduler) Every(d time.Duration, job Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	expr := "@every " + durationToCron(d)
	_, err := s.cron.AddFunc(expr, func() {
		_ = s.manager.Dispatch(job).Dispatch(context.Background())
	})
	return err
}

func durationToCron(d time.Duration) string {
	if d >= time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	if d >= time.Minute {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d >= time.Second {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return "1m"
}

// Start runs the scheduler. Blocks until ctx is done.
func (s *Scheduler) Start(ctx context.Context) {
	s.cron.Start()
	<-ctx.Done()
	s.cron.Stop()
}

// Stop stops the scheduler.
func (s *Scheduler) Stop() {
	s.cron.Stop()
}
