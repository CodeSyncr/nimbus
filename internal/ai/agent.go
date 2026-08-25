package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// AgentState represents the state of the agent.
type AgentState string

const (
	StateIdle      AgentState = "idle"
	StatePlanning  AgentState = "planning"
	StateReviewing AgentState = "reviewing"
	StateExecuting AgentState = "executing"
	StateCompleted AgentState = "completed"
	StateFailed    AgentState = "failed"
)

// AgentCallbacks defines hooks for the TUI to render real-time progress.
type AgentCallbacks struct {
	OnRequestSent        func()
	OnStreamDelta        func(delta string)
	OnPlanGenerated      func(plan *PlanSummary)
	OnStepUpdate         func(step *PlanStep)
	OnDiffGenerated      func(filePath, diff string)
	OnToolCall           func(toolName string, args map[string]any)
	OnToolResult         func(toolName string, args map[string]any, output string, err error)
	OnExecutionCompleted func(summary string)
}

// Agent manages the two-phase planning and execution flow.
type Agent struct {
	Client    AIClient
	Tools     *ToolExecutor
	Context   *ProjectContext
	Session   *Session
	Model     string
	State     AgentState
	Callbacks AgentCallbacks
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
	return &Agent{
		Client:    client,
		Tools:     tools,
		Context:   projCtx,
		Session:   session,
		Model:     model,
		State:     StateIdle,
		Callbacks: AgentCallbacks{},
	}
}

// PlanSystemPrompt defines strict JSON output rules for Plan Mode.
const PlanSystemPrompt = `You are Nimbus AI, an expert full-stack software engineer and architectural planner with deep mastery across modern web technologies (HTML/CSS/JS, React, Vue, Python, Go, SQL) and specialized expertise in the Nimbus Go Framework.

ROLE & BEHAVIOR:
1. You are a flexible, general-purpose coding copilot. Adapt to whatever language, library, or tech stack the user specifies or that exists in their workspace.
2. If the user request is specific about the stack (e.g. "create todo app using html css and js"), plan ONLY for that stack. Do NOT force Go or Nimbus framework files unless requested or present in the workspace.
3. If the user request is broad or underspecified (e.g. "create todo app"), you may set "needs_clarification": true and provide 2-3 essential clarification questions (tech stack with Nimbus featured, database, styling).
4. Output ONLY valid, raw JSON matching the schema. Do NOT output markdown backticks outside the JSON.

SCHEMA:
{
  "summary": "High-level summary of what will be built or modified",
  "overview": "Architectural explanation of the proposed changes",
  "needs_clarification": false,
  "questions": [
    {
      "id": "tech_stack",
      "question": "Which technology stack would you prefer?",
      "options": ["Nimbus Go + Livewire (Recommended)", "Plain HTML, CSS & JavaScript", "Go Standard Library", "React / Next.js"],
      "default": "Nimbus Go + Livewire (Recommended)"
    }
  ],
  "phases": [
    {
      "name": "Phase 1: Layer Name",
      "description": "What is accomplished in this phase",
      "files": ["path/to/file"]
    }
  ],
  "steps": [
    {
      "id": 1,
      "phase": "Phase 1: Layer Name",
      "action": "create_file|edit_file|run_command|delete_file",
      "target": "path/to/file or command string",
      "description": "Clear explanation of what this step accomplishes",
      "risk": "low|medium|high"
    }
  ]
}`

// ExecuteSystemPrompt defines rules and tool constraints during Execute Mode.
const ExecuteSystemPrompt = `You are Nimbus AI executing an APPROVED architectural plan.
You have access to tools: read_file, list_dir, grep, write_file, edit_file, delete_file, bash, load_skill.

EXECUTION RULES:
1. Execute the approved steps in logical order.
2. Write complete, production-ready code with no placeholders or TODO comments.
3. Follow the exact language, framework, and design constraints of the approved plan.
4. Before modifying an existing file, use read_file or grep to inspect the current state if needed.
5. Check the Available Skills list below. If a skill's description matches the current task, call load_skill before proceeding with related work. Don't load skills that aren't relevant.
6. If the project contains build/test verification (e.g. Go, Node.js), use bash to verify compilation where appropriate.
7. When all steps are executed, provide a concise summary of changes.`

// GeneratePlan generates a structured plan for the user prompt.
func (a *Agent) GeneratePlan(ctx context.Context, userPrompt string) (*PlanSummary, error) {
	a.State = StatePlanning
	a.Session.InitialQuery = userPrompt

	if a.Callbacks.OnRequestSent != nil {
		a.Callbacks.OnRequestSent()
	}

	plan, err := a.Client.GeneratePlan(ctx, userPrompt, a.Context, a.Model)
	if err != nil {
		a.State = StateFailed
		return nil, fmt.Errorf("plan generation failed: %w", err)
	}

	// Default all steps to approved initially for user review
	for i := range plan.Steps {
		plan.Steps[i].Approved = true
		plan.Steps[i].Status = "pending"
	}

	a.Session.Plan = plan
	a.Session.History = append(a.Session.History, Message{Role: "user", Content: userPrompt})
	a.State = StateReviewing

	if a.Callbacks.OnPlanGenerated != nil {
		a.Callbacks.OnPlanGenerated(plan)
	}

	_ = SaveSession(a.Context.AppRoot, a.Session)
	return plan, nil
}

