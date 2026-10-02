/*
|--------------------------------------------------------------------------
| AI SDK — Tool approval (human in the loop)
|--------------------------------------------------------------------------
|
| Some tools should not run just because a model asked: refunds, deletes,
| emails to customers. Mark them with RequireApproval (or Tool.
| NeedsApproval; MCP tools the server marks destructive are marked
| already), then either:
|
|   // decide on the spot (policy, CLI prompt):
|   agent.OnApproval(func(ctx context.Context, r ai.ApprovalRequest) (ai.Decision, error) {
|       return ai.Decision{Approved: r.Call.Name != "delete_account"}, nil
|   })
|
|   // or pause and ask a person (web apps):
|   resp, err := agent.Prompt(ctx, msg)
|   var pending *ai.ApprovalRequiredError
|   if errors.As(err, &pending) {
|       // show pending.Pending to the user, then in a later request:
|       resp, err = agent.Resume(ctx, ai.Approve(callID), ai.Deny(otherID, "too large"))
|   }
|
| A paused conversation is kept in the agent's memory (or, without memory,
| in the agent itself), so Resume works across requests when the agent has
| memory.
|
*/

package ai

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// ApprovalRequest is one tool call waiting for a decision.
type ApprovalRequest struct {
	Call ToolCall
	Tool *Tool
}

// Decision approves or denies one tool call.
type Decision struct {
	CallID   string
	Approved bool
	// Reason is told to the model when a call is denied.
	Reason string
}

// Approve approves the call with this id.
func Approve(callID string) Decision { return Decision{CallID: callID, Approved: true} }

// Deny denies the call with this id; reason is passed to the model.
func Deny(callID, reason string) Decision {
	return Decision{CallID: callID, Approved: false, Reason: reason}
}

// ApprovalFunc decides on a call as it happens.
type ApprovalFunc func(ctx context.Context, req ApprovalRequest) (Decision, error)

// ApprovalRequiredError is returned when tool calls need a person's
// decision. The conversation is saved; continue it with Agent.Resume.
type ApprovalRequiredError struct {
	Pending []ToolCall
}

func (e *ApprovalRequiredError) Error() string {
	names := make([]string, len(e.Pending))
	for i, c := range e.Pending {
		names[i] = c.Name
	}
	return "ai: tool calls need approval: " + strings.Join(names, ", ")
}

// ErrNothingToResume is returned by Resume when no call is waiting.
var ErrNothingToResume = errors.New("ai: no tool calls are waiting for approval")

// OnApproval decides on calls to tools that need approval as they happen.
// Without it, such calls pause the agent (see ApprovalRequiredError).
func (a *Agent) OnApproval(fn ApprovalFunc) *Agent {
	a.approval = fn
	return a
}

// findTool resolves a call to the tool that will run it.
func (a *Agent) findTool(name string) *Tool {
	if name == SkillToolName && a.skills.Len() > 0 {
		return nil
	}
	for _, t := range a.tools {
		if t.Name == name {
			return t
		}
	}
	if t, ok := GetTool(name); ok {
		return t
	}
	return nil
}

// runToolCalls executes one step's tool calls. Calls that need approval
// go to the approval hook; without one they are returned as pending and
// get no result yet.
func (a *Agent) runToolCalls(ctx context.Context, step int, calls []ToolCall) (msgs []Message, pending []ToolCall, handoff *Agent, err error) {
	if a.parallel && len(calls) > 1 && a.allPlain(calls) {
		results := make([]Message, len(calls))
		var wg sync.WaitGroup
		for i, tc := range calls {
			wg.Add(1)
			go func(i int, tc ToolCall) {
				defer wg.Done()
				results[i] = a.toolResult(ctx, tc)
			}(i, tc)
		}
		wg.Wait()
		for _, m := range results {
			a.fireHooks(step, m)
		}
		return results, nil, nil, nil
	}
	for _, tc := range calls {
		if target := a.handoffTarget(tc.Name); target != nil {
			// A handoff answers its call and puts the target in charge from
			// the next step; only the first one in a step counts.
			text := "Transferred to " + target.name + "."
			if handoff != nil {
				text = "Ignored: the conversation was already transferred to " + handoff.name + "."
			} else {
				handoff = target
			}
			m := Message{Role: RoleTool, Content: text, ToolCallID: tc.ID}
			msgs = append(msgs, m)
			a.fireHooks(step, m)
			continue
		}
		if tool := a.findTool(tc.Name); tool != nil && tool.NeedsApproval {
			if a.approval == nil {
				pending = append(pending, tc)
				continue
			}
			d, aErr := a.approval(ctx, ApprovalRequest{Call: tc, Tool: tool})
			if aErr != nil {
				return msgs, nil, nil, fmt.Errorf("ai: approval for %s: %w", tc.Name, aErr)
			}
			if !d.Approved {
				m := deniedResult(tc, d.Reason)
				msgs = append(msgs, m)
				a.fireHooks(step, m)
				continue
			}
		}
		m := a.toolResult(ctx, tc)
		msgs = append(msgs, m)
		a.fireHooks(step, m)
	}
	return msgs, pending, handoff, nil
}

