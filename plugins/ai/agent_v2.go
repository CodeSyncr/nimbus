/*
|--------------------------------------------------------------------------
| AI SDK — Agent Runtime (v2)
|--------------------------------------------------------------------------
|
| An Agent is an AI entity with instructions, tools, memory,
| and an autonomous reasoning loop. When the model returns tool
| calls the agent executes them and feeds results back — repeating
| until the model produces a final text answer or the step limit
| is reached.
|
| Usage:
|
|   agent := ai.NewAgent("You are a Go expert").
|       WithTools("weather", "calculator").
|       WithMemory("session:abc").
|       MaxSteps(10)
|
|   resp, err := agent.Prompt(ctx, "What is 2+2 and the weather in NYC?")
|
|   // Streaming
|   stream, err := agent.Stream(ctx, "Write a haiku about Go")
|   for chunk := range stream.Chunks { ... }
|
*/

package ai

import (
	"context"
	"encoding/json"
	"fmt"
)

// ---------------------------------------------------------------------------
// Agent
// ---------------------------------------------------------------------------

// Agent is an AI entity with instructions, tools, and optional
// conversational memory.
type Agent struct {
	instructions string
	tools        []*Tool
	toolNames    []string
	memory       Memory
	memoryKey    string
	messages     []Message
	model        string
	maxSteps     int
	client       *Client
	hooks        []AgentHook
}

// AgentHook is called at each step of the agent's reasoning loop.
type AgentHook func(step int, msg Message)

// NewAgent creates an agent with system instructions.
func NewAgent(instructions string) *Agent {
	return &Agent{
		instructions: instructions,
		maxSteps:     10,
		client:       GetClient(),
	}
}

// WithClient overrides the default global client.
func (a *Agent) WithClient(c *Client) *Agent {
	a.client = c
	return a
}

// WithModel overrides the provider's default model for this agent.
func (a *Agent) WithModel(model string) *Agent {
	a.model = model
	return a
}

// WithTools attaches named tools (from the global registry).
func (a *Agent) WithTools(names ...string) *Agent {
	a.toolNames = append(a.toolNames, names...)
	return a
}

// WithToolObjects attaches tool instances directly.
func (a *Agent) WithToolObjects(tools ...*Tool) *Agent {
	a.tools = append(a.tools, tools...)
	return a
}

// WithMemory enables persistent memory backed by the given Memory
// implementation. The key scopes the conversation (e.g. session ID).
func (a *Agent) WithMemory(m Memory, key string) *Agent {
	a.memory = m
	a.memoryKey = key
	return a
}

// WithMessages sets initial conversation history directly.
func (a *Agent) WithMessages(msgs []Message) *Agent {
	a.messages = msgs
	return a
}

// MaxSteps limits the number of tool-call → result round-trips.
func (a *Agent) MaxSteps(n int) *Agent {
	a.maxSteps = n
	return a
}

// OnStep registers a hook called at each reasoning step.
func (a *Agent) OnStep(h AgentHook) *Agent {
	a.hooks = append(a.hooks, h)
	return a
}

// ---------------------------------------------------------------------------
// Prompt — synchronous reasoning loop
// ---------------------------------------------------------------------------

// Prompt sends a user message and runs the full reasoning loop
// (tool-call → execute → feed-back) until the model produces a
// text answer or maxSteps is exhausted.
func (a *Agent) Prompt(ctx context.Context, userMessage string, opts ...GenerateOption) (*GenerateResponse, error) {
	msgs, err := a.loadHistory(ctx)
	if err != nil {
		return nil, err
	}
	msgs = append(msgs, Message{Role: RoleUser, Content: userMessage})

	toolSpecs := a.resolveToolSpecs()

	for step := 0; step < a.maxSteps; step++ {
		req := &GenerateRequest{
			Messages:  msgs,
			System:    a.instructions,
			Model:     a.model,
			MaxTokens: 4096,
			Tools:     toolSpecs,
		}
		for _, opt := range opts {
			opt(req)
		}

		resp, err := a.client.GenerateRequest(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("ai: agent step %d: %w", step, err)
		}

		assistantMsg := Message{
			Role:      RoleAssistant,
			Content:   resp.Text,
			ToolCalls: resp.ToolCalls,
		}
		msgs = append(msgs, assistantMsg)
		a.fireHooks(step, assistantMsg)

		// If no tool calls, we have a final answer.
		if len(resp.ToolCalls) == 0 {
			a.saveHistory(ctx, msgs)
			return resp, nil
		}

		// Execute each tool call and feed results back.
		for _, tc := range resp.ToolCalls {
			result, execErr := a.executeTool(ctx, tc)
			toolMsg := Message{
				Role:       RoleTool,
				Content:    string(result),
				ToolCallID: tc.ID,
			}
			if execErr != nil {
				toolMsg.Content = fmt.Sprintf("Error: %v", execErr)
			}
			msgs = append(msgs, toolMsg)
			a.fireHooks(step, toolMsg)
		}
	}

	return nil, fmt.Errorf("ai: agent exceeded max steps (%d)", a.maxSteps)
}

