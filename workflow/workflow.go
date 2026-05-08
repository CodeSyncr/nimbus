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
	"time"
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
