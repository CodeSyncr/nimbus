package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// AgentState represents the state of the agent.
type AgentState string

const (
	StateIdle      AgentState = "idle"
	StateExploring AgentState = "exploring"
	StatePlanning  AgentState = "planning"
	StateReviewing AgentState = "reviewing"
	StateExecuting AgentState = "executing"
	StateVerifying AgentState = "verifying"
	StateCompleted AgentState = "completed"
	StateFailed    AgentState = "failed"
)

// AgentCallbacks defines hooks for the TUI to render real-time progress.
type AgentCallbacks struct {
	OnRequestSent        func()
	OnStreamDelta        func(delta string)
	OnStatus             func(text string)
	OnPlanGenerated      func(plan *PlanSummary)
	OnStepUpdate         func(step *PlanStep)
	OnDiffGenerated      func(filePath, diff string)
	OnToolCall           func(toolName string, args map[string]any)
	OnToolResult         func(toolName string, args map[string]any, output string, err error)
	OnExecutionCompleted func(summary string)
	// OnUsage reports the tokens a turn consumed and the session total.
	OnUsage func(turn *TokenUsage, session SessionUsage)
}

// Loop limits.
const (
	maxExploreIterations = 14
	maxPlanIterations    = 8
	maxExecuteIterations = 60
	maxRepairRounds      = 3
	maxRepeatedToolCalls = 3
)

// Agent manages the explore → plan → execute → verify flow.
type Agent struct {
	Client    AIClient
	Tools     *ToolExecutor
	Context   *ProjectContext
	Session   *Session
	Model     string
	State     AgentState
	Callbacks AgentCallbacks

	// Verifier runs the project's build/tests after execution and returns
	// (output, ok). Defaults to a Go build when go.mod is present; tests
	// override it. Nil disables verification.
	Verifier func(ctx context.Context) (string, bool)

	// turnSupport caches whether the server implements /ai/turn:
	// 0 unknown, 1 supported, -1 unsupported (legacy endpoints are used).
	turnSupport int
	// agentModeUnsupported records that this server predates the "agent"
	// turn mode, so the conversational loop falls back to chat mode.
	agentModeUnsupported bool
	// changedFiles collects paths touched by write/edit/delete tools during
	// the current execution, for the conversation memory.
	changedFiles map[string]bool
	// lastToolSig / repeatCount detect a model stuck re-issuing one call.
	lastToolSig string
	repeatCount int
	// settings is the resolved configuration for this run. See ApplySettings.
	settings Settings
	// compactStalledAt records the size at which a compaction attempt
	// reclaimed nothing, so the next one waits for real growth instead of
	// paying for a summary every iteration. Zero means not stalled.
	compactStalledAt int
}

// NewAgent creates a new Nimbus AI Agent.
func NewAgent(client AIClient, tools *ToolExecutor, projCtx *ProjectContext, session *Session) *Agent {
	if session == nil {
		session = NewSession("optimal")
	}
	model := session.Model
	if model == "" {
		model = "optimal"
	}
	a := &Agent{
		Client:    client,
		Tools:     tools,
		Context:   projCtx,
		Session:   session,
		Model:     model,
		State:     StateIdle,
		Callbacks: AgentCallbacks{},
		settings:  DefaultSettings(),
	}
	a.Verifier = a.defaultVerifier

	// Surface transport retries as ordinary status: a stalled run that is
	// quietly backing off looks identical to a hung one otherwise.
	if r, ok := client.(RetryReporter); ok {
		r.SetRetryHook(func(attempt int, reason string) {
			a.status(fmt.Sprintf("Connection problem (%s) — retrying, attempt %d of %d", reason, attempt+1, maxAttempts))
		})
	}
	return a
}

// ApplySettings wires resolved settings into the agent and the pieces it owns.
//
// One place does this so a setting cannot be half-applied — read by the screen
// that shows it but never reaching the code that acts on it, which is the
// usual way a configuration screen becomes a lie.
func (a *Agent) ApplySettings(s Settings) {
	a.settings = s

	if a.Session != nil {
		a.Session.SetLimits(s.ContextLimit, s.CompactThresholdPercent)
		if s.Model != "" {
			a.Session.Model = s.Model
		}
	}
	if s.Model != "" {
		a.Model = s.Model
	}
	if a.Tools != nil {
		a.Tools.SetPermissionMode(ParsePermissionMode(s.PermissionMode))
		a.Tools.MaxCommandOutput = s.MaxCommandOutput
	}

	// Verification is a function rather than a flag, so turning it off means
	// removing it; defaultVerifier is restored rather than remembered, since
	// a caller that installed its own (the tests do) sets it after this.
	if s.VerifyBuilds {
		if a.Verifier == nil {
			a.Verifier = a.defaultVerifier
		}
	} else {
		a.Verifier = nil
	}
}

