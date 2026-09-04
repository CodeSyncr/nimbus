package ai

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func longConversation(n int) []Message {
	msgs := []Message{{Role: "user", Content: "build a checkout flow"}}
	for i := 0; i < n; i++ {
		msgs = append(msgs, Message{Role: "assistant", Content: []ContentBlock{
			{Type: "tool_use", ID: fmt.Sprintf("t%d", i), Name: "read_file",
				Input: map[string]any{"path": fmt.Sprintf("app/file%d.go", i)}},
		}})
		msgs = append(msgs, Message{Role: "user", Content: []ContentBlock{
			{Type: "tool_result", ToolUseID: fmt.Sprintf("t%d", i), Content: strings.Repeat("source line\n", 50)},
		}})
	}
	return msgs
}

func TestContextUsageTracksTheConversation(t *testing.T) {
	s := NewSession("optimal")
	if u := s.ContextUsage(); u.Tokens != 0 || u.Percent() != 0 {
		t.Errorf("an empty session should be at 0%%, got %+v", u)
	}

	s.Messages = longConversation(20)
	u := s.ContextUsage()
	if u.Tokens == 0 {
		t.Fatal("a full conversation measured as empty")
	}
	if u.Limit != ContextLimit() {
		t.Errorf("limit = %d, want %d", u.Limit, ContextLimit())
	}
	if u.Percent() < 0 || u.Percent() > 100 {
		t.Errorf("percent out of range: %d", u.Percent())
	}
	if u.Remaining() != u.Limit-u.Tokens {
		t.Errorf("remaining = %d", u.Remaining())
	}
}

func TestNeedsCompactionAtTheThreshold(t *testing.T) {
	limit := ContextLimit()
	below := ContextUsage{Tokens: int(float64(limit) * 0.5), Limit: limit}
	above := ContextUsage{Tokens: int(float64(limit) * 0.9), Limit: limit}

	if below.NeedsCompaction() {
		t.Error("half full should not trigger compaction")
	}
	if !above.NeedsCompaction() {
		t.Error("90% full should trigger compaction")
	}
}

// Compaction replaces the middle with a summary, keeping the original request
// and the live end of the conversation.
func TestCompactSummarisesTheMiddle(t *testing.T) {
	client := &scriptedTurnClient{
		responses: map[TurnMode][]*MessageResponse{
			TurnModeChat: {text("GOAL — build a checkout flow\nCHANGED — app/file1.go\nOPEN — tests")},
		},
	}
	agent := NewAgent(client, NewToolExecutor(t.TempDir()), &ProjectContext{AppRoot: t.TempDir()}, NewSession("optimal"))
	agent.Session.Messages = longConversation(20)
	originalLen := len(agent.Session.Messages)

	res, err := agent.Compact(context.Background())
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if res.Summarised == 0 {
		t.Fatal("nothing was summarised")
	}
	if res.Saved() <= 0 {
		t.Errorf("compaction did not reclaim tokens: %+v", res)
	}
	if len(agent.Session.Messages) >= originalLen {
		t.Errorf("conversation did not shrink: %d -> %d", originalLen, len(agent.Session.Messages))
	}

	// The opening request survives verbatim.
	if first, _ := agent.Session.Messages[0].Content.(string); !strings.Contains(first, "build a checkout flow") {
		t.Errorf("the original request was lost: %v", agent.Session.Messages[0].Content)
	}
	// The summary is present and marked as such.
	var sawSummary bool
	for _, m := range agent.Session.Messages {
		if s, ok := m.Content.(string); ok && strings.Contains(s, "summarised to save context") {
			sawSummary = true
			if !strings.Contains(s, "GOAL") {
				t.Errorf("the summary body is missing: %q", s)
			}
		}
	}
	if !sawSummary {
		t.Error("no summary was inserted")
	}
}

// A short conversation is left alone rather than summarised into nothing.
func TestCompactLeavesShortConversationsAlone(t *testing.T) {
	client := &scriptedTurnClient{}
	agent := NewAgent(client, NewToolExecutor(t.TempDir()), &ProjectContext{AppRoot: t.TempDir()}, NewSession("optimal"))
	agent.Session.AppendUser("hello")
	agent.Session.AppendAssistant([]ContentBlock{{Type: "text", Text: "hi"}})

	res, err := agent.Compact(context.Background())
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if res.Summarised != 0 {
		t.Errorf("a short conversation was compacted: %+v", res)
	}
	if len(agent.Session.Messages) != 2 {
		t.Errorf("messages changed: %d", len(agent.Session.Messages))
	}
}

// If the summary call fails the conversation must be left intact rather than
// silently losing history to a fallback.
func TestCompactFailureLeavesHistoryIntact(t *testing.T) {
	agent := NewAgent(&failingClient{}, NewToolExecutor(t.TempDir()), &ProjectContext{AppRoot: t.TempDir()}, NewSession("optimal"))
	agent.Session.Messages = longConversation(20)
	before := len(agent.Session.Messages)

	if _, err := agent.Compact(context.Background()); err == nil {
		t.Fatal("expected an error when summarising fails")
	}
	if len(agent.Session.Messages) != before {
		t.Errorf("history changed despite the failure: %d -> %d", before, len(agent.Session.Messages))
	}
}

type failingClient struct{ mockAIClient }

func (f *failingClient) Turn(ctx context.Context, req *TurnRequest, onDelta StreamHandler) (*MessageResponse, error) {
	return nil, fmt.Errorf("provider unavailable")
}

// Compaction must not cut between a tool call and the result that answers it.
func TestCompactKeepsToolCallsWithTheirResults(t *testing.T) {
	client := &scriptedTurnClient{
		responses: map[TurnMode][]*MessageResponse{TurnModeChat: {text("summary")}},
	}
	agent := NewAgent(client, NewToolExecutor(t.TempDir()), &ProjectContext{AppRoot: t.TempDir()}, NewSession("optimal"))
	agent.Session.Messages = longConversation(20)

	if _, err := agent.Compact(context.Background()); err != nil {
		t.Fatal(err)
	}
	for i, m := range agent.Session.Messages {
		if !isToolResultMessage(m) {
			continue
		}
		if i == 0 {
			t.Fatal("the conversation opens with an orphaned tool result")
		}
		prev := agent.Session.Messages[i-1]
		blocks, ok := prev.Content.([]ContentBlock)
		if !ok {
			t.Errorf("message %d is a tool result with no tool call before it", i)
			continue
		}
		var sawToolUse bool
		for _, b := range blocks {
			if b.Type == "tool_use" {
				sawToolUse = true
			}
		}
		if !sawToolUse {
			t.Errorf("message %d is a tool result whose call was cut away", i)
		}
	}
}
