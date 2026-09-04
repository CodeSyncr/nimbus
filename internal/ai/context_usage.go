package ai

import (
	"os"
	"strconv"
	"strings"
)

/*
How full the conversation is.

A long session eventually exceeds what the model will accept. Until now the
only defence was pruneMessages, which drops messages out of the middle: cheap,
but the work described in them is simply gone, and nothing tells the user it
happened.

This measures the conversation so the UI can show how much room is left, and
gives compaction a threshold to fire on (see agent_compact.go).

Token counts are estimated rather than measured. A real tokeniser is
model-specific and the CLI talks to whatever the server is configured with, so
the estimate is deliberately conservative: it is used to decide when to
summarise, and being early costs a summary while being late costs the turn.
*/

// contextLimitEnv overrides the assumed context window.
const contextLimitEnv = "NIMBUS_CONTEXT_LIMIT"

// defaultContextLimit is the window assumed when nothing says otherwise. It is
// intentionally modest: overshooting means requests fail, undershooting only
// means compacting sooner than strictly necessary.
const defaultContextLimit = 128000

// DefaultCompactPercent is how full the window gets before the conversation
// is compacted automatically, when the setting says nothing.
const DefaultCompactPercent = 80

// ContextUsage describes how much of the window a conversation occupies.
type ContextUsage struct {
	Tokens int
	Limit  int
	// ThresholdPercent is how full the window gets before compaction runs.
	// Zero means the default, so a usage built by hand still behaves.
	ThresholdPercent int
}

// Threshold is the configured compaction point, as a percentage.
func (u ContextUsage) Threshold() int {
	if u.ThresholdPercent <= 0 {
		return DefaultCompactPercent
	}
	return u.ThresholdPercent
}

// Percent is how full the window is, 0–100.
func (u ContextUsage) Percent() int {
	if u.Limit <= 0 {
		return 0
	}
	p := u.Tokens * 100 / u.Limit
	if p > 100 {
		return 100
	}
	return p
}

// Remaining is the estimated room left, never negative.
func (u ContextUsage) Remaining() int {
	if r := u.Limit - u.Tokens; r > 0 {
		return r
	}
	return 0
}

// NeedsCompaction reports whether the conversation has passed the threshold.
//
// The comparison is in integers scaled by 100 rather than in floats: the
// threshold is a whole percentage from the settings screen, and this way it
// means exactly what it says at the boundary.
func (u ContextUsage) NeedsCompaction() bool {
	return u.Limit > 0 && u.Tokens*100 >= u.Limit*u.Threshold()
}

// ContextLimit returns the assumed context window when no session says
// otherwise. Settings reach a session through Session.SetLimits.
func ContextLimit() int {
	if v := strings.TrimSpace(os.Getenv(contextLimitEnv)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return defaultContextLimit
}

// SetLimits applies the configured window and compaction point.
//
// They are held on the session rather than read from a package-level global so
// that two agents in one process — a test suite, a future subagent — do not
// have to agree about them.
func (s *Session) SetLimits(limitTokens, compactPercent int) {
	s.limitTokens = limitTokens
	s.compactPercent = compactPercent
}

// ContextUsage estimates how much of the window this session occupies.
func (s *Session) ContextUsage() ContextUsage {
	limit := s.limitTokens
	if limit <= 0 {
		limit = ContextLimit()
	}
	return ContextUsage{
		Tokens:           EstimateTokens(s.Messages) + s.ContextOverhead,
		Limit:            limit,
		ThresholdPercent: s.compactPercent,
	}
}

// RecordContextTokens calibrates the estimate against what the server counted.
//
// EstimateTokens can only see s.Messages, but a request also carries the
// system prompt, every tool schema and the project context — none of it
// visible here, and easily thousands of tokens. The gap between the server's
// input count and the estimate for the messages that produced it is exactly
// that overhead, so recording it makes every later reading honest.
//
// Keeping the overhead separate rather than caching the absolute count is what
// makes the number survive compaction: the message estimate drops immediately,
// the overhead does not, and the gauge is right before the next turn rather
// than after it.
func (s *Session) RecordContextTokens(inputTokens int) {
	if inputTokens <= 0 {
		return
	}
	overhead := inputTokens - EstimateTokens(s.Messages)
	if overhead < 0 {
		// The estimate ran ahead of the real count; nothing to attribute.
		overhead = 0
	}
	s.ContextOverhead = overhead
}

// EstimateTokens approximates the tokens a conversation occupies.
//
// Four characters per token is the usual rule of thumb for English prose and
// code; tool results skew longer, which the estimate absorbs by counting every
// character of them too.
func EstimateTokens(messages []Message) int {
	chars := 0
	for _, m := range messages {
		chars += len(m.Role) + 4 // per-message overhead
		chars += contentChars(m.Content)
	}
	return chars / 4
}

// contentChars counts the characters of a message body in any of its shapes.
func contentChars(content any) int {
	switch v := content.(type) {
	case string:
		return len(v)
	case []ContentBlock:
		n := 0
		for _, b := range v {
			n += len(b.Text) + len(b.Content) + len(b.Name)
			for k, val := range b.Input {
				n += len(k)
				if s, ok := val.(string); ok {
					n += len(s)
				} else {
					n += 8
				}
			}
		}
		return n
	case []any:
		// The shape a session takes once it has been through JSON, which is
		// every resumed session — so this branch decides the estimate exactly
		// when the conversation is longest.
		return measureAny(v)
	}
	return 0
}

// measureAny counts the characters a decoded JSON value occupies.
//
// The previous version walked one level and scored anything that was not a
// string as 8 characters. A tool result whose content came back as a nested
// array therefore measured 8 instead of thousands, and the fuller the session
// the further the estimate drifted below the truth.
func measureAny(v any) int {
	switch t := v.(type) {
	case string:
		return len(t)
	case map[string]any:
		n := 0
		for k, val := range t {
			n += len(k) + measureAny(val)
		}
		return n
	case []any:
		n := 0
		for _, item := range t {
			n += measureAny(item)
		}
		return n
	case bool:
		return 5
	case nil:
		return 4
	case float64:
		return len(strconv.FormatFloat(t, 'g', -1, 64))
	default:
		return 8
	}
}
