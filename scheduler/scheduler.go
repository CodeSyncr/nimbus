package scheduler

import (
	"context"
	"sync"
	"time"
)

// Task runs on a schedule (plan: schedule.EveryMinute(), schedule.Daily()).
type Task struct {
	Interval time.Duration
	Run      func(ctx context.Context) error
}

// Scheduler runs tasks at intervals.
type Scheduler struct {
	mu    sync.Mutex
	tasks []Task
	stop  chan struct{}
}

// New returns a new scheduler.
func New() *Scheduler {
	return &Scheduler{tasks: nil, stop: nil}
}

// EveryMinute adds a task that runs every minute.
func (s *Scheduler) EveryMinute(fn func(context.Context) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks = append(s.tasks, Task{Interval: time.Minute, Run: fn})
}

// EveryHour adds a task that runs every hour.
func (s *Scheduler) EveryHour(fn func(context.Context) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks = append(s.tasks, Task{Interval: time.Hour, Run: fn})
}

// Daily adds a task that runs every 24 hours.
func (s *Scheduler) Daily(fn func(context.Context) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks = append(s.tasks, Task{Interval: 24 * time.Hour, Run: fn})
}

// Weekly adds a task that runs every 7 days.
func (s *Scheduler) Weekly(fn func(context.Context) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks = append(s.tasks, Task{Interval: 7 * 24 * time.Hour, Run: fn})
}

// Run starts the scheduler (blocks until Stop). Call in a goroutine.
func (s *Scheduler) Run(ctx context.Context) {
	s.mu.Lock()
	tasks := make([]Task, len(s.tasks))
	copy(tasks, s.tasks)
	s.stop = make(chan struct{})
	s.mu.Unlock()
	for _, t := range tasks {
		go s.runTask(ctx, t)
	}
	<-s.stop
}

func (s *Scheduler) runTask(ctx context.Context, t Task) {
	ticker := time.NewTicker(t.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = t.Run(ctx)
		}
	}
}

// Stop stops the scheduler.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stop != nil {
		close(s.stop)
		s.stop = nil
	}
}