// allPlain reports whether no call is a handoff or needs approval, so the
// calls can run concurrently.
func (a *Agent) allPlain(calls []ToolCall) bool {
	for _, tc := range calls {
		if a.handoffTarget(tc.Name) != nil {
			return false
		}
		if t := a.findTool(tc.Name); t != nil && t.NeedsApproval {
			return false
		}
	}
	return true
}

func (a *Agent) toolResult(ctx context.Context, tc ToolCall) Message {
	result, execErr := a.executeTool(ctx, tc)
	m := Message{Role: RoleTool, Content: string(result), ToolCallID: tc.ID}
	if execErr != nil {
		m.Content = fmt.Sprintf("Error: %v", execErr)
	}
	return m
}

func deniedResult(tc ToolCall, reason string) Message {
	text := "Error: the user did not approve this call to " + tc.Name + "."
	if strings.TrimSpace(reason) != "" {
		text += " Reason: " + reason
	}
	return Message{Role: RoleTool, Content: text, ToolCallID: tc.ID}
}

// pause saves a conversation that is waiting for approval.
func (a *Agent) pause(ctx context.Context, msgs []Message) {
	if a.memory != nil && a.memoryKey != "" {
		a.saveHistory(ctx, msgs)
		return
	}
	a.messages = append([]Message(nil), msgs...)
}

// pendingCalls returns the tool calls of the last assistant turn that have
// no result yet.
func pendingCalls(msgs []Message) []ToolCall {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != RoleAssistant {
			continue
		}
		answered := map[string]bool{}
		for _, m := range msgs[i+1:] {
			if m.Role == RoleTool {
				answered[m.ToolCallID] = true
			}
		}
		var out []ToolCall
		for _, tc := range msgs[i].ToolCalls {
			if !answered[tc.ID] {
				out = append(out, tc)
			}
		}
		return out
	}
	return nil
}

// Pending returns the tool calls waiting for approval in the agent's saved
// conversation.
func (a *Agent) Pending(ctx context.Context) ([]ToolCall, error) {
	msgs, err := a.loadHistory(ctx)
	if err != nil {
		return nil, err
	}
	return pendingCalls(msgs), nil
}

// Resume continues a conversation paused for approval: approved calls run,
// denied ones are reported to the model, and calls without a decision are
// denied. The agent then carries on as Prompt would.
func (a *Agent) Resume(ctx context.Context, decisions ...Decision) (*GenerateResponse, error) {
	msgs, err := a.loadHistory(ctx)
	if err != nil {
		return nil, err
	}
	waiting := pendingCalls(msgs)
	if len(waiting) == 0 {
		return nil, ErrNothingToResume
	}
	byID := map[string]Decision{}
	for _, d := range decisions {
		byID[d.CallID] = d
	}
	for _, tc := range waiting {
		d, ok := byID[tc.ID]
		switch {
		case ok && d.Approved:
			msgs = append(msgs, a.toolResult(ctx, tc))
		case ok:
			msgs = append(msgs, deniedResult(tc, d.Reason))
		default:
			msgs = append(msgs, deniedResult(tc, "no decision was given"))
		}
	}
	return a.loop(ctx, msgs, nil)
}