// Settings returns the settings currently in force.
func (a *Agent) Settings() Settings { return a.settings }

// PlanSystemPrompt documents the plan JSON contract the CLI expects. The
// authoritative prompts live on Nimbus Cloud; this is kept for reference and
// offline tooling.
const PlanSystemPrompt = `You are Nimbus AI, an expert full-stack software engineer and architectural planner.
Ground every plan in the actual code you investigated. Output ONLY valid JSON matching:
{
  "summary": "...", "overview": "...", "needs_clarification": false,
  "questions": [{"id": "...", "question": "...", "options": ["..."], "default": "..."}],
  "phases": [{"name": "...", "description": "...", "files": ["..."]}],
  "steps": [{"id": 1, "phase": "...", "action": "create_file|edit_file|run_command|delete_file", "target": "...", "description": "...", "risk": "low|medium|high"}]
}`

// ExecuteSystemPrompt documents execution rules (authoritative copy on Nimbus Cloud).
const ExecuteSystemPrompt = `You are Nimbus AI executing an APPROVED plan with tools: read_file, list_dir, find_files, grep, bash, load_skill, write_file, edit_file, delete_file.
Read before you edit, prefer edit_file for existing files, verify with the build, and finish with a concise summary.`

// ---------------------------------------------------------------------------
// Callbacks helpers
// ---------------------------------------------------------------------------

func (a *Agent) status(text string) {
	if a.Callbacks.OnStatus != nil {
		a.Callbacks.OnStatus(text)
	}
}

func (a *Agent) requestSent() {
	if a.Callbacks.OnRequestSent != nil {
		a.Callbacks.OnRequestSent()
	}
}

func (a *Agent) delta() StreamHandler {
	return func(d string) {
		if a.Callbacks.OnStreamDelta != nil {
			a.Callbacks.OnStreamDelta(d)
		}
	}
}

func (a *Agent) saveSession() {
	if a.Context != nil && a.Context.AppRoot != "" {
		_ = SaveSession(a.Context.AppRoot, a.Session)
	}
}

// turn runs one model turn, tracking whether the server supports it.
func (a *Agent) turn(ctx context.Context, mode TurnMode, messages []Message, tools []ToolDefinition, plan *PlanSummary) (*MessageResponse, error) {
	// A client that never resolved would otherwise crash the terminal on the
	// first turn, in a goroutine, with a nil dereference.
	if a.Client == nil {
		return nil, errors.New("no AI client is configured for this session")
	}
	if a.turnSupport < 0 {
		return nil, ErrTurnUnsupported
	}
	// Servers older than the conversational loop reject "agent"; chat is the
	// closest mode they understand and keeps the CLI working against them.
	if mode == TurnModeAgent && a.agentModeUnsupported {
		mode = TurnModeChat
	}
	a.requestSent()
	resp, err := a.Client.Turn(ctx, &TurnRequest{
		Mode:     mode,
		Model:    a.Model,
		Prompt:   a.Session.InitialQuery,
		Messages: messages,
		Tools:    tools,
		Plan:     plan,
		Context:  a.Context,
	}, a.delta())
	if err != nil {
		if errors.Is(err, ErrTurnUnsupported) {
			a.turnSupport = -1
		}
		// An older server answers an unknown mode with 400; retry once in a
		// mode it knows rather than failing the user's request.
		if mode == TurnModeAgent && isUnknownModeErr(err) {
			a.agentModeUnsupported = true
			return a.turn(ctx, TurnModeChat, messages, tools, plan)
		}
		return nil, err
	}
	a.turnSupport = 1

	// One place to account for spend: every model turn passes through here.
	if a.Session != nil {
		a.Session.Usage.Add(resp.Usage)
	}
	if a.Callbacks.OnUsage != nil && resp.Usage != nil {
		a.Callbacks.OnUsage(resp.Usage, a.Session.Usage)
	}
	return resp, nil
}