// RegenerateStep regenerates a modified step and any downstream steps.
func (a *Agent) RegenerateStep(ctx context.Context, stepIndex int, newDescription string) (*PlanSummary, error) {
	if a.Session.Plan == nil || stepIndex < 0 || stepIndex >= len(a.Session.Plan.Steps) {
		return nil, errors.New("no active plan to regenerate")
	}

	if a.Callbacks.OnRequestSent != nil {
		a.Callbacks.OnRequestSent()
	}

	updatedPlan, err := a.Client.RegenerateStep(ctx, stepIndex, newDescription, a.Session.Plan, a.Context, a.Model)
	if err != nil {
		return nil, err
	}

	for i := range updatedPlan.Steps {
		updatedPlan.Steps[i].Approved = true
		updatedPlan.Steps[i].Status = "pending"
	}

	a.Session.Plan = updatedPlan
	_ = SaveSession(a.Context.AppRoot, a.Session)
	return updatedPlan, nil
}

// ExecuteApprovedPlan executes the approved steps using tools.
func (a *Agent) ExecuteApprovedPlan(ctx context.Context, plan *PlanSummary) (string, error) {
	a.State = StateExecuting
	a.Session.ApprovedPlan = plan
	_ = SaveSession(a.Context.AppRoot, a.Session)

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

	messages := []Message{
		{Role: "user", Content: fmt.Sprintf("Execute the approved steps for prompt: %s", a.Session.InitialQuery)},
	}

	tools := a.Tools.GetToolDefinitions()

	// Tool loop — Max 25 iterations
	for iter := 0; iter < 25; iter++ {
		if a.Callbacks.OnRequestSent != nil {
			a.Callbacks.OnRequestSent()
		}
		resp, err := a.Client.StreamExecute(ctx, a.Session.InitialQuery, plan, messages, tools, a.Context, func(delta string) {
			if a.Callbacks.OnStreamDelta != nil {
				a.Callbacks.OnStreamDelta(delta)
			}
		})
		if err != nil {
			a.State = StateFailed
			return "", fmt.Errorf("execution error at step %d: %w", iter, err)
		}

		toolBlocks := resp.ToolUseBlocks()
		if len(toolBlocks) == 0 {
			// No more tool calls; finished
			finalText := resp.TextContent()
			a.State = StateCompleted
			a.Session.Status = "completed"
			if a.Callbacks.OnExecutionCompleted != nil {
				a.Callbacks.OnExecutionCompleted(finalText)
			}
			_ = SaveSession(a.Context.AppRoot, a.Session)
			return finalText, nil
		}

		// Append assistant response with tool calls to history
		messages = append(messages, Message{
			Role:    "assistant",
			Content: resp.Content,
		})

		// Execute tool calls and create tool results
		var toolResults []ContentBlock
		for _, tb := range toolBlocks {
			if a.Callbacks.OnToolCall != nil {
				a.Callbacks.OnToolCall(tb.Name, tb.Input)
			}

			var out string
			var diff string
			var err error

			if tb.Name == "load_skill" || tb.Name == "read_skill" || tb.Name == "query_skill" {
				skillName, _ := tb.Input["skill_name"].(string)
				if skillName == "" {
					skillName, _ = tb.Input["name"].(string)
				}
				skillName = strings.TrimSpace(skillName)
				if a.Session.LoadedSkills == nil {
					a.Session.LoadedSkills = make(map[string]string)
				}
				if cached, ok := a.Session.LoadedSkills[skillName]; ok && cached != "" && tb.Name != "query_skill" {
					out = cached
					err = nil
				} else {
					out, diff, err = a.Tools.ExecuteTool(ctx, tb.Name, tb.Input)
					if err == nil && out != "" && tb.Name != "query_skill" {
						a.Session.LoadedSkills[skillName] = out
						_ = SaveSession(a.Context.AppRoot, a.Session)
					}
				}

				if err == nil && out != "" {
					// Mount active skill into System Frame slot instead of polluting message history
					a.Context.ActiveSkillFrame = out
				}
			} else {
				out, diff, err = a.Tools.ExecuteTool(ctx, tb.Name, tb.Input)
			}

			if a.Callbacks.OnToolResult != nil {
				a.Callbacks.OnToolResult(tb.Name, tb.Input, out, err)
			}

			if diff != "" && a.Callbacks.OnDiffGenerated != nil {
				path, _ := tb.Input["path"].(string)
				a.Callbacks.OnDiffGenerated(path, diff)
			}

			// Compact result text in history if loading skill to keep context lightweight
			historyContent := out
			if (tb.Name == "load_skill" || tb.Name == "read_skill" || tb.Name == "query_skill") && err == nil {
				historyContent = "[Skill content mounted into active system frame]"
			}

			resBlock := ContentBlock{
				Type:      "tool_result",
				ToolUseID: tb.ID,
				Content:   historyContent,
			}
			if err != nil {
				resBlock.IsError = true
				resBlock.Content = fmt.Sprintf("Error: %v", err)
			}
			toolResults = append(toolResults, resBlock)

		}

		// Append tool results to messages
		messages = append(messages, Message{
			Role:    "user",
			Content: toolResults,
		})
	}

	a.State = StateCompleted
	return "Reached maximum execution limit.", nil
}

func parsePlanJSON(raw string) (*PlanSummary, error) {
	raw = strings.TrimSpace(raw)
	// Strip markdown code fences if model enclosed it
	reFence := regexp.MustCompile("(?s)^```(?:json)?\\s*(.+?)\\s*```$")
	if m := reFence.FindStringSubmatch(raw); len(m) == 2 {
		raw = strings.TrimSpace(m[1])
	} else if strings.Contains(raw, "{") && strings.Contains(raw, "}") {
		start := strings.Index(raw, "{")
		end := strings.LastIndex(raw, "}")
		if start != -1 && end != -1 && end > start {
			raw = raw[start : end+1]
		}
	}

	var plan PlanSummary
	if err := json.Unmarshal([]byte(raw), &plan); err != nil {
		return nil, err
	}
	if len(plan.Steps) == 0 {
		return nil, errors.New("plan contains 0 steps")
	}
	return &plan, nil
}
