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
	owner       string        // this engine's identity for run leases
	leaseTTL    time.Duration // how long a lease outlives a dead instance

	pollInterval time.Duration // cross-instance cancel and signal checks
	execMu       sync.Mutex
	running      map[string]*execution // runs executing on this engine
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
		definitions:  make(map[string]*Definition),
		store:        store,
		signals:      make(map[string]chan Payload),
		owner:        uuid.New().String(),
		leaseTTL:     time.Minute,
		pollInterval: time.Second,
		running:      make(map[string]*execution),
	}
}

// SetLeaseTTL sets how long a run stays leased to an engine that stopped
// renewing it (crashed) before Resume on another instance takes it over.
func (e *Engine) SetLeaseTTL(d time.Duration) {
	if d > 0 {
		e.leaseTTL = d
	}
}

// Resume picks up unfinished runs whose engine is gone (after a restart or
// a crashed instance) and continues them from their last completed step.
// Steps that were mid-flight run again. It returns how many runs it took.
// Only stores that implement Leaser support it.
func (e *Engine) Resume(ctx context.Context) (int, error) {
	leaser, ok := e.store.(Leaser)
	if !ok {
		return 0, nil
	}
	runs, err := leaser.Unfinished(ctx, 100)
	if err != nil {
		return 0, err
	}
	taken := 0
	for _, run := range runs {
		e.mu.RLock()
		def, ok := e.definitions[run.Workflow]
		e.mu.RUnlock()
		if !ok {
			continue
		}
		claimed, err := leaser.Claim(ctx, run.ID, e.owner, e.leaseTTL)
		if err != nil || !claimed {
			continue
		}
		// Re-read under the lease: the previous owner may have saved since.
		if fresh, err := e.store.Load(ctx, run.ID); err == nil {
			run = fresh
		}
		if finished(run.Status) {
			_ = leaser.Release(ctx, run.ID, e.owner)
			continue
		}
		for _, step := range def.Steps {
			si := run.Steps[step.Name]
			if si == nil {
				run.Steps[step.Name] = &StepInstance{Name: step.Name, Status: StatusPending}
			} else if si.Status == StatusRunning || si.Status == StatusWaiting {
				si.Status = StatusPending
			}
		}
		if run.Payload == nil {
			run.Payload = Payload{}
		}
		taken++
		go e.runLeased(def, run)
	}
	return taken, nil
}

// StartRecovery calls Resume every interval until ctx is done, so runs of
// a crashed instance are continued by a live one.
func (e *Engine) StartRecovery(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if n, err := e.Resume(ctx); err != nil {
			log.Printf("[workflow] resume: %v", err)
		} else if n > 0 {
			log.Printf("[workflow] resumed %d run(s)", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// claim leases a new run to this engine when the store supports leases.
func (e *Engine) claim(ctx context.Context, run *RunInstance) {
	if leaser, ok := e.store.(Leaser); ok {
		if _, err := leaser.Claim(ctx, run.ID, e.owner, e.leaseTTL); err != nil {
			log.Printf("[workflow] lease run %s: %v", run.ID, err)
		}
	}
}

// runLeased executes a run while renewing its lease, then releases it.
func (e *Engine) runLeased(def *Definition, run *RunInstance) {
	leaser, ok := e.store.(Leaser)
	if !ok {
		e.execute(def, run)
		return
	}
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		t := time.NewTicker(e.leaseTTL / 3)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				if ok, err := leaser.Renew(context.Background(), run.ID, e.owner, e.leaseTTL); err == nil && !ok {
					log.Printf("[workflow] run %s lost its lease; another instance may resume it", run.ID)
				}
			}
		}
	}()
	e.execute(def, run)
	close(done)
	wg.Wait()
	_ = leaser.Release(context.Background(), run.ID, e.owner)
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
	e.claim(context.Background(), run)

	go e.runLeased(def, run)
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
	e.claim(ctx, run)

	e.runLeased(def, run)
	return e.store.Load(ctx, run.ID)
}