// isUnknownModeErr detects a server that does not know the requested mode.
// Servers phrase this differently ("unknown mode", "bad mode",
// "unsupported mode"), so the check is on the shape of the complaint; the
// cost of a false positive is one extra attempt in chat mode.
func isUnknownModeErr(err error) bool {
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "mode") {
		return false
	}
	for _, marker := range []string{"unknown", "bad", "unsupported", "invalid", "not supported"} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Planning (explore → plan)
// ---------------------------------------------------------------------------

// GeneratePlan investigates the codebase for the request, then produces a
// structured plan grounded in what it found. Conversational requests come
// back as a plan with zero steps whose Summary holds the answer.
func (a *Agent) GeneratePlan(ctx context.Context, userPrompt string) (*PlanSummary, error) {
	a.Session.InitialQuery = userPrompt
	a.Session.History = append(a.Session.History, Message{Role: "user", Content: userPrompt})
	if a.Context != nil {
		a.Context.Refresh()
	}

	var plan *PlanSummary
	var err error
	if a.turnSupport >= 0 {
		plan, err = a.planWithTurns(ctx, userPrompt)
		if err != nil && !errors.Is(err, ErrTurnUnsupported) {
			a.State = StateFailed
			return nil, fmt.Errorf("plan generation failed: %w", err)
		}
	}
	if a.turnSupport < 0 {
		// Legacy server: single-shot planning endpoint. Carry conversation
		// memory in the prompt so follow-ups still have context.
		a.State = StatePlanning
		a.requestSent()
		plan, err = a.Client.GeneratePlan(ctx, a.promptWithMemory(userPrompt), a.Context, a.Model)
		if err != nil {
			a.State = StateFailed
			return nil, fmt.Errorf("plan generation failed: %w", err)
		}
	}

	// Default all steps to approved initially for user review
	for i := range plan.Steps {
		plan.Steps[i].Approved = true
		plan.Steps[i].Status = "pending"
		if plan.Steps[i].ID == 0 {
			plan.Steps[i].ID = i + 1
		}
	}

	a.Session.Plan = plan
	a.State = StateReviewing
	if len(plan.Steps) == 0 && !plan.NeedsClarification {
		// Conversational answer: remember it so follow-ups can refer back.
		a.Session.RecordTurn(userPrompt, "", plan.Summary, nil)
		a.State = StateCompleted
	}

	if a.Callbacks.OnPlanGenerated != nil {
		a.Callbacks.OnPlanGenerated(plan)
	}

	a.saveSession()
	return plan, nil
}

func (a *Agent) promptWithMemory(userPrompt string) string {
	memory := a.Session.ConversationSummary()
	if memory == "" {
		return userPrompt
	}
	return fmt.Sprintf("%s\n\nEARLIER IN THIS SESSION:\n%s", userPrompt, memory)
}

func (a *Agent) planWithTurns(ctx context.Context, userPrompt string) (*PlanSummary, error) {
	// Phase 1: explore. Read-only tools; ends with a findings report.
	a.State = StateExploring
	a.status("Exploring the codebase…")
	findings, err := a.explore(ctx, userPrompt)
	if err != nil {
		return nil, err
	}
	a.Session.Findings = findings
	a.saveSession()

	// Phase 2: plan. The model may still read a few more files, then must
	// answer with the plan JSON (or a direct answer for questions).
	a.State = StatePlanning
	a.status("Designing the plan…")
	messages := []Message{{Role: "user", Content: a.planBrief(userPrompt, findings)}}
	tools := a.Tools.ReadOnlyToolDefinitions()

	var lastText string
	for iter := 0; iter < maxPlanIterations; iter++ {
		resp, err := a.turn(ctx, TurnModePlan, messages, tools, nil)
		if err != nil {
			return nil, err
		}
		toolBlocks := resp.ToolUseBlocks()
		lastText = resp.TextContent()
		if len(toolBlocks) == 0 {
			break
		}
		messages = append(messages, Message{Role: "assistant", Content: resp.Content})
		results := a.runToolCalls(ctx, toolBlocks)
		messages = append(messages, Message{Role: "user", Content: results})
		lastText = ""
	}
	if strings.TrimSpace(lastText) == "" {
		// The model kept calling tools; demand the final answer once more.
		messages = append(messages, Message{Role: "user", Content: "You have enough information. Respond now with the final plan JSON (or a direct answer) and no further tool calls."})
		resp, err := a.turn(ctx, TurnModePlan, messages, nil, nil)
		if err != nil {
			return nil, err
		}
		lastText = resp.TextContent()
	}

	plan, err := parsePlanJSON(lastText)
	if err != nil {
		if strings.TrimSpace(lastText) == "" {
			return nil, errors.New("the model returned an empty plan")
		}
		// A plan that was cut off is still JSON-shaped, and echoing it puts a
		// wall of broken JSON on screen as though it were the answer. Say what
		// happened instead.
		if looksLikeTruncatedJSON(lastText) {
			return nil, errors.New(
				"the plan came back incomplete — the model hit its output limit mid-response. " +
					"Ask again, narrow the request, or raise AI_MAX_TOKENS on the server")
		}
		// Genuinely not JSON: a conversational answer.
		return &PlanSummary{Summary: strings.TrimSpace(lastText), Overview: strings.TrimSpace(lastText), Steps: []PlanStep{}}, nil
	}
	return plan, nil
}

