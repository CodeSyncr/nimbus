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
	"strings"
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
	skills       *SkillSet
	configErr    error
	promptCache  bool
	reasoning    *Reasoning
	approval     ApprovalFunc
	context      *contextPolicy
	name         string
	description  string
	handoffs     []*Agent
	parallel     bool
}

// AgentHook is called at each step of the agent's reasoning loop.
type AgentHook func(step int, msg Message)

// NewAgent creates an agent with system instructions.
func NewAgent(instructions string) *Agent {
	return &Agent{
		instructions: instructions,
		maxSteps:     10,
	}
}

// llm is the client the agent talks to: the one set with WithClient, else
// the global client (resolved when the agent runs, so an agent can be built
// before the AI plugin boots, or with only WithClient in tests).
func (a *Agent) llm() *Client {
	if a.client != nil {
		return a.client
	}
	return GetClient()
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

// WithSkills gives the agent skills: the system prompt lists them and the
// model loads one with the load_skill tool when a request matches (see
// skill.go). Several sets merge; a skill name may appear only once.
func (a *Agent) WithSkills(sets ...*SkillSet) *Agent {
	var all []Skill
	all = append(all, a.skills.Skills()...)
	for _, set := range sets {
		all = append(all, set.Skills()...)
	}
	merged, err := NewSkillSet(all...)
	if err != nil {
		a.configErr = err
		return a
	}
	a.skills = merged
	return a
}

// WithSkill adds skills built in code (see NewSkill).
func (a *Agent) WithSkill(skills ...Skill) *Agent {
	set, err := NewSkillSet(skills...)
	if err != nil {
		a.configErr = err
		return a
	}
	return a.WithSkills(set)
}

// WithPromptCache caches the agent's system prompt, tools and conversation
// between steps and turns (see WithPromptCache). Agents resend all three on
// every step, so this is where caching pays most.
func (a *Agent) WithPromptCache() *Agent {
	a.promptCache = true
	return a
}

// WithReasoning makes the agent think before each step ("low", "medium",
// "high"; see WithReasoning). Its thinking is kept with each turn, as
// providers require when tools are involved.
func (a *Agent) WithReasoning(effort string) *Agent {
	a.reasoning = &Reasoning{Effort: effort}
	return a
}

// WithParallelTools runs the tool calls of one step concurrently (results
// keep the model's order). Use it when tools are independent and safe to
// run at once, e.g. several lookups.
func (a *Agent) WithParallelTools() *Agent {
	a.parallel = true
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
	if a.configErr != nil {
		return nil, a.configErr
	}
	msgs, err := a.loadHistory(ctx)
	if err != nil {
		return nil, err
	}
	msgs = append(msgs, Message{Role: RoleUser, Content: userMessage})

	// Move any per-call images (WithImages) onto the current user message so
	// vision-capable providers can serialize them as multimodal content.
	if imgs, files := attachmentsFromOpts(opts); len(imgs)+len(files) > 0 {
		msgs[len(msgs)-1].Images, msgs[len(msgs)-1].Files = imgs, files
	}

	return a.loop(ctx, msgs, opts)
}

// loop runs model steps until a final answer, a pause for approval, or the
// step limit.
func (a *Agent) loop(ctx context.Context, msgs []Message, opts []GenerateOption) (*GenerateResponse, error) {
	// cur is the agent in charge: a, or whoever it handed off to. The
	// conversation (memory, history) stays a's.
	cur := a
	toolSpecs := cur.resolveToolSpecs()

	for step := 0; step < a.maxSteps; step++ {
		var err error
		if msgs, err = cur.fitContext(ctx, msgs, cur.fixedTokens(toolSpecs)); err != nil {
			return nil, err
		}
		req := cur.request(msgs, toolSpecs, opts)
		if step > 0 && forcesTool(req.ToolChoice) {
			req.ToolChoice = "" // forced once; then the model may answer
		}
		resp, err := cur.llm().GenerateRequest(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("ai: agent step %d: %w", step, err)
		}
		resp.Agent = cur.name

		assistantMsg := Message{
			Role:      RoleAssistant,
			Content:   resp.Text,
			ToolCalls: resp.ToolCalls,
			Reasoning: resp.ReasoningBlocks,
		}
		msgs = append(msgs, assistantMsg)
		a.fireHooks(step, assistantMsg)

		// If no tool calls, we have a final answer.
		if len(resp.ToolCalls) == 0 {
			a.saveHistory(ctx, msgs)
			return resp, nil
		}

		toolMsgs, pending, next, err := cur.runToolCalls(ctx, step, resp.ToolCalls)
		msgs = append(msgs, toolMsgs...)
		if err != nil {
			return nil, err
		}
		if len(pending) > 0 {
			a.pause(ctx, msgs)
			return resp, &ApprovalRequiredError{Pending: pending}
		}
		if next != nil {
			cur = next
			toolSpecs = cur.resolveToolSpecs()
		}
	}

	return nil, fmt.Errorf("ai: agent exceeded max steps (%d)", a.maxSteps)
}

// ---------------------------------------------------------------------------
// Stream — streaming with tool loop
// ---------------------------------------------------------------------------

// Stream runs the agent loop and streams the model's text as it arrives.
//
// When the provider reports tool calls while streaming (OpenAI and
// OpenAI-compatible providers), or the agent has no tools, every step is
// streamed. Otherwise tool steps run as ordinary requests and the final
// answer is delivered from that same response, so the model is never asked
// twice. Either way the final answer is saved to memory once the stream
// ends. Errors, including a failure before the first token, arrive on Err.
func (a *Agent) Stream(ctx context.Context, userMessage string, opts ...GenerateOption) (*StreamResponse, error) {
	if a.configErr != nil {
		return nil, a.configErr
	}
	msgs, err := a.loadHistory(ctx)
	if err != nil {
		return nil, err
	}
	msgs = append(msgs, Message{Role: RoleUser, Content: userMessage})
	if imgs, files := attachmentsFromOpts(opts); len(imgs)+len(files) > 0 {
		msgs[len(msgs)-1].Images, msgs[len(msgs)-1].Files = imgs, files
	}

	cur := a // the agent in charge, after any handoff
	toolSpecs := cur.resolveToolSpecs()
	streamSteps := len(toolSpecs) == 0 || providerStreamsToolCalls(cur.llm())

	out := make(chan StreamChunk, 32)
	errc := make(chan error, 1)
	send := func(c StreamChunk) bool {
		select {
		case out <- c:
			return true
		case <-ctx.Done():
			return false
		}
	}

	go func() {
		defer close(out)
		for step := 0; step < a.maxSteps; step++ {
			var fitErr error
			if msgs, fitErr = cur.fitContext(ctx, msgs, cur.fixedTokens(toolSpecs)); fitErr != nil {
				errc <- fitErr
				return
			}
			req := cur.request(msgs, toolSpecs, opts)
			if step > 0 && forcesTool(req.ToolChoice) {
				req.ToolChoice = ""
			}
			var (
				text      string
				calls     []ToolCall
				usage     *Usage
				reasoning []ReasoningBlock
			)
			if streamSteps {
				req.Stream = true
				st, err := cur.llm().StreamRequest(ctx, req)
				if err != nil {
					errc <- fmt.Errorf("ai: agent stream step %d: %w", step, err)
					return
				}
				for chunk := range st.Chunks {
					if chunk.Text != "" {
						text += chunk.Text
						if !send(StreamChunk{Text: chunk.Text}) {
							errc <- ctx.Err()
							return
						}
					}
					calls = append(calls, chunk.ToolCalls...)
					reasoning = append(reasoning, chunk.Reasoning...)
					if chunk.Usage != nil {
						usage = chunk.Usage
					}
				}
				select {
				case err := <-st.Err:
					if err != nil {
						errc <- fmt.Errorf("ai: agent stream step %d: %w", step, err)
						return
					}
				default:
				}
			} else {
				resp, err := cur.llm().GenerateRequest(ctx, req)
				if err != nil {
					errc <- fmt.Errorf("ai: agent stream step %d: %w", step, err)
					return
				}
				text, calls, usage, reasoning = resp.Text, resp.ToolCalls, resp.Usage, resp.ReasoningBlocks
				if len(calls) == 0 && text != "" && !send(StreamChunk{Text: text}) {
					errc <- ctx.Err()
					return
				}
			}

			assistantMsg := Message{Role: RoleAssistant, Content: text, ToolCalls: calls, Reasoning: reasoning}
			msgs = append(msgs, assistantMsg)
			a.fireHooks(step, assistantMsg)

			if len(calls) == 0 {
				a.saveHistory(ctx, msgs)
				send(StreamChunk{Usage: usage, Done: true})
				errc <- nil
				return
			}
			toolMsgs, pending, next, err := cur.runToolCalls(ctx, step, calls)
			msgs = append(msgs, toolMsgs...)
			if err != nil {
				errc <- err
				return
			}
			if len(pending) > 0 {
				a.pause(ctx, msgs)
				errc <- &ApprovalRequiredError{Pending: pending}
				return
			}
			if next != nil {
				cur = next
				toolSpecs = cur.resolveToolSpecs()
				streamSteps = len(toolSpecs) == 0 || providerStreamsToolCalls(cur.llm())
			}
		}
		errc <- fmt.Errorf("ai: agent exceeded max steps (%d)", a.maxSteps)
	}()

	return &StreamResponse{Chunks: out, Err: errc}, nil
}

// providerStreamsToolCalls reports whether the client's provider includes
// tool calls in its stream.
func providerStreamsToolCalls(c *Client) bool {
	if c == nil {
		return false
	}
	ts, ok := c.Provider().(ToolCallStreamer)
	return ok && ts.StreamsToolCalls()
}

// request builds one step's request: the agent's system prompt (with its
// skills), model, tools and the caller's options.
func (a *Agent) request(msgs []Message, toolSpecs []ToolSpec, opts []GenerateOption) *GenerateRequest {
	req := &GenerateRequest{
		Messages:  msgs,
		System:    a.systemPrompt(),
		Model:     a.model,
		MaxTokens: 4096,
		Tools:     toolSpecs,
		Cache:     a.promptCache,
		Reasoning: a.reasoning,
	}
	for _, opt := range opts {
		opt(req)
	}
	// The agent already put WithImages/WithFiles attachments on the user
	// message; leaving them on the request too would make the client attach
	// them a second time, on every step.
	req.Images, req.Files = nil, nil
	return req
}

// fixedTokens estimates the part of every request history cannot shrink:
// the system prompt and tool definitions.
func (a *Agent) fixedTokens(tools []ToolSpec) int {
	n := (len(a.systemPrompt()) + 3) / 4
	for _, t := range tools {
		n += (len(t.Name) + len(t.Description) + len(t.Parameters) + 3) / 4
	}
	return n
}

// systemPrompt is the agent's instructions plus the list of its skills.
func (a *Agent) systemPrompt() string {
	idx := skillIndex(a.skills)
	if idx == "" {
		return a.instructions
	}
	if strings.TrimSpace(a.instructions) == "" {
		return idx
	}
	return strings.TrimRight(a.instructions, "\n") + "\n\n" + idx
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

	if a.skills.Len() > 0 {
		specs = append(specs, skillTool(a.skills).ToSpec())
	}

	for _, h := range a.handoffs {
		specs = append(specs, handoffSpec(h))
	}

	return specs
}

func (a *Agent) executeTool(ctx context.Context, tc ToolCall) (json.RawMessage, error) {
	if tc.Name == SkillToolName && a.skills.Len() > 0 {
		return skillTool(a.skills).Execute(ctx, tc.Args)
	}
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

// attachmentsFromOpts applies the given options to a throwaway request purely to
// extract the images and files set via WithImages and WithFiles, without disturbing the per-step
// request configuration.
func attachmentsFromOpts(opts []GenerateOption) (images, files []string) {
	probe := &GenerateRequest{}
	for _, opt := range opts {
		opt(probe)
	}
	return probe.Images, probe.Files
}
