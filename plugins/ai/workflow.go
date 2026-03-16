/*
|--------------------------------------------------------------------------
| AI SDK — Workflow Engine
|--------------------------------------------------------------------------
|
| Multi-step AI pipelines with sequential, parallel, and conditional
| execution. Each step receives the accumulated context from prior
| steps and contributes its result.
|
| Usage:
|
|   wf := ai.NewWorkflow("content-pipeline").
|       Step("outline",   generateOutline).
|       Step("draft",     writeDraft).
|       Step("review",    reviewDraft).
|       Step("final",     finalize)
|
|   result, err := wf.Run(ctx, ai.WorkflowInput{
|       "topic": "Go concurrency patterns",
|   })
|
|   // Parallel steps:
|   wf.Parallel("media",
|       ai.StepFunc("images", generateImages),
|       ai.StepFunc("audio",  generateAudio),
|   )
|
|   // Conditional branching:
|   wf.Branch("route", func(ctx ai.WorkflowContext) string {
|       if ctx.Get("needs_review") == "true" { return "review" }
|       return "publish"
|   })
|
*/

package ai

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// WorkflowInput is the initial data map passed to a workflow.
type WorkflowInput map[string]any

// WorkflowContext carries accumulated state through the pipeline.
type WorkflowContext struct {
	context.Context
	data map[string]any
	mu   sync.RWMutex
}

// Set stores a value in the workflow context.
func (wc *WorkflowContext) Set(key string, value any) {
	wc.mu.Lock()
	defer wc.mu.Unlock()
	wc.data[key] = value
}

// Get retrieves a value from the workflow context.
func (wc *WorkflowContext) Get(key string) any {
	wc.mu.RLock()
	defer wc.mu.RUnlock()
	return wc.data[key]
}

