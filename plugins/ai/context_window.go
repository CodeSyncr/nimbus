/*
|--------------------------------------------------------------------------
| AI SDK — Context window management
|--------------------------------------------------------------------------
|
| A conversation in memory grows with every turn until it no longer fits
| the model. WithContextLimit keeps an agent's prompt under a token budget
| by dropping the oldest turns, or by summarising them:
|
|   agent.WithContextLimit(60_000)                 // drop old turns
|   agent.WithContextLimit(60_000, ai.SummarizeOlder()) // fold them into a summary
|
| History is only ever cut before a user message, so a tool call is never
| separated from its result. The compacted history is what gets saved to
| memory, so the next turn starts from it.
|
| Token counts are estimated (about four characters per token, plus a flat
| cost per image) so no tokenizer is needed; leave headroom below the
| model's real limit.
|
*/

package ai

import (
	"context"
	"fmt"
	"strings"
)

// imageTokenEstimate is what an attached image is assumed to cost.
const imageTokenEstimate = 1000

// EstimateTokens roughly counts the tokens of a conversation.
func EstimateTokens(msgs []Message) int {
	n := 0
	for _, m := range msgs {
		n += 4 + (len(m.Content)+3)/4 + len(m.Images)*imageTokenEstimate
		for _, tc := range m.ToolCalls {
			n += 4 + (len(tc.Name)+len(tc.Args)+3)/4
		}
		for _, r := range m.Reasoning {
			n += (len(r.Text) + 3) / 4
		}
	}
	return n
}

// ContextOption configures WithContextLimit.
type ContextOption func(*contextPolicy)

type contextPolicy struct {
	maxTokens  int
	summarize  bool
	keepRecent int
}

// SummarizeOlder makes WithContextLimit fold the turns it removes into a short
// summary (written by the agent's own model) instead of dropping them.
func SummarizeOlder() ContextOption { return func(p *contextPolicy) { p.summarize = true } }

// KeepRecent keeps at least the last n messages untouched (default 6).
func KeepRecent(n int) ContextOption { return func(p *contextPolicy) { p.keepRecent = n } }

// WithContextLimit keeps the agent's prompt (system prompt, tools and
// history) under maxTokens, dropping or summarising the oldest turns.
func (a *Agent) WithContextLimit(maxTokens int, opts ...ContextOption) *Agent {
	p := &contextPolicy{maxTokens: maxTokens, keepRecent: 6}
	for _, o := range opts {
		o(p)
	}
	a.context = p
	return a
}

// summaryPrefix marks a message that summarises removed turns.
const summaryPrefix = "Summary of the earlier conversation:\n"

// fitContext returns msgs trimmed to the agent's budget.
func (a *Agent) fitContext(ctx context.Context, msgs []Message, fixedTokens int) ([]Message, error) {
	p := a.context
	if p == nil || p.maxTokens <= 0 {
		return msgs, nil
	}
	budget := p.maxTokens - fixedTokens
	if EstimateTokens(msgs) <= budget {
		return msgs, nil
	}

	// A previous summary stays at the front and is folded into a new one.
	var prior string
	body := msgs
	if len(body) > 0 && body[0].Role == RoleSystem && strings.HasPrefix(body[0].Content, summaryPrefix) {
		prior = strings.TrimPrefix(body[0].Content, summaryPrefix)
		body = body[1:]
	}

	// Cut points: indexes of user messages, so tool results stay with
	// their calls. Never cut into the last keepRecent messages, and always
	// keep the latest user message.
	limit := len(body) - p.keepRecent
	var cuts []int
	for i := 1; i < len(body); i++ {
		if body[i].Role == RoleUser && i <= limit {
			cuts = append(cuts, i)
		}
	}
	if lastUser := lastUserIndex(body); lastUser > 0 && (len(cuts) == 0 || cuts[len(cuts)-1] < lastUser) && lastUser > limit {
		cuts = append(cuts, lastUser)
	}
	if len(cuts) == 0 {
		return msgs, nil // nothing safe to remove
	}

	cut := cuts[len(cuts)-1]
	for _, c := range cuts {
		if EstimateTokens(body[c:])+summaryAllowance(p) <= budget {
			cut = c
			break
		}
	}
	removed, kept := body[:cut], body[cut:]

	if !p.summarize {
		return append([]Message(nil), kept...), nil
	}
	summary, err := a.summarize(ctx, prior, removed)
	if err != nil {
		return nil, fmt.Errorf("ai: summarise earlier conversation: %w", err)
	}
	out := make([]Message, 0, len(kept)+1)
	out = append(out, Message{Role: RoleSystem, Content: summaryPrefix + summary})
	return append(out, kept...), nil
}

func summaryAllowance(p *contextPolicy) int {
	if p.summarize {
		return 400
	}
	return 0
}

func lastUserIndex(msgs []Message) int {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == RoleUser {
			return i
		}
	}
	return -1
}

// summarize asks the agent's model for a summary of removed turns.
func (a *Agent) summarize(ctx context.Context, prior string, removed []Message) (string, error) {
	var b strings.Builder
	if prior != "" {
		b.WriteString("Earlier summary:\n" + prior + "\n\n")
	}
	b.WriteString("Conversation:\n")
	for _, m := range removed {
		switch {
		case m.Role == RoleTool:
			b.WriteString("tool result: " + truncateText(m.Content, 2000) + "\n")
		case len(m.ToolCalls) > 0:
			for _, tc := range m.ToolCalls {
				b.WriteString("assistant called " + tc.Name + "(" + truncateText(string(tc.Args), 500) + ")\n")
			}
			if m.Content != "" {
				b.WriteString(m.Role + ": " + m.Content + "\n")
			}
		default:
			b.WriteString(m.Role + ": " + m.Content + "\n")
		}
	}
	resp, err := a.llm().GenerateRequest(ctx, &GenerateRequest{
		Model:     a.model,
		MaxTokens: 600,
		System:    "Summarise this conversation for the assistant who will continue it. Keep facts, names, numbers, decisions, open questions and what tools returned. Write plain sentences, under 250 words.",
		Messages:  []Message{{Role: RoleUser, Content: b.String()}},
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.Text), nil
}

func truncateText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