// Signal sends an external event to a workflow step waiting for it. If the
// run is waiting on this instance, the step wakes at once. Otherwise, with a
// store that implements SignalStore (all built-in stores do), the signal is
// kept until the step picks it up: on another instance, after a restart, or
// if the step has not started waiting yet.
func (e *Engine) Signal(runID, event string, data Payload) error {
	key := runID + ":" + event
	e.signalMu.Lock()
	ch, ok := e.signals[key]
	e.signalMu.Unlock()
	if ok {
		select {
		case ch <- data:
			return nil
		default:
		}
	}
	ss, ok := e.store.(SignalStore)
	if !ok {
		return fmt.Errorf("no workflow waiting for event %q on run %s", event, runID)
	}
	ctx := context.Background()
	run, err := e.store.Load(ctx, runID)
	if err != nil {
		return err
	}
	if finished(run.Status) {
		return fmt.Errorf("workflow run %s already %s", runID, run.Status)
	}
	return ss.PushSignal(ctx, runID, event, data)
}

// Cancel cancels a workflow run. A step running on this instance sees its
// context cancelled at once; an instance running it elsewhere notices
// within its poll interval (see SetPollInterval). Steps that have not run
// are marked cancelled.
func (e *Engine) Cancel(ctx context.Context, runID string) error {
	if x := e.execution(runID); x != nil {
		// Cancels the step's context; the run saves itself as cancelled
		// once the step returns. The store is marked now as well, so the
		// cancellation shows (and holds) even if the step ignores ctx.
		x.cancelRun()
	}
	run, err := e.store.Load(ctx, runID)
	if err != nil {
		return err
	}
	if finished(run.Status) {
		return nil
	}
	markCancelled(run)
	return e.store.Save(ctx, run)
}

func markCancelled(run *RunInstance) {
	now := time.Now()
	run.Status = RunCancelled
	run.CompletedAt = &now
	run.UpdatedAt = now
	for _, step := range run.Steps {
		if step.Status == StatusPending || step.Status == StatusWaiting || step.Status == StatusRunning {
			step.Status = StatusCancelled
		}
	}
}