// GetString retrieves a string value.
func (wc *WorkflowContext) GetString(key string) string {
	v := wc.Get(key)
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// All returns a copy of all data.
func (wc *WorkflowContext) All() map[string]any {
	wc.mu.RLock()
	defer wc.mu.RUnlock()
	cp := make(map[string]any, len(wc.data))
	for k, v := range wc.data {
		cp[k] = v
	}
	return cp
}

// newWorkflowContext creates a WorkflowContext with initial data.
func newWorkflowContext(ctx context.Context, input WorkflowInput) *WorkflowContext {
	data := make(map[string]any)
	for k, v := range input {
		data[k] = v
	}
	return &WorkflowContext{Context: ctx, data: data}
}

// ---------------------------------------------------------------------------
// Step interface
// ---------------------------------------------------------------------------

// StepHandler is a function that executes a workflow step. It receives
// the accumulated context and should call wc.Set() to contribute results.
type StepHandler func(wc *WorkflowContext) error

// WorkflowResult is the final output of a workflow run.
type WorkflowResult struct {
	Data     map[string]any `json:"data"`
	Duration time.Duration  `json:"duration"`
	Steps    []StepResult   `json:"steps"`
}

// StepResult records the outcome of a single step.
type StepResult struct {
	Name     string        `json:"name"`
	Duration time.Duration `json:"duration"`
	Error    error         `json:"error,omitempty"`
}

// ---------------------------------------------------------------------------
// Workflow
// ---------------------------------------------------------------------------

// Workflow is a named multi-step AI pipeline.
type Workflow struct {
	name  string
	steps []workflowStep
	hooks []WorkflowHook
}

// WorkflowHook is called before/after steps.
type WorkflowHook func(step string, event string, wc *WorkflowContext)

type workflowStep struct {
	name    string
	kind    stepKind
	handler StepHandler

	// For parallel steps.
	parallel []workflowStep

	// For branch steps.
	branchFn func(wc *WorkflowContext) string
	branches map[string]StepHandler
}

type stepKind int

const (
	stepSequential stepKind = iota
	stepParallel
	stepBranch
)

// NewWorkflow creates a named workflow.
func NewWorkflow(name string) *Workflow {
	return &Workflow{name: name}
}

// Step adds a sequential step to the workflow.
func (w *Workflow) Step(name string, handler StepHandler) *Workflow {
	w.steps = append(w.steps, workflowStep{
		name:    name,
		kind:    stepSequential,
		handler: handler,
	})
	return w
}

// Parallel adds a set of steps that execute concurrently.
func (w *Workflow) Parallel(name string, steps ...NamedStep) *Workflow {
	var parallel []workflowStep
	for _, s := range steps {
		parallel = append(parallel, workflowStep{
			name:    s.Name,
			kind:    stepSequential,
			handler: s.Handler,
		})
	}
	w.steps = append(w.steps, workflowStep{
		name:     name,
		kind:     stepParallel,
		parallel: parallel,
	})
	return w
}

// Branch adds a conditional branching step.
func (w *Workflow) Branch(name string, selector func(wc *WorkflowContext) string, branches map[string]StepHandler) *Workflow {
	w.steps = append(w.steps, workflowStep{
		name:     name,
		kind:     stepBranch,
		branchFn: selector,
		branches: branches,
	})
	return w
}

// OnStep registers a hook for step lifecycle events.
func (w *Workflow) OnStep(h WorkflowHook) *Workflow {
	w.hooks = append(w.hooks, h)
	return w
}

// NamedStep associates a name with a step handler.
type NamedStep struct {
	Name    string
	Handler StepHandler
}

// StepFunc creates a NamedStep from a name and handler.
func StepFunc(name string, handler StepHandler) NamedStep {
	return NamedStep{Name: name, Handler: handler}
}

// ---------------------------------------------------------------------------
// Run
// ---------------------------------------------------------------------------

// Run executes the workflow with the given input and returns the result.
func (w *Workflow) Run(ctx context.Context, input WorkflowInput) (*WorkflowResult, error) {
	wc := newWorkflowContext(ctx, input)
	start := time.Now()
	result := &WorkflowResult{}

	for _, step := range w.steps {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("ai: workflow %q cancelled: %w", w.name, err)
		}

		switch step.kind {
		case stepSequential:
			sr, err := w.runStep(wc, step)
			result.Steps = append(result.Steps, sr)
			if err != nil {
				result.Data = wc.All()
				result.Duration = time.Since(start)
				return result, fmt.Errorf("ai: workflow %q step %q: %w", w.name, step.name, err)
			}

		case stepParallel:
			results, err := w.runParallel(wc, step)
			result.Steps = append(result.Steps, results...)
			if err != nil {
				result.Data = wc.All()
				result.Duration = time.Since(start)
				return result, fmt.Errorf("ai: workflow %q parallel %q: %w", w.name, step.name, err)
			}

		case stepBranch:
			branch := step.branchFn(wc)
			handler, ok := step.branches[branch]
			if !ok {
				return nil, fmt.Errorf("ai: workflow %q branch %q: unknown branch %q", w.name, step.name, branch)
			}
			sr, err := w.runStep(wc, workflowStep{name: step.name + "/" + branch, handler: handler})
			result.Steps = append(result.Steps, sr)
			if err != nil {
				result.Data = wc.All()
				result.Duration = time.Since(start)
				return result, err
			}
		}
	}

	result.Data = wc.All()
	result.Duration = time.Since(start)
	return result, nil
}

func (w *Workflow) runStep(wc *WorkflowContext, step workflowStep) (StepResult, error) {
	w.fireHook(step.name, "before", wc)
	start := time.Now()
	err := step.handler(wc)
	sr := StepResult{
		Name:     step.name,
		Duration: time.Since(start),
		Error:    err,
	}
	w.fireHook(step.name, "after", wc)
	return sr, err
}

func (w *Workflow) runParallel(wc *WorkflowContext, step workflowStep) ([]StepResult, error) {
	var (
		mu       sync.Mutex
		results  []StepResult
		wg       sync.WaitGroup
		firstErr error
	)

	for _, s := range step.parallel {
		wg.Add(1)
		go func(s workflowStep) {
			defer wg.Done()
			sr, err := w.runStep(wc, s)
			mu.Lock()
			results = append(results, sr)
			if err != nil && firstErr == nil {
				firstErr = err
			}
			mu.Unlock()
		}(s)
	}

	wg.Wait()
	return results, firstErr
}

func (w *Workflow) fireHook(step, event string, wc *WorkflowContext) {
	for _, h := range w.hooks {
		h(step, event, wc)
	}
}
