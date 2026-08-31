package server

import (
	"fmt"
	"sync"
	"time"

	"github.com/CodeSyncr/nimbus/plugins/captcha"
)

// TaskItem represents an active or completed captcha solving job in memory.
type TaskItem struct {
	ID        string
	Payload   captcha.TaskPayload
	Status    string // "processing", "ready", "failed"
	Solution  captcha.Solution
	ErrorMsg  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TaskQueue is a thread-safe, in-memory queue for task management and retrieval.
type TaskQueue struct {
	tasks map[string]*TaskItem
	mu    sync.RWMutex
	ttl   time.Duration
}

// NewTaskQueue initializes a task queue.
func NewTaskQueue(ttl time.Duration) *TaskQueue {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	q := &TaskQueue{
		tasks: make(map[string]*TaskItem),
		ttl:   ttl,
	}

	// Periodic cleanup of expired tasks
	go q.startCleaner()

	return q
}

// Add enqueues a new task.
func (q *TaskQueue) Add(id string, payload captcha.TaskPayload) *TaskItem {
	q.mu.Lock()
	defer q.mu.Unlock()

	item := &TaskItem{
		ID:        id,
		Payload:   payload,
		Status:    "processing",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	q.tasks[id] = item
	return item
}

// Get retrieves a task by ID.
func (q *TaskQueue) Get(id string) (*TaskItem, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	item, exists := q.tasks[id]
	if !exists {
		return nil, false
	}
	return item, true
}

// Complete marks a task as successfully solved.
func (q *TaskQueue) Complete(id string, sol captcha.Solution) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	item, exists := q.tasks[id]
	if !exists {
		return fmt.Errorf("task %s not found", id)
	}

	item.Status = "ready"
	item.Solution = sol
	item.UpdatedAt = time.Now()
	return nil
}

// Fail marks a task as failed.
func (q *TaskQueue) Fail(id string, errMsg string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	item, exists := q.tasks[id]
	if !exists {
		return fmt.Errorf("task %s not found", id)
	}

	item.Status = "failed"
	item.ErrorMsg = errMsg
	item.UpdatedAt = time.Now()
	return nil
}

func (q *TaskQueue) startCleaner() {
	ticker := time.NewTicker(1 * time.Minute)
	for range ticker.C {
		q.mu.Lock()
		now := time.Now()
		for id, item := range q.tasks {
			if now.Sub(item.CreatedAt) > q.ttl {
				delete(q.tasks, id)
			}
		}
		q.mu.Unlock()
	}
}
