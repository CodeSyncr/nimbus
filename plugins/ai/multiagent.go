/*
|--------------------------------------------------------------------------
| AI SDK — Multi-agent: agents as tools, and handoffs
|--------------------------------------------------------------------------
|
| Two ways for agents to work together:
|
|   // 1. A specialist the main agent consults and gets an answer back from.
|   researcher := ai.NewAgent("You research facts and cite sources.").WithTools("web_search")
|   writer := ai.NewAgent("You write the reply.").
|       WithToolObjects(researcher.AsTool("research", "Look up facts for the reply"))
|
|   // 2. A handoff: the conversation moves to the specialist, who answers
|   //    the user from then on with its own instructions, tools and model.
|   billing := ai.NewAgent("You handle invoices and refunds.").Named("billing", "Invoices, refunds, payment problems")
|   support := ai.NewAgent("Triage the request.").WithHandoffs(billing, tech)
|   resp, _ := support.Prompt(ctx, msg)   // resp.Agent == "billing" if it handed off
|
| A handoff keeps the conversation (and the first agent's memory); the
| target's own handoffs carry on from there.
|
*/

package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Named gives the agent a name (used for handoffs and in responses) and a
// description of what it handles (shown to agents that can hand off to it).
func (a *Agent) Named(name, description string) *Agent {
	a.name, a.description = name, description
	return a
}

// Name returns the agent's name.
func (a *Agent) Name() string { return a.name }

// WithHandoffs lets the agent transfer the conversation to these agents,
// which must be Named. Each becomes a transfer_to_<name> tool.
func (a *Agent) WithHandoffs(agents ...*Agent) *Agent {
	for _, h := range agents {
		if h == nil {
			continue
		}
		if h.name == "" {
			a.configErr = fmt.Errorf("ai: a handoff target needs Named(name, description)")
			continue
		}
		a.handoffs = append(a.handoffs, h)
	}
	return a
}

var handoffNameUnsafe = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

func handoffToolName(h *Agent) string {
	n := "transfer_to_" + strings.Trim(handoffNameUnsafe.ReplaceAllString(h.name, "_"), "_")
	if len(n) > 64 {
		n = n[:64]
	}
	return n
}

func handoffSpec(h *Agent) ToolSpec {
	desc := "Transfer the conversation to " + h.name + "."
	if h.description != "" {
		desc += " Use for: " + h.description
	}
	return ToolSpec{
		Name:        handoffToolName(h),
		Description: desc,
		Parameters:  json.RawMessage(`{"type":"object","properties":{"reason":{"type":"string","description":"Why the conversation is being transferred."}}}`),
	}
}

func (a *Agent) handoffTarget(toolName string) *Agent {
	for _, h := range a.handoffs {
		if handoffToolName(h) == toolName {
			return h
		}
	}
	return nil
}

// AsTool wraps the agent as a tool another agent can call: the caller
// passes a task, this agent works on it (with its own tools and a fresh
// conversation each time) and its answer is the tool's result.
func (a *Agent) AsTool(name, description string) *Tool {
	schema := json.RawMessage(`{"type":"object","properties":{"input":{"type":"string","description":"The task or question, with everything needed to do it."}},"required":["input"]}`)
	tool, err := NewRawTool(name, description, schema, func(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
		var in struct {
			Input string `json:"input"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, err
		}
		sub := *a // a fresh conversation for every call
		sub.memory, sub.memoryKey, sub.messages = nil, "", nil
		resp, err := sub.Prompt(ctx, in.Input)
		if err != nil {
			return nil, err
		}
		return json.Marshal(resp.Text)
	})
	if err != nil {
		panic(err) // the schema and handler above are fixed
	}
	return tool
}