// SetPollInterval sets how often a running run checks the store for a
// cancellation made on another instance, and a waiting step checks for
// stored signals (default 1s).
func (e *Engine) SetPollInterval(d time.Duration) {
	if d > 0 {
		e.pollInterval = d
	}
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

// execution is a run in progress on this engine. Every read or write of
// run happens under mu, so parallel steps and saves never race; step
// functions get their own copy of the payload.
type execution struct {
	e      *Engine
	mu     sync.Mutex
	run    *RunInstance
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	cancelled bool // set (under mu) once the run was cancelled
}

func (e *Engine) execution(runID string) *execution {
	e.execMu.Lock()
	defer e.execMu.Unlock()
	return e.running[runID]
}

func (x *execution) cancelRun() {
	x.mu.Lock()
	x.cancelled = true
	x.mu.Unlock()
	x.cancel()
}

// cancelledElsewhere reports whether the stored run was cancelled by
// another instance.
func (x *execution) cancelledElsewhere() bool {
	stored, err := x.e.store.Load(context.Background(), x.run.ID)
	return err == nil && stored.Status == RunCancelled
}

// save persists the run unless it was cancelled meanwhile, in which case
// it cancels the execution instead of overwriting the cancellation.
func (x *execution) save() {
	if x.cancelledElsewhere() {
		x.cancelRun()
		return
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	x.run.UpdatedAt = time.Now()
	_ = x.e.store.Save(context.Background(), x.run)
}

// watch cancels the execution when the stored run is cancelled elsewhere.
func (x *execution) watch() {
	t := time.NewTicker(x.e.pollInterval)
	defer t.Stop()
	for {
		select {
		case <-x.done:
			return
		case <-x.ctx.Done():
			return
		case <-t.C:
			if x.cancelledElsewhere() {
				x.cancelRun()
				return
			}
		}
	}
}

func (e *Engine) execute(def *Definition, run *RunInstance) {
	ctx, cancel := context.WithCancel(context.Background())
	x := &execution{e: e, run: run, ctx: ctx, cancel: cancel, done: make(chan struct{})}
	e.execMu.Lock()
	e.running[run.ID] = x
	e.execMu.Unlock()
	defer func() {
		e.execMu.Lock()
		delete(e.running, run.ID)
		e.execMu.Unlock()
		cancel()
		close(x.done)
	}()
	go x.watch()

	x.mu.Lock()
	run.Status = RunRunning
	if run.Payload == nil {
		run.Payload = Payload{}
	}
	// Build dependency graph. Steps finished before a restart count.
	completed := make(map[string]bool)
	failed := false
	for name, si := range run.Steps {
		switch si.Status {
		case StatusCompleted, StatusSkipped:
			completed[name] = true
		case StatusFailed:
			completed[name] = true
			if def.step(name) == nil || def.step(name).OnFailure != "continue" {
				failed = true
			}
		}
	}
	x.mu.Unlock()
	x.save()

	for !failed && ctx.Err() == nil {
		x.mu.Lock()
		ready := e.findReadySteps(def, run, completed)
		x.mu.Unlock()
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
					err := e.executeStep(x, s)
					mu.Lock()
					if err != nil && s.OnFailure != "continue" {
						failed = true
					}
					completed[s.Name] = true
					mu.Unlock()
				}(step)
			}
			wg.Wait()
			x.save()
		}

		// Run sequential steps one by one
		for _, step := range sequential {
			if failed || ctx.Err() != nil {
				break
			}
			if err := e.executeStep(x, step); err != nil {
				if step.OnFailure != "continue" {
					failed = true
				}
			}
			completed[step.Name] = true
			x.save()
		}
	}

	// Final status
	if !x.cancelled && x.cancelledElsewhere() {
		x.cancelRun()
	}
	x.mu.Lock()
	now := time.Now()
	run.CompletedAt = &now
	run.UpdatedAt = now
	var onFail, onComplete bool
	switch {
	case x.cancelled:
		markCancelled(run)
	case failed:
		run.Status = RunFailed
		for _, si := range run.Steps {
			if si.Status == StatusPending {
				si.Status = StatusCancelled
			}
		}
		onFail = true
	default:
		run.Status = RunCompleted
		onComplete = true
	}
	payload := clonePayload(run.Payload)
	_ = e.store.Save(context.Background(), run)
	x.mu.Unlock()

	if onFail && e.hooks.OnRunFail != nil {
		e.hooks.OnRunFail(run.ID, run.Workflow, fmt.Errorf("workflow failed"))
	}
	if onComplete && e.hooks.OnRunComplete != nil {
		e.hooks.OnRunComplete(run.ID, run.Workflow, payload)
	}
}