// explore runs the read-only investigation loop and returns the findings.
func (a *Agent) explore(ctx context.Context, userPrompt string) (string, error) {
	messages := []Message{{Role: "user", Content: a.exploreBrief(userPrompt)}}
	tools := a.Tools.ReadOnlyToolDefinitions()
	a.resetRepeatGuard()

	var findings string
	for iter := 0; iter < maxExploreIterations; iter++ {
		resp, err := a.turn(ctx, TurnModeExplore, messages, tools, nil)
		if err != nil {
			return "", err
		}
		toolBlocks := resp.ToolUseBlocks()
		if len(toolBlocks) == 0 {
			findings = resp.TextContent()
			break
		}
		messages = append(messages, Message{Role: "assistant", Content: resp.Content})
		results := a.runToolCalls(ctx, toolBlocks)
		if nudge := a.repeatNudge(toolBlocks); nudge != "" {
			results = append(results, ContentBlock{Type: "text", Text: nudge})
		}
		messages = append(messages, Message{Role: "user", Content: results})
	}
	if strings.TrimSpace(findings) == "" {
		// Out of iterations: ask for the report without tools.
		messages = append(messages, Message{Role: "user", Content: "Stop investigating and write the Findings report now, based on what you have read so far."})
		resp, err := a.turn(ctx, TurnModeExplore, messages, nil, nil)
		if err != nil {
			return "", err
		}
		findings = resp.TextContent()
	}
	return strings.TrimSpace(findings), nil
}

func (a *Agent) exploreBrief(userPrompt string) string {
	var sb strings.Builder
	sb.WriteString("REQUEST:\n")
	sb.WriteString(userPrompt)
	if memory := a.Session.ConversationSummary(); memory != "" {
		sb.WriteString("\n\nEARLIER IN THIS SESSION:\n")
		sb.WriteString(memory)
	}
	sb.WriteString("\n\nInvestigate the workspace so the plan for this request can be grounded in real code. Use find_files, grep, list_dir and read_file to locate and read every file the change would touch or must integrate with (routes, models, controllers, views, config, tests, existing conventions). Do not modify anything. When you have enough, reply with the Findings report.")
	return sb.String()
}

func (a *Agent) planBrief(userPrompt, findings string) string {
	var sb strings.Builder
	sb.WriteString("REQUEST:\n")
	sb.WriteString(userPrompt)
	if memory := a.Session.ConversationSummary(); memory != "" {
		sb.WriteString("\n\nEARLIER IN THIS SESSION:\n")
		sb.WriteString(memory)
	}
	if findings != "" {
		sb.WriteString("\n\nFINDINGS FROM INVESTIGATION:\n")
		sb.WriteString(findings)
	}
	sb.WriteString("\n\nProduce the plan for this request as JSON. Every step must reference real paths from the findings (edit_file for existing files, create_file for new ones, run_command for verification). If the request is a question or explanation rather than a change, answer it directly in \"summary\" with an empty \"steps\" array. You may read more files first if something essential is missing.")
	return sb.String()
}

