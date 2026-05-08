package workflow

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Engine
// ---------------------------------------------------------------------------

// Engine orchestrates workflow execution.
type Engine struct {
	mu          sync.RWMutex
	definitions map[string]*Definition
	store       Store
	signals     map[string]chan Payload // runID+event -> channel
	signalMu    sync.Mutex
	hooks       EngineHooks
}

// EngineHooks allows observability into workflow execution.
type EngineHooks struct {
	OnStepStart    func(runID, step string)
	OnStepComplete func(runID, step string, output Payload, duration time.Duration)
	OnStepFail     func(runID, step string, err error, attempt int)
	OnRunComplete  func(runID, workflow string, payload Payload)
	OnRunFail      func(runID, workflow string, err error)
}

// NewEngine creates a new workflow engine.
func NewEngine(store Store) *Engine {
	if store == nil {
		store = NewMemoryStore()
	}
	return &Engine{
		definitions: make(map[string]*Definition),
		store:       store,
		signals:     make(map[string]chan Payload),
	}
}

// SetHooks configures lifecycle hooks.
func (e *Engine) SetHooks(h EngineHooks) {
	e.hooks = h
}

// Register adds a workflow definition to the engine.
func (e *Engine) Register(def *Definition) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.definitions[def.Name] = def
}

// Dispatch starts a new workflow run asynchronously.
func (e *Engine) Dispatch(name string, payload Payload) (string, error) {
	e.mu.RLock()
	def, ok := e.definitions[name]
	e.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("workflow %q not registered", name)
	}

	run := &RunInstance{
		ID:        uuid.New().String(),
		Workflow:  name,
		Status:    RunPending,
		Payload:   payload,
		Steps:     make(map[string]*StepInstance),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	for _, step := range def.Steps {
		run.Steps[step.Name] = &StepInstance{
			Name:   step.Name,
			Status: StatusPending,
		}
	}

	if err := e.store.Save(context.Background(), run); err != nil {
		return "", fmt.Errorf("workflow store save: %w", err)
	}

	go e.execute(def, run)
	return run.ID, nil
}

// DispatchSync starts a workflow and blocks until completion.
func (e *Engine) DispatchSync(ctx context.Context, name string, payload Payload) (*RunInstance, error) {
	e.mu.RLock()
	def, ok := e.definitions[name]
	e.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("workflow %q not registered", name)
	}

	run := &RunInstance{
		ID:        uuid.New().String(),
		Workflow:  name,
		Status:    RunPending,
		Payload:   payload,
		Steps:     make(map[string]*StepInstance),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	for _, step := range def.Steps {
		run.Steps[step.Name] = &StepInstance{
			Name:   step.Name,
			Status: StatusPending,
		}
	}

	if err := e.store.Save(ctx, run); err != nil {
		return nil, fmt.Errorf("workflow store save: %w", err)
	}

	e.execute(def, run)
	return e.store.Load(ctx, run.ID)
}

// Signal sends an external event to a waiting workflow step.
func (e *Engine) Signal(runID, event string, data Payload) error {
	key := runID + ":" + event
	e.signalMu.Lock()
	ch, ok := e.signals[key]
	e.signalMu.Unlock()
	if !ok {
		return fmt.Errorf("no workflow waiting for event %q on run %s", event, runID)
	}
	select {
	case ch <- data:
		return nil
	default:
		return fmt.Errorf("signal channel full for %s", key)
	}
}

// Cancel cancels a running workflow.
func (e *Engine) Cancel(ctx context.Context, runID string) error {
	run, err := e.store.Load(ctx, runID)
	if err != nil {
		return err
	}
	run.Status = RunCancelled
	now := time.Now()
	run.CompletedAt = &now
	run.UpdatedAt = now
	for _, step := range run.Steps {
		if step.Status == StatusPending || step.Status == StatusWaiting || step.Status == StatusRunning {
			step.Status = StatusCancelled
		}
	}
	return e.store.Save(ctx, run)
}

// Status returns the current state of a workflow run.
func (e *Engine) Status(ctx context.Context, runID string) (*RunInstance, error) {
	return e.store.Load(ctx, runID)
}

// List returns recent runs for a workflow.
func (e *Engine) List(ctx context.Context, workflow string, limit int) ([]*RunInstance, error) {
	return e.store.List(ctx, workflow, limit)
}

// Workflows returns all registered workflow names.
func (e *Engine) Workflows() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	names := make([]string, 0, len(e.definitions))
	for name := range e.definitions {
		names = append(names, name)
	}
	return names
}

// ---------------------------------------------------------------------------
// Execution Engine
// ---------------------------------------------------------------------------