// findReadySteps must be called with the execution lock held.
func (e *Engine) findReadySteps(def *Definition, run *RunInstance, completed map[string]bool) []*StepDef {
	var ready []*StepDef
	for _, step := range def.Steps {
		si := run.Steps[step.Name]
		if si.Status != StatusPending {
			continue
		}
		// Check condition
		if step.Condition != nil && !step.Condition(clonePayload(run.Payload)) {
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

func clonePayload(p Payload) Payload {
	out := make(Payload, len(p))
	for k, v := range p {
		out[k] = v
	}
	return out
}

func (e *Engine) executeStep(x *execution, step *StepDef) error {
	ctx, run := x.ctx, x.run

	x.mu.Lock()
	si := run.Steps[step.Name]
	si.Status = StatusRunning
	start := time.Now()
	si.StartedAt = &start
	run.UpdatedAt = start
	x.mu.Unlock()

	if e.hooks.OnStepStart != nil {
		e.hooks.OnStepStart(run.ID, step.Name)
	}

	// Handle wait-for-event steps
	if step.WaitEvent != "" {
		return e.executeWaitStep(x, step, si)
	}

	// Handle steps with no function (marker steps)
	if step.Fn == nil {
		x.mu.Lock()
		si.Status = StatusCompleted
		finish := time.Now()
		si.FinishedAt = &finish
		si.Duration = finish.Sub(start)
		x.mu.Unlock()
		return nil
	}

	// Execute with retries
	var lastErr error
	maxAttempts := step.MaxRetries + 1
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		x.mu.Lock()
		si.Attempts = attempt
		input := clonePayload(run.Payload)
		x.mu.Unlock()

		// Apply timeout
		execCtx := ctx
		var cancel context.CancelFunc
		if step.Timeout > 0 {
			execCtx, cancel = context.WithTimeout(ctx, step.Timeout)
		}

		output, err := step.Fn(execCtx, input)
		if cancel != nil {
			cancel()
		}

		if err == nil {
			// Success — merge output into payload
			x.mu.Lock()
			si.Status = StatusCompleted
			si.Output = output
			finish := time.Now()
			si.FinishedAt = &finish
			si.Duration = finish.Sub(start)
			for k, v := range output {
				run.Payload[k] = v
			}
			duration := si.Duration
			x.mu.Unlock()
			if e.hooks.OnStepComplete != nil {
				e.hooks.OnStepComplete(run.ID, step.Name, output, duration)
			}
			return nil
		}

		lastErr = err
		if e.hooks.OnStepFail != nil {
			e.hooks.OnStepFail(run.ID, step.Name, err, attempt)
		}
		if ctx.Err() != nil {
			break // cancelled: no point retrying
		}
		if attempt < maxAttempts {
			log.Printf("[workflow] step %s attempt %d/%d failed: %v, retrying in %v",
				step.Name, attempt, maxAttempts, err, step.RetryDelay)
			select {
			case <-time.After(step.RetryDelay):
			case <-ctx.Done():
			}
		}
	}

	// All attempts exhausted (or cancelled)
	x.mu.Lock()
	defer x.mu.Unlock()
	finish := time.Now()
	si.FinishedAt = &finish
	si.Duration = finish.Sub(start)
	if ctx.Err() != nil {
		si.Status = StatusCancelled
		return ctx.Err()
	}
	si.Status = StatusFailed
	si.Error = lastErr.Error()
	run.Error = fmt.Sprintf("step %s failed: %s", step.Name, lastErr.Error())
	return lastErr
}

func (e *Engine) executeWaitStep(x *execution, step *StepDef, si *StepInstance) error {
	ctx, run := x.ctx, x.run
	x.mu.Lock()
	si.Status = StatusWaiting
	run.Status = RunPaused
	x.mu.Unlock()
	x.save()

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
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	// Signals sent to another instance (or before this step started
	// waiting) are kept by the store; poll for them.
	ss, _ := e.store.(SignalStore)
	pop := func() (Payload, bool) {
		if ss == nil {
			return nil, false
		}
		data, ok, err := ss.PopSignal(context.Background(), run.ID, step.WaitEvent)
		if err != nil {
			log.Printf("[workflow] read signal %q for run %s: %v", step.WaitEvent, run.ID, err)
		}
		return data, ok
	}
	poll := time.NewTicker(e.pollInterval)
	defer poll.Stop()

	received := func(data Payload) error {
		x.mu.Lock()
		defer x.mu.Unlock()
		si.Status = StatusCompleted
		si.Output = data
		now := time.Now()
		si.FinishedAt = &now
		for k, v := range data {
			run.Payload[k] = v
		}
		run.Status = RunRunning
		return nil
	}

	if data, ok := pop(); ok {
		return received(data)
	}
	for {
		select {
		case data := <-ch:
			return received(data)
		case <-poll.C:
			if data, ok := pop(); ok {
				return received(data)
			}
		case <-deadline.C:
			x.mu.Lock()
			defer x.mu.Unlock()
			si.Status = StatusFailed
			si.Error = fmt.Sprintf("wait for event %q timed out after %v", step.WaitEvent, timeout)
			now := time.Now()
			si.FinishedAt = &now
			run.Status = RunRunning
			return fmt.Errorf("%s", si.Error)
		case <-ctx.Done():
			x.mu.Lock()
			defer x.mu.Unlock()
			si.Status = StatusCancelled
			return ctx.Err()
		}
	}
}