// RegenerateStep regenerates a modified step and any downstream steps.
func (a *Agent) RegenerateStep(ctx context.Context, stepIndex int, newDescription string) (*PlanSummary, error) {
	if a.Session.Plan == nil || stepIndex < 0 || stepIndex >= len(a.Session.Plan.Steps) {
		return nil, errors.New("no active plan to regenerate")
	}

	a.requestSent()

	updatedPlan, err := a.Client.RegenerateStep(ctx, stepIndex, newDescription, a.Session.Plan, a.Context, a.Model)
	if err != nil {
		return nil, err
	}

	for i := range updatedPlan.Steps {
		updatedPlan.Steps[i].Approved = true
		updatedPlan.Steps[i].Status = "pending"
	}

	a.Session.Plan = updatedPlan
	a.saveSession()
	return updatedPlan, nil
}

// ---------------------------------------------------------------------------
// Execution (execute → verify → repair)
// ---------------------------------------------------------------------------

// ExecuteApprovedPlan executes the approved steps using tools, then verifies
// the result (build) and lets the model repair failures.
func (a *Agent) ExecuteApprovedPlan(ctx context.Context, plan *PlanSummary) (string, error) {
	a.State = StateExecuting
	a.Session.ApprovedPlan = plan
	a.Session.Status = "executing"
	a.changedFiles = map[string]bool{}
	a.resetRepeatGuard()
	a.saveSession()

	// Filter approved steps
	var approvedSteps []PlanStep
	for _, s := range plan.Steps {
		if s.Approved {
			approvedSteps = append(approvedSteps, s)
		}
	}

	if len(approvedSteps) == 0 {
		a.State = StateCompleted
		return "No steps were approved for execution.", nil
	}
	approvedPlan := *plan
	approvedPlan.Steps = approvedSteps

	var summary string
	var err error
	if a.turnSupport >= 0 {
		summary, err = a.executeWithTurns(ctx, &approvedPlan)
		if err != nil && !errors.Is(err, ErrTurnUnsupported) {
			a.State = StateFailed
			return "", err
		}
	}
	if a.turnSupport < 0 {
		summary, err = a.executeLegacy(ctx, &approvedPlan)
		if err != nil {
			a.State = StateFailed
			return "", err
		}
	}

	a.State = StateCompleted
	a.Session.Status = "completed"
	a.Session.RecordTurn(a.Session.InitialQuery, plan.Summary, summary, a.changedFileList())
	if a.Context != nil {
		a.Context.Refresh()
	}
	if a.Callbacks.OnExecutionCompleted != nil {
		a.Callbacks.OnExecutionCompleted(summary)
	}
	a.saveSession()
	return summary, nil
}

func (a *Agent) executeWithTurns(ctx context.Context, plan *PlanSummary) (string, error) {
	a.status("Executing the plan…")
	messages := []Message{{Role: "user", Content: a.executeBrief(plan)}}
	tools := a.Tools.GetToolDefinitions()
	repairRounds := 0

	for iter := 0; iter < maxExecuteIterations; iter++ {
		resp, err := a.turn(ctx, TurnModeExecute, messages, tools, plan)
		if err != nil {
			return "", err
		}
		toolBlocks := resp.ToolUseBlocks()
		if len(toolBlocks) == 0 {
			finalText := strings.TrimSpace(resp.TextContent())
			// The model believes it is done: verify before accepting.
			if a.Verifier != nil && len(a.changedFiles) > 0 && repairRounds < maxRepairRounds {
				a.State = StateVerifying
				a.status("Verifying the build…")
				out, ok := a.Verifier(ctx)
				a.State = StateExecuting
				if !ok {
					repairRounds++
					a.status(fmt.Sprintf("Build failed — repairing (%d/%d)…", repairRounds, maxRepairRounds))
					messages = append(messages, Message{Role: "assistant", Content: resp.Content})
					messages = append(messages, Message{Role: "user", Content: fmt.Sprintf(
						"VERIFICATION FAILED. The project does not build after your changes:\n\n%s\n\nRead the errors, fix the root cause with edit_file/write_file, re-run the build with bash, and only then reply with the final summary.", out)})
					continue
				}
			}
			if finalText == "" {
				finalText = "All approved steps were executed."
			}
			return finalText, nil
		}

		messages = append(messages, Message{Role: "assistant", Content: resp.Content})
		results := a.runToolCalls(ctx, toolBlocks)
		if nudge := a.repeatNudge(toolBlocks); nudge != "" {
			results = append(results, ContentBlock{Type: "text", Text: nudge})
		}
		messages = append(messages, Message{Role: "user", Content: results})
	}

	return "Reached the maximum number of execution iterations. Review the changes made so far and run the agent again to continue.", nil
}