func (e *Engine) execute(def *Definition, run *RunInstance) {
	ctx := context.Background()
	run.Status = RunRunning
	run.UpdatedAt = time.Now()
	_ = e.store.Save(ctx, run)

	// Build dependency graph
	completed := make(map[string]bool)
	failed := false

	for !failed {
		// Find steps that are ready to run
		ready := e.findReadySteps(def, run, completed)
		if len(ready) == 0 {
			break
		}

		// Separate parallel and sequential steps
		var parallel []*StepDef
		var sequential []*StepDef
		for _, s := range ready {
			if s.IsParallel {
				parallel = append(parallel, s)
			} else {
				sequential = append(sequential, s)
			}
		}

		// Run parallel steps concurrently
		if len(parallel) > 0 {
			var wg sync.WaitGroup
			var mu sync.Mutex
			for _, step := range parallel {
				wg.Add(1)
				go func(s *StepDef) {
					defer wg.Done()
					if err := e.executeStep(ctx, def, run, s); err != nil {
						if s.OnFailure != "continue" {
							mu.Lock()
							failed = true
							mu.Unlock()
						}
					}
					mu.Lock()
					completed[s.Name] = true
					mu.Unlock()
				}(step)
			}
			wg.Wait()
			_ = e.store.Save(ctx, run)
		}

		// Run sequential steps one by one
		for _, step := range sequential {
			if failed {
				break
			}
			if err := e.executeStep(ctx, def, run, step); err != nil {
				if step.OnFailure != "continue" {
					failed = true
				}
			}
			completed[step.Name] = true
			_ = e.store.Save(ctx, run)
		}
	}

	// Final status
	now := time.Now()
	run.CompletedAt = &now
	run.UpdatedAt = now
	if failed {
		run.Status = RunFailed
		for _, si := range run.Steps {
			if si.Status == StatusPending {
				si.Status = StatusCancelled
			}
		}
		if e.hooks.OnRunFail != nil {
			e.hooks.OnRunFail(run.ID, run.Workflow, fmt.Errorf("workflow failed"))
		}
	} else {
		run.Status = RunCompleted
		if e.hooks.OnRunComplete != nil {
			e.hooks.OnRunComplete(run.ID, run.Workflow, run.Payload)
		}
	}
	_ = e.store.Save(ctx, run)
}

func (e *Engine) findReadySteps(def *Definition, run *RunInstance, completed map[string]bool) []*StepDef {
	var ready []*StepDef
	for _, step := range def.Steps {
		si := run.Steps[step.Name]
		if si.Status != StatusPending {
			continue
		}
		// Check condition
		if step.Condition != nil && !step.Condition(run.Payload) {
			si.Status = StatusSkipped
			completed[step.Name] = true
			continue
		}
		// Check dependencies
		allDepsCompleted := true
		for _, dep := range step.DependsOn {
			if !completed[dep] {
				allDepsCompleted = false
				break
			}
		}
		if allDepsCompleted {
			ready = append(ready, step)
		}
	}
	return ready
}

func (e *Engine) executeStep(ctx context.Context, def *Definition, run *RunInstance, step *StepDef) error {
	si := run.Steps[step.Name]
	si.Status = StatusRunning
	now := time.Now()
	si.StartedAt = &now
	run.UpdatedAt = now

	if e.hooks.OnStepStart != nil {
		e.hooks.OnStepStart(run.ID, step.Name)
	}

	// Handle wait-for-event steps
	if step.WaitEvent != "" {
		return e.executeWaitStep(ctx, run, step, si)
	}

	// Handle steps with no function (marker steps)
	if step.Fn == nil {
		si.Status = StatusCompleted
		finish := time.Now()
		si.FinishedAt = &finish
		si.Duration = finish.Sub(now)
		return nil
	}

	// Execute with retries
	var lastErr error
	maxAttempts := step.MaxRetries + 1
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		si.Attempts = attempt

		// Apply timeout
		execCtx := ctx
		var cancel context.CancelFunc
		if step.Timeout > 0 {
			execCtx, cancel = context.WithTimeout(ctx, step.Timeout)
		}

		output, err := step.Fn(execCtx, run.Payload)
		if cancel != nil {
			cancel()
		}

		if err == nil {
			// Success — merge output into payload
			si.Status = StatusCompleted
			si.Output = output
			finish := time.Now()
			si.FinishedAt = &finish
			si.Duration = finish.Sub(now)
			if output != nil {
				for k, v := range output {
					run.Payload[k] = v
				}
			}
			if e.hooks.OnStepComplete != nil {
				e.hooks.OnStepComplete(run.ID, step.Name, output, si.Duration)
			}
			return nil
		}

		lastErr = err
		if e.hooks.OnStepFail != nil {
			e.hooks.OnStepFail(run.ID, step.Name, err, attempt)
		}

		if attempt < maxAttempts {
			log.Printf("[workflow] step %s attempt %d/%d failed: %v, retrying in %v",
				step.Name, attempt, maxAttempts, err, step.RetryDelay)
			time.Sleep(step.RetryDelay)
		}
	}

	// All attempts exhausted
	si.Status = StatusFailed
	si.Error = lastErr.Error()
	finish := time.Now()
	si.FinishedAt = &finish
	si.Duration = finish.Sub(now)
	run.Error = fmt.Sprintf("step %s failed: %s", step.Name, lastErr.Error())
	return lastErr
}

func (e *Engine) executeWaitStep(ctx context.Context, run *RunInstance, step *StepDef, si *StepInstance) error {
	si.Status = StatusWaiting
	run.Status = RunPaused
	_ = e.store.Save(ctx, run)

	key := run.ID + ":" + step.WaitEvent
	ch := make(chan Payload, 1)
	e.signalMu.Lock()
	e.signals[key] = ch
	e.signalMu.Unlock()

	defer func() {
		e.signalMu.Lock()
		delete(e.signals, key)
		e.signalMu.Unlock()
	}()

	timeout := step.WaitTimeout
	if timeout <= 0 {
		timeout = 24 * time.Hour
	}

	select {
	case data := <-ch:
		// Event received — merge data and continue
		si.Status = StatusCompleted
		si.Output = data
		now := time.Now()
		si.FinishedAt = &now
		if data != nil {
			for k, v := range data {
				run.Payload[k] = v
			}
		}
		run.Status = RunRunning
		return nil
	case <-time.After(timeout):
		si.Status = StatusFailed
		si.Error = fmt.Sprintf("wait for event %q timed out after %v", step.WaitEvent, timeout)
		now := time.Now()
		si.FinishedAt = &now
		return fmt.Errorf("%s", si.Error)
	case <-ctx.Done():
		si.Status = StatusCancelled
		return ctx.Err()
	}
}
