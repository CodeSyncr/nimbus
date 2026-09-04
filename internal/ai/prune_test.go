package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

// smallTalk builds a conversation of many short messages.
func smallTalk(n int) []Message {
	msgs := []Message{{Role: "user", Content: "start"}}
	for i := 0; i < n; i++ {
		msgs = append(msgs, Message{Role: "assistant", Content: "ok"})
	}
	return msgs
}

// Pruning deletes history outright; compaction summarises it. Pruning at 120
// messages meant ordinary sessions lost work compaction was about to keep.
func TestPruningLeavesRoomForCompaction(t *testing.T) {
	s := NewSession("optimal")
	s.SetLimits(128000, 80)
	s.Messages = smallTalk(200)
	before := len(s.Messages)

	s.pruneMessages()

	if len(s.Messages) != before {
		t.Errorf("200 short messages were pruned (%d left) — compaction never got the chance",
			len(s.Messages))
	}
}

// Counting messages is the wrong measure: a few very large results can exceed
// the window while the count stays low.
func TestPruningIsDrivenBySizeNotJustCount(t *testing.T) {
	s := NewSession("optimal")
	s.SetLimits(20000, 80) // ceiling is 40000 tokens

	s.Messages = []Message{{Role: "user", Content: "read everything"}}
	for i := 0; i < 8; i++ {
		s.Messages = append(s.Messages, Message{Role: "assistant", Content: []ContentBlock{
			{Type: "tool_use", ID: "t", Name: "read_file", Input: map[string]any{"path": "big.go"}},
		}})
		s.Messages = append(s.Messages, Message{Role: "user", Content: []ContentBlock{
			{Type: "tool_result", ToolUseID: "t", Content: strings.Repeat("x", 40000)},
		}})
	}

	if len(s.Messages) > maxSessionMessages {
		t.Fatal("the fixture is over the message cap, which is not what this tests")
	}
	if !s.needsPruning() {
		t.Fatal("a conversation twice the window was not judged to need pruning")
	}

	s.pruneMessages()

	if got := EstimateTokens(s.Messages); got > s.pruneCeiling() {
		t.Errorf("after pruning the conversation is %d tokens, ceiling is %d", got, s.pruneCeiling())
	}
	if len(s.Messages) < 2 {
		t.Error("pruning emptied the conversation")
	}
	if !strings.Contains(firstUserText(s.Messages), "read everything") {
		t.Error("the opening request was lost")
	}
}

// A tool result must never be left without the call it answers.
func TestPruningNeverOrphansAToolResult(t *testing.T) {
	s := NewSession("optimal")
	s.SetLimits(8000, 80)
	s.Messages = []Message{{Role: "user", Content: "go"}}
	for i := 0; i < 20; i++ {
		s.Messages = append(s.Messages, Message{Role: "assistant", Content: []ContentBlock{
			{Type: "tool_use", ID: "t", Name: "grep", Input: map[string]any{"pattern": "x"}},
		}})
		s.Messages = append(s.Messages, Message{Role: "user", Content: []ContentBlock{
			{Type: "tool_result", ToolUseID: "t", Content: strings.Repeat("y", 6000)},
		}})
	}
	s.pruneMessages()

	// The first message after the trim notice must not be a bare result.
	for i, m := range s.Messages {
		if i <= keptLeadMessages {
			continue
		}
		if isToolResultMessage(m) && !isToolCallMessage(s.Messages[i-1]) {
			t.Fatalf("message %d is a tool result with no call before it", i)
		}
	}
}

func firstUserText(msgs []Message) string {
	for _, m := range msgs {
		if s, ok := m.Content.(string); ok {
			return s
		}
	}
	return ""
}

func isToolCallMessage(m Message) bool {
	blocks, ok := m.Content.([]ContentBlock)
	if !ok {
		return false
	}
	for _, b := range blocks {
		if b.Type == "tool_use" {
			return true
		}
	}
	return false
}

// A resumed session holds decoded JSON, not typed blocks. The estimator used
// to score every non-string value as 8 characters, so a nested tool result
// measured as almost nothing exactly when the session was longest.
func TestEstimatorMeasuresRestoredSessions(t *testing.T) {
	big := strings.Repeat("z", 20000)

	typed := []Message{{Role: "user", Content: []ContentBlock{
		{Type: "tool_result", ToolUseID: "t", Content: big},
	}}}

	// The same message after a save/load round trip.
	raw, err := json.Marshal(typed)
	if err != nil {
		t.Fatal(err)
	}
	var restored []Message
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}

	want := EstimateTokens(typed)
	got := EstimateTokens(restored)
	if got < want*9/10 {
		t.Errorf("restored session estimated at %d tokens, typed at %d — the estimate collapses on reload",
			got, want)
	}
}

// Nested structures are measured through, not counted as a constant.
func TestEstimatorWalksNestedValues(t *testing.T) {
	shallow := measureAny(map[string]any{"content": "short"})
	deep := measureAny(map[string]any{
		"content": []any{
			map[string]any{"text": strings.Repeat("a", 5000)},
		},
	})
	if deep <= shallow {
		t.Errorf("a nested 5000-character value measured %d, a short one %d", deep, shallow)
	}
	if deep < 4000 {
		t.Errorf("nested content measured %d, far below its real size", deep)
	}
}