func (a *Agent) executeBrief(plan *PlanSummary) string {
	var sb strings.Builder
	sb.WriteString("REQUEST:\n")
	sb.WriteString(a.Session.InitialQuery)
	if memory := a.Session.ConversationSummary(); memory != "" {
		sb.WriteString("\n\nEARLIER IN THIS SESSION:\n")
		sb.WriteString(memory)
	}
	if a.Session.Findings != "" {
		sb.WriteString("\n\nFINDINGS FROM INVESTIGATION:\n")
		sb.WriteString(a.Session.Findings)
	}
	sb.WriteString("\n\nAPPROVED PLAN: ")
	sb.WriteString(plan.Summary)
	if plan.Overview != "" && plan.Overview != plan.Summary {
		sb.WriteString("\n")
		sb.WriteString(plan.Overview)
	}
	sb.WriteString("\n\nSTEPS:\n")
	for i, s := range plan.Steps {
		id := s.ID
		if id == 0 {
			id = i + 1
		}
		sb.WriteString(fmt.Sprintf("%d. [%s] %s — %s\n", id, s.Action, s.Target, s.Description))
	}
	sb.WriteString("\nExecute every step in order using the tools. Read existing files before changing them, use edit_file for targeted changes, write_file only for new files or full rewrites of files you have read, run the build/tests with bash to verify, fix any errors, and finish with a concise summary of what changed and how to verify it.")
	return sb.String()
}

// executeLegacy drives the pre-turn /ai/execute endpoint (older servers).
func (a *Agent) executeLegacy(ctx context.Context, plan *PlanSummary) (string, error) {
	messages := []Message{
		{Role: "user", Content: fmt.Sprintf("Execute the approved steps for prompt: %s", a.Session.InitialQuery)},
	}
	tools := a.Tools.GetToolDefinitions()

	for iter := 0; iter < 25; iter++ {
		a.requestSent()
		resp, err := a.Client.StreamExecute(ctx, a.Session.InitialQuery, plan, messages, tools, a.Context, a.delta())
		if err != nil {
			return "", fmt.Errorf("execution error at step %d: %w", iter, err)
		}

		toolBlocks := resp.ToolUseBlocks()
		if len(toolBlocks) == 0 {
			return resp.TextContent(), nil
		}

		messages = append(messages, Message{Role: "assistant", Content: resp.Content})
		results := a.runToolCalls(ctx, toolBlocks)
		messages = append(messages, Message{Role: "user", Content: results})
	}

	return "Reached maximum execution limit.", nil
}

// ---------------------------------------------------------------------------
// Tool execution
// ---------------------------------------------------------------------------

// runToolCalls executes tool_use blocks, fires callbacks, tracks changed
// files, and returns the matching tool_result blocks.
// parallelToolLimit bounds how many read-only tools run at once. It is a
// courtesy to the filesystem and to a server behind fetch_url, not a
// correctness constraint.
const parallelToolLimit = 8

// isParallelSafe reports whether a tool only observes the workspace.
//
// These can run together because none of them changes anything another could
// see: no file is written, no command runs, and the executor's own mutable
// state — the read cache and the injection taint — is behind a mutex.
// Everything else stays sequential, because the model issuing a write and
// then a read of the same path expects to see what it just wrote.
func isParallelSafe(name string) bool {
	switch strings.ToLower(name) {
	case "read_file", "read", "list_dir", "find_files", "glob", "grep", "search", "fetch_url":
		return true
	}
	return false
}

// toolOutcome is what one tool invocation produced.
type toolOutcome struct {
	out  string
	diff string
	err  error
}

