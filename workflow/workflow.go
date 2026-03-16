/*
|--------------------------------------------------------------------------
| Nimbus Workflow Engine
|--------------------------------------------------------------------------
|
| Durable, multi-step workflow orchestration with retries, timeouts,
| parallel branches, and human approvals. Think Temporal/Inngest but
| built into the framework with zero external dependencies.
|
| Usage:
|
|   // Define a workflow
|   wf := workflow.Define("onboard-user", func(run *workflow.Run) {
|       run.Step("send-welcome", sendWelcomeEmail)
|       run.Step("create-org", createOrganisation).After("send-welcome")
|       run.Step("notify-team", notifySlack).Parallel()
|       run.Step("wait-approval", nil).WaitForEvent("approval.granted", 48*time.Hour)
|       run.Step("activate", activateAccount).After("wait-approval")
|   })
|
|   // Execute
|   engine.Dispatch("onboard-user", workflow.Payload{"user_id": "123"})
|
|   // Resume from external event
|   engine.Signal("onboard-user", runID, "approval.granted", data)
|
*/

package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Core Types
// ---------------------------------------------------------------------------

// Payload is the data bag passed through a workflow run.
type Payload map[string]any

// StepFunc performs a single unit of work within a workflow.
type StepFunc func(ctx context.Context, payload Payload) (Payload, error)

// StepStatus tracks the lifecycle of a step.
type StepStatus string

const (
	StatusPending   StepStatus = "pending"
	StatusRunning   StepStatus = "running"
	StatusCompleted StepStatus = "completed"
	StatusFailed    StepStatus = "failed"
	StatusSkipped   StepStatus = "skipped"
	StatusWaiting   StepStatus = "waiting" // waiting for external event
	StatusCancelled StepStatus = "cancelled"
)

// RunStatus tracks the lifecycle of a workflow run.
type RunStatus string

const (
	RunPending   RunStatus = "pending"
	RunRunning   RunStatus = "running"
	RunCompleted RunStatus = "completed"
	RunFailed    RunStatus = "failed"
	RunCancelled RunStatus = "cancelled"
	RunPaused    RunStatus = "paused" // waiting for human input
)

// ---------------------------------------------------------------------------
// Step Definition
// ---------------------------------------------------------------------------

// StepDef describes a single step in a workflow.
type StepDef struct {
	Name        string
	Fn          StepFunc
	DependsOn   []string      // steps that must complete first
	IsParallel  bool          // can run in parallel with siblings
	MaxRetries  int           // 0 = no retries
	RetryDelay  time.Duration // delay between retries
	Timeout     time.Duration // per-execution timeout
	WaitEvent   string        // external event to wait for
	WaitTimeout time.Duration // how long to wait for the event
	Condition   func(Payload) bool
	OnFailure   string // "continue" | "abort" (default: abort)
}

// StepBuilder provides a fluent API for defining steps.
type StepBuilder struct {
	def *StepDef
}

func (b *StepBuilder) After(deps ...string) *StepBuilder {
	b.def.DependsOn = append(b.def.DependsOn, deps...)
	return b
}

func (b *StepBuilder) Parallel() *StepBuilder {
	b.def.IsParallel = true
	return b
}

func (b *StepBuilder) Retry(max int, delay time.Duration) *StepBuilder {
	b.def.MaxRetries = max
	b.def.RetryDelay = delay
	return b
}

func (b *StepBuilder) WithTimeout(d time.Duration) *StepBuilder {
	b.def.Timeout = d
	return b
}

func (b *StepBuilder) WaitForEvent(event string, timeout time.Duration) *StepBuilder {
	b.def.WaitEvent = event
	b.def.WaitTimeout = timeout
	return b
}

func (b *StepBuilder) When(fn func(Payload) bool) *StepBuilder {
	b.def.Condition = fn
	return b
}

func (b *StepBuilder) ContinueOnFailure() *StepBuilder {
	b.def.OnFailure = "continue"
	return b
}

// ---------------------------------------------------------------------------
// Workflow Definition
// ---------------------------------------------------------------------------

// Definition describes a workflow template.
type Definition struct {
	Name  string
	Steps []*StepDef
}

// Run is the build context passed to the definition function.
type Run struct {
	steps []*StepDef
}

// Step registers a named step.
func (r *Run) Step(name string, fn StepFunc) *StepBuilder {
	def := &StepDef{
		Name:       name,
		Fn:         fn,
		MaxRetries: 0,
		RetryDelay: 1 * time.Second,
		OnFailure:  "abort",
	}
	r.steps = append(r.steps, def)
	return &StepBuilder{def: def}
}

// Define creates a workflow definition.
func Define(name string, builder func(r *Run)) *Definition {
	r := &Run{}
	builder(r)
	return &Definition{Name: name, Steps: r.steps}
}

// ---------------------------------------------------------------------------
// Step Instance (runtime state)
// ---------------------------------------------------------------------------

// StepInstance holds the state of a step within a running workflow.
type StepInstance struct {
	Name       string        `json:"name"`
	Status     StepStatus    `json:"status"`
	Output     Payload       `json:"output,omitempty"`
	Error      string        `json:"error,omitempty"`
	Attempts   int           `json:"attempts"`
	StartedAt  *time.Time    `json:"started_at,omitempty"`
	FinishedAt *time.Time    `json:"finished_at,omitempty"`
	Duration   time.Duration `json:"duration_ms,omitempty"`
}

// RunInstance holds the state of a running workflow.
type RunInstance struct {
	ID          string                   `json:"id"`
	Workflow    string                   `json:"workflow"`
	Status      RunStatus                `json:"status"`
	Payload     Payload                  `json:"payload"`
	Steps       map[string]*StepInstance `json:"steps"`
	CreatedAt   time.Time                `json:"created_at"`
	UpdatedAt   time.Time                `json:"updated_at"`
	CompletedAt *time.Time               `json:"completed_at,omitempty"`
	Error       string                   `json:"error,omitempty"`
}

// ---------------------------------------------------------------------------
// Store Interface
// ---------------------------------------------------------------------------

// Store persists workflow run state.
type Store interface {
	Save(ctx context.Context, run *RunInstance) error
	Load(ctx context.Context, id string) (*RunInstance, error)
	List(ctx context.Context, workflow string, limit int) ([]*RunInstance, error)
	Delete(ctx context.Context, id string) error
}

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

// ---------------------------------------------------------------------------
// Memory Store
// ---------------------------------------------------------------------------

// MemoryStore provides an in-memory Store implementation.
type MemoryStore struct {
	mu   sync.RWMutex
	runs map[string]*RunInstance
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{runs: make(map[string]*RunInstance)}
}

func (s *MemoryStore) Save(_ context.Context, run *RunInstance) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Deep copy to avoid mutation issues
	data, _ := json.Marshal(run)
	var copy RunInstance
	_ = json.Unmarshal(data, &copy)
	s.runs[run.ID] = &copy
	return nil
}

func (s *MemoryStore) Load(_ context.Context, id string) (*RunInstance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	run, ok := s.runs[id]
	if !ok {
		return nil, fmt.Errorf("workflow run %q not found", id)
	}
	return run, nil
}

func (s *MemoryStore) List(_ context.Context, workflow string, limit int) ([]*RunInstance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*RunInstance
	for _, run := range s.runs {
		if workflow == "" || run.Workflow == workflow {
			result = append(result, run)
		}
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result, nil
}

func (s *MemoryStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.runs, id)
	return nil
}