// ---------------------------------------------------------------------------
// Stream — streaming with tool loop
// ---------------------------------------------------------------------------

// Stream runs the agent loop but streams the final text response.
// Intermediate tool-call steps are executed internally; only the
// final answer is streamed to the caller.
func (a *Agent) Stream(ctx context.Context, userMessage string, opts ...GenerateOption) (*StreamResponse, error) {
	msgs, err := a.loadHistory(ctx)
	if err != nil {
		return nil, err
	}
	msgs = append(msgs, Message{Role: RoleUser, Content: userMessage})

	toolSpecs := a.resolveToolSpecs()

	// Run tool loop synchronously until no more tool calls.
	for step := 0; step < a.maxSteps; step++ {
		req := &GenerateRequest{
			Messages:  msgs,
			System:    a.instructions,
			Model:     a.model,
			MaxTokens: 4096,
			Tools:     toolSpecs,
		}
		for _, opt := range opts {
			opt(req)
		}

		// Non-streaming call to check for tool calls.
		resp, err := a.client.GenerateRequest(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("ai: agent stream step %d: %w", step, err)
		}

		if len(resp.ToolCalls) == 0 {
			// No tools — re-issue as a stream for the final answer.
			break
		}

		assistantMsg := Message{
			Role:      RoleAssistant,
			Content:   resp.Text,
			ToolCalls: resp.ToolCalls,
		}
		msgs = append(msgs, assistantMsg)
		a.fireHooks(step, assistantMsg)

		for _, tc := range resp.ToolCalls {
			result, execErr := a.executeTool(ctx, tc)
			toolMsg := Message{
				Role:       RoleTool,
				Content:    string(result),
				ToolCallID: tc.ID,
			}
			if execErr != nil {
				toolMsg.Content = fmt.Sprintf("Error: %v", execErr)
			}
			msgs = append(msgs, toolMsg)
		}
	}

	// Final streaming call (no tools in spec so model produces text).
	req := &GenerateRequest{
		Messages:  msgs,
		System:    a.instructions,
		Model:     a.model,
		MaxTokens: 4096,
		Stream:    true,
	}
	for _, opt := range opts {
		opt(req)
	}

	stream, err := a.client.StreamRequest(ctx, req)
	if err != nil {
		return nil, err
	}

	a.saveHistory(ctx, msgs)
	return stream, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func (a *Agent) resolveToolSpecs() []ToolSpec {
	var specs []ToolSpec

	// Tools from global registry by name.
	for _, name := range a.toolNames {
		if t, ok := GetTool(name); ok {
			specs = append(specs, t.ToSpec())
		}
	}

	// Directly attached tools.
	for _, t := range a.tools {
		specs = append(specs, t.ToSpec())
	}

	return specs
}

func (a *Agent) executeTool(ctx context.Context, tc ToolCall) (json.RawMessage, error) {
	// First check direct tool objects.
	for _, t := range a.tools {
		if t.Name == tc.Name {
			return t.Execute(ctx, tc.Args)
		}
	}
	// Then check global registry.
	return ExecuteTool(ctx, tc.Name, tc.Args)
}

func (a *Agent) loadHistory(ctx context.Context) ([]Message, error) {
	if a.memory != nil && a.memoryKey != "" {
		history, err := a.memory.Load(ctx, a.memoryKey)
		if err != nil {
			return nil, fmt.Errorf("ai: load memory: %w", err)
		}
		return history, nil
	}
	if len(a.messages) > 0 {
		out := make([]Message, len(a.messages))
		copy(out, a.messages)
		return out, nil
	}
	return nil, nil
}

func (a *Agent) saveHistory(ctx context.Context, msgs []Message) {
	if a.memory != nil && a.memoryKey != "" {
		_ = a.memory.Save(ctx, a.memoryKey, msgs)
	}
}

func (a *Agent) fireHooks(step int, msg Message) {
	for _, h := range a.hooks {
		h(step, msg)
	}
}