// runToolCalls executes a model turn's tool calls and returns their results in
// the order they were asked for.
//
// A turn commonly opens with several reads at once — the model locating a
// handful of files before deciding anything — and running those one after
// another made the whole batch as slow as its parts combined. Maximal runs of
// read-only calls now execute together; anything else runs alone and in
// order. Results, callbacks and diffs stay strictly in the model's order
// either way, so nothing downstream can tell which path a call took.
func (a *Agent) runToolCalls(ctx context.Context, toolBlocks []ContentBlock) []ContentBlock {
	outcomes := make([]toolOutcome, len(toolBlocks))
	toolResults := make([]ContentBlock, 0, len(toolBlocks))

	for i := 0; i < len(toolBlocks); {
		j := i
		for j < len(toolBlocks) && isParallelSafe(toolBlocks[j].Name) {
			j++
		}
		// A lone read-only call is not worth a goroutine.
		if j-i < 2 {
			j = i + 1
		}

		group := toolBlocks[i:j]
		for _, tb := range group {
			if a.Callbacks.OnToolCall != nil {
				a.Callbacks.OnToolCall(tb.Name, tb.Input)
			}
		}

		if len(group) == 1 {
			outcomes[i] = a.runOneTool(ctx, group[0])
		} else {
			a.runToolGroup(ctx, group, outcomes[i:j])
		}

		// Reporting happens after the group, in the model's order, so the
		// transcript reads the same whether or not the calls overlapped.
		for k, tb := range group {
			res := outcomes[i+k]
			if res.err == nil {
				a.trackChange(tb.Name, tb.Input)
			}
			if a.Callbacks.OnToolResult != nil {
				a.Callbacks.OnToolResult(tb.Name, tb.Input, res.out, res.err)
			}
			if res.diff != "" && a.Callbacks.OnDiffGenerated != nil {
				path, _ := tb.Input["path"].(string)
				a.Callbacks.OnDiffGenerated(path, res.diff)
			}

			block := ContentBlock{Type: "tool_result", ToolUseID: tb.ID, Content: res.out}
			if res.err != nil {
				block.IsError = true
				block.Content = fmt.Sprintf("Error: %v", res.err)
			}
			toolResults = append(toolResults, block)
		}

		i = j
	}
	return toolResults
}

// runToolGroup executes read-only calls concurrently, writing each result to
// its own slot so the caller keeps the model's ordering.
func (a *Agent) runToolGroup(ctx context.Context, group []ContentBlock, into []toolOutcome) {
	limit := parallelToolLimit
	if len(group) < limit {
		limit = len(group)
	}
	sem := make(chan struct{}, limit)

	var wg sync.WaitGroup
	for idx := range group {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			into[idx] = a.runOneTool(ctx, group[idx])
		}(idx)
	}
	wg.Wait()
}

// runOneTool executes a single call. It reports no progress and fires no
// callbacks: those are the caller's, so that they stay ordered even when this
// runs on several goroutines at once.
func (a *Agent) runOneTool(ctx context.Context, tb ContentBlock) toolOutcome {
	if !isSkillTool(tb.Name) {
		out, diff, err := a.Tools.ExecuteTool(ctx, tb.Name, tb.Input)
		return toolOutcome{out: out, diff: diff, err: err}
	}

	// Skills are never in a parallel group: loading one writes the session and
	// the system frame, so it runs alone and this needs no lock.
	skillName, _ := tb.Input["skill_name"].(string)
	if skillName == "" {
		skillName, _ = tb.Input["name"].(string)
	}
	skillName = strings.TrimSpace(skillName)
	if a.Session.LoadedSkills == nil {
		a.Session.LoadedSkills = make(map[string]string)
	}

	// query_skill results depend on the query, so only full loads are cached.
	cacheable := tb.Name != "query_skill"
	var res toolOutcome
	if cached, ok := a.Session.LoadedSkills[skillName]; ok && cached != "" && cacheable {
		res.out = cached
	} else {
		res.out, res.diff, res.err = a.Tools.ExecuteTool(ctx, tb.Name, tb.Input)
		if res.err == nil && res.out != "" && cacheable {
			a.Session.LoadedSkills[skillName] = res.out
			a.saveSession()
		}
	}
	if res.err == nil && res.out != "" && a.Context != nil {
		// Mount the active skill into the system frame so the server can keep
		// it in the system prompt across turns.
		a.Context.ActiveSkillFrame = res.out
	}
	return res
}

func isSkillTool(name string) bool {
	return name == "load_skill" || name == "read_skill" || name == "query_skill"
}

func (a *Agent) trackChange(tool string, args map[string]any) {
	switch strings.ToLower(tool) {
	case "write_file", "create_file", "create", "write", "edit_file", "edit", "delete_file", "delete":
		if a.changedFiles == nil {
			a.changedFiles = map[string]bool{}
		}
		if p, ok := args["path"].(string); ok && p != "" {
			a.changedFiles[filepath.ToSlash(p)] = true
		}
	}
}

