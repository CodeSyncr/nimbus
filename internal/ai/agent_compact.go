package ai

import (
	"context"
	"fmt"
	"strings"
)

/*
Compaction.

When a conversation approaches the context window something has to give. The
blunt option is to drop old messages, which is what pruneMessages does — the
work described in them disappears, and the model later re-reads files and
re-derives decisions it had already made.

Compaction replaces the older half with a written summary instead: what was
asked, what was decided, which files changed, what is still outstanding. The
conversation gets shorter while the knowledge in it survives.

The opening request and the most recent exchanges are always kept verbatim:
the first because it is the goal, the last because that is where the work is
happening right now.
*/

const (
	// keptOpeningMessages preserves the original request.
	keptOpeningMessages = 1
	// keptRecentMessages preserves the live end of the conversation. A tool
	// call and its result must not be separated, so the boundary is adjusted
	// when it lands between them.
	keptRecentMessages = 12
	// minCompactable is the smallest middle worth summarising.
	minCompactable = 6
	// keptIntactMessages is the live end of the tail, kept byte for byte
	// because it is where the current work is happening.
	keptIntactMessages = 4
	// maxRetainedResultChars caps a tool result kept behind the live end.
	// Twelve verbatim messages can hold several large reads, which is enough
	// to leave the tail alone over the threshold: compaction then reclaims
	// nothing and is attempted again on the next turn, for ever.
	maxRetainedResultChars = 2000
)

// CompactResult reports what compaction achieved.
type CompactResult struct {
	BeforeTokens int
	AfterTokens  int
	Summarised   int // messages replaced by the summary
}

// Saved is the estimated tokens reclaimed.
func (r CompactResult) Saved() int {
	if r.BeforeTokens > r.AfterTokens {
		return r.BeforeTokens - r.AfterTokens
	}
	return 0
}

// Compact summarises the older part of the conversation in place.
//
// It asks the model for the summary, so it costs one turn; when that fails the
// conversation is left untouched and the caller decides what to do, rather
// than silently losing history to a fallback.
func (a *Agent) Compact(ctx context.Context) (*CompactResult, error) {
	if a.Session == nil {
		return nil, fmt.Errorf("no session to compact")
	}

	messages := a.Session.Messages
	before := EstimateTokens(messages)

	start := keptOpeningMessages
	end := len(messages) - keptRecentMessages
	if end-start < minCompactable {
		return &CompactResult{BeforeTokens: before, AfterTokens: before}, nil
	}
	// Never cut between an assistant's tool call and its result.
	for end < len(messages) && isToolResultMessage(messages[end]) {
		end++
	}
	if end-start < minCompactable {
		return &CompactResult{BeforeTokens: before, AfterTokens: before}, nil
	}

	a.status("Compacting the conversation…")

	summary, err := a.summarise(ctx, messages[start:end])
	if err != nil {
		return nil, err
	}

	compacted := make([]Message, 0, keptOpeningMessages+1+len(messages)-end)
	compacted = append(compacted, messages[:start]...)
	compacted = append(compacted, Message{
		Role: "user",
		Content: "[Earlier in this session, summarised to save context]\n\n" + summary +
			"\n\nContinue from here. Do not re-investigate what is settled above.",
	})
	compacted = append(compacted, shrinkToolResults(messages[end:], keptIntactMessages, maxRetainedResultChars)...)

	a.Session.Messages = compacted
	a.saveSession()

	return &CompactResult{
		BeforeTokens: before,
		AfterTokens:  EstimateTokens(compacted),
		Summarised:   end - start,
	}, nil
}

// shrinkToolResults truncates tool results in the retained tail, leaving the
// most recent messages untouched.
//
// The tail is kept so the model can carry on without re-reading, but "carry
// on" needs the shape of what was found, not every byte of it — and a tail
// heavy enough to exceed the threshold by itself defeats compaction entirely.
// The newest messages are exempt: that is the work in progress.
func shrinkToolResults(messages []Message, keepIntact, max int) []Message {
	out := make([]Message, len(messages))
	copy(out, messages)

	cut := len(out) - keepIntact
	for i := 0; i < cut; i++ {
		switch blocks := out[i].Content.(type) {
		case []ContentBlock:
			shrunk := make([]ContentBlock, len(blocks))
			copy(shrunk, blocks)
			for j, b := range shrunk {
				if b.Type == "tool_result" && len(b.Content) > max {
					shrunk[j].Content = truncateOutput(b.Content, max)
				}
			}
			out[i] = Message{Role: out[i].Role, Content: shrunk}
		case []any:
			// A session restored from disk holds decoded JSON rather than
			// typed blocks, and those are exactly the sessions long enough to
			// need this.
			for _, raw := range blocks {
				block, ok := raw.(map[string]any)
				if !ok || block["type"] != "tool_result" {
					continue
				}
				if s, ok := block["content"].(string); ok && len(s) > max {
					block["content"] = truncateOutput(s, max)
				}
			}
		}
	}
	return out
}

// summarise asks the model to condense a stretch of conversation.
func (a *Agent) summarise(ctx context.Context, messages []Message) (string, error) {
	transcript := renderTranscript(messages)
	if strings.TrimSpace(transcript) == "" {
		return "", fmt.Errorf("nothing to summarise")
	}

	req := []Message{{Role: "user", Content: `Summarise the following stretch of an engineering session so work can continue from the summary alone.

Write it as dense notes under these headings, and omit a heading with nothing under it:

GOAL — what the user is trying to achieve
DECISIONS — choices made and the reason, including anything the user corrected
CHANGED — files created, edited or deleted, and what changed in each
LEARNED — facts about the codebase discovered by reading it (paths, structures, conventions) that would otherwise have to be re-derived
OPEN — what is unfinished, failing, or waiting on a decision

Be specific: real paths, real names, real values. Do not editorialise, do not restate the instructions, and do not invent anything that is not in the transcript.

--- TRANSCRIPT ---
` + transcript}}

	resp, err := a.turn(ctx, TurnModeChat, req, nil, nil)
	if err != nil {
		return "", fmt.Errorf("could not summarise the conversation: %w", err)
	}
	summary := strings.TrimSpace(resp.TextContent())
	if summary == "" {
		return "", fmt.Errorf("the summary came back empty")
	}
	return summary, nil
}

// renderTranscript flattens messages into text for summarising, keeping tool
// activity visible but compressing its output — what matters is which files
// were touched and what was found, not every byte that came back.
func renderTranscript(messages []Message) string {
	var sb strings.Builder
	for _, m := range messages {
		switch content := m.Content.(type) {
		case string:
			sb.WriteString(strings.ToUpper(m.Role) + ": " + content + "\n\n")
		case []ContentBlock:
			for _, b := range content {
				switch b.Type {
				case "text":
					if strings.TrimSpace(b.Text) != "" {
						sb.WriteString("ASSISTANT: " + b.Text + "\n\n")
					}
				case "tool_use":
					sb.WriteString(fmt.Sprintf("TOOL %s(%s)\n", b.Name, toolArgsSummary(b.Input)))
				case "tool_result":
					sb.WriteString("RESULT: " + truncateOutput(b.Content, 400) + "\n\n")
				}
			}
		}
	}
	return sb.String()
}

// toolArgsSummary renders the identifying argument of a tool call.
func toolArgsSummary(input map[string]any) string {
	for _, key := range []string{"path", "command", "pattern", "url", "skill_name"} {
		if v, ok := input[key].(string); ok && v != "" {
			return truncateOutput(v, 120)
		}
	}
	return ""
}
