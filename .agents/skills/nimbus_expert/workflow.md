# Workflows

A durable, multi-step orchestration engine: steps with dependencies, retries,
timeouts, conditional execution, and waits for external events. State lives in a
`Store`, so a run survives a process restart.

## Defining a workflow

```go
onboarding := workflow.Define("user-onboarding", func(r *workflow.Run) {
    r.Step("create-account", createAccount)

    r.Step("send-welcome", sendWelcome).
        After("create-account").
        Retry(3, 2*time.Second)

    r.Step("provision", provision).
        After("create-account").
        Parallel().
        WithTimeout(30 * time.Second)

    r.Step("await-verification", noop).
        After("send-welcome").
        WaitForEvent("email:verified", 24*time.Hour)

    r.Step("upgrade", upgrade).
        After("await-verification").
        When(func(p workflow.Payload) bool { return p["plan"] == "pro" })

    r.Step("analytics", track).
        After("create-account").
        ContinueOnFailure()
})
```

A step is `func(ctx context.Context, payload Payload) (Payload, error)`. The
returned payload is merged into the run payload, so later steps see earlier
results. `Payload` is `map[string]any`.

### `StepBuilder`

| Method | Effect |
| --- | --- |
| `After(deps ...string)` | Run only after these steps complete |
| `Parallel()` | May run concurrently with its siblings |
| `Retry(max int, delay time.Duration)` | Retry on error |
| `WithTimeout(d)` | Cancel the step's context after `d` |
| `WaitForEvent(event string, timeout)` | Suspend until `Signal` arrives, or time out |
| `When(fn func(Payload) bool)` | Skip unless the predicate passes |
| `ContinueOnFailure()` | A failure does not fail the run |

## Running

```go
engine := workflow.NewEngine(store)
engine.Register(onboarding)

runID, err := engine.Dispatch("user-onboarding", workflow.Payload{"user_id": id})

// or block until it finishes
run, err := engine.DispatchSync(ctx, "user-onboarding", payload)
```

| Method | Purpose |
| --- | --- |
| `Register(def)` | Make a definition runnable |
| `Dispatch(name, payload) (string, error)` | Start asynchronously; returns the run id |
| `DispatchSync(ctx, name, payload) (*RunInstance, error)` | Start and wait |
| `Signal(runID, event, data) error` | Deliver the event a `WaitForEvent` step is blocked on |
| `Cancel(ctx, runID) error` | Stop a run |
| `Status(ctx, runID) (*RunInstance, error)` | Inspect a run |
| `List(ctx, workflow, limit) ([]*RunInstance, error)` | Recent runs |
| `Workflows() []string` | Registered definition names |
| `SetHooks(EngineHooks)` | Observe step and run transitions |

`RunInstance` carries a `RunStatus` and the `StepInstance` list, each with a
`StepStatus` — that is what a dashboard or a debugging session reads.

## Persistence

`Store` is an interface — `Save`, `Load`, `List`, `Delete`. `NewMemoryStore()`
ships in the box.

**A memory store loses every in-flight run on restart.** Anything with a
`WaitForEvent` step measured in hours needs a durable store, or the wait never
resumes.

## As a plugin

```go
app.RegisterPlugin(workflow.NewPlugin(store))
```

`WorkflowPlugin` implements `Register`, `Boot`, `DefaultConfig`, and
`RegisterRoutes`, so mounting it exposes the run-inspection endpoints.

## Workflow vs queue

| Use | Reach for |
| --- | --- |
| One unit of background work | [queue jobs](queue_jobs.md) |
| Several steps with ordering, waits, or partial failure | Workflow |

A workflow step that does real work should still be idempotent — a retry runs it
again.