func (a *Agent) changedFileList() []string {
	if len(a.changedFiles) == 0 {
		return nil
	}
	files := make([]string, 0, len(a.changedFiles))
	for f := range a.changedFiles {
		files = append(files, f)
	}
	sort.Strings(files)
	return files
}

func (a *Agent) resetRepeatGuard() {
	a.lastToolSig = ""
	a.repeatCount = 0
}

// repeatNudge returns a message when the model keeps issuing the identical
// tool batch, which otherwise burns the whole iteration budget.
func (a *Agent) repeatNudge(toolBlocks []ContentBlock) string {
	var sb strings.Builder
	for _, tb := range toolBlocks {
		args, _ := json.Marshal(tb.Input)
		sb.WriteString(tb.Name)
		sb.Write(args)
		sb.WriteString(";")
	}
	sig := sb.String()
	if sig == a.lastToolSig {
		a.repeatCount++
	} else {
		a.lastToolSig = sig
		a.repeatCount = 1
	}
	if a.repeatCount >= maxRepeatedToolCalls {
		return "NOTE: you have issued this exact tool call " + fmt.Sprint(a.repeatCount) + " times; the result will not change. Use the output you already have, try a different approach, or finish."
	}
	return ""
}

// ---------------------------------------------------------------------------
// Verification
// ---------------------------------------------------------------------------

// defaultVerifier builds Go projects; other stacks are skipped (ok=true).
func (a *Agent) defaultVerifier(ctx context.Context) (string, bool) {
	if a.Tools == nil {
		return "", true
	}
	if _, err := os.Stat(filepath.Join(a.Tools.AppRoot, "go.mod")); err != nil {
		return "", true
	}
	out, ok := a.Tools.RunCommand(ctx, "go build ./...")
	if !ok {
		return out, false
	}
	if vetOut, vetOK := a.Tools.RunCommand(ctx, "go vet ./..."); !vetOK {
		return vetOut, false
	}
	return out, true
}

// ---------------------------------------------------------------------------
// Plan parsing
// ---------------------------------------------------------------------------

var reJSONFence = regexp.MustCompile("(?s)```(?:json)?\\s*(\\{.*?\\})\\s*```")

// looksLikeTruncatedJSON reports whether text was meant to be a plan but never
// finished: it opens like JSON (optionally inside a fence) and its braces
// never balance.
func looksLikeTruncatedJSON(text string) bool {
	trimmed := strings.TrimSpace(text)
	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSpace(trimmed)

	if !strings.HasPrefix(trimmed, "{") {
		return false
	}
	// Balanced braces mean it is complete JSON that failed to decode for some
	// other reason; unbalanced means it stopped partway.
	depth := 0
	inString := false
	escaped := false
	for _, r := range trimmed {
		switch {
		case escaped:
			escaped = false
		case r == '\\':
			escaped = true
		case r == '"':
			inString = !inString
		case inString:
			// braces inside strings do not count
		case r == '{':
			depth++
		case r == '}':
			depth--
		}
	}
	return depth > 0 || inString
}

// parsePlanJSON extracts the plan object from model output. It accepts a
// plan with steps, a clarification request, or a summary-only answer; it
// fails only when no JSON object can be decoded.
func parsePlanJSON(raw string) (*PlanSummary, error) {
	raw = strings.TrimSpace(raw)
	candidate := raw
	if m := reJSONFence.FindStringSubmatch(raw); len(m) == 2 {
		candidate = strings.TrimSpace(m[1])
	} else if start := strings.Index(raw, "{"); start != -1 {
		if end := strings.LastIndex(raw, "}"); end > start {
			candidate = raw[start : end+1]
		}
	}

	var plan PlanSummary
	if err := json.Unmarshal([]byte(candidate), &plan); err != nil {
		return nil, err
	}
	if len(plan.Steps) == 0 && len(plan.Questions) == 0 && strings.TrimSpace(plan.Summary) == "" {
		return nil, errors.New("plan contains no steps, questions, or summary")
	}
	if plan.NeedsClarification && len(plan.Questions) == 0 {
		plan.NeedsClarification = false
	}
	if plan.Steps == nil {
		plan.Steps = []PlanStep{}
	}
	return &plan, nil
}
