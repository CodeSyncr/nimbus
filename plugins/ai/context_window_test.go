package ai

import (
	"context"
	"strings"
	"testing"
)

func longTurns(n int) []Message {
	var msgs []Message
	for i := 0; i < n; i++ {
		msgs = append(msgs,
			Message{Role: RoleUser, Content: strings.Repeat("question ", 100)},
			Message{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "t", Name: "echo", Args: []byte(`{"q":"x"}`)}}},
			Message{Role: RoleTool, ToolCallID: "t", Content: strings.Repeat("result ", 100)},
			Message{Role: RoleAssistant, Content: strings.Repeat("answer ", 100)},
		)
	}
	return msgs
}

func TestEstimateTokens(t *testing.T) {
	n := EstimateTokens([]Message{{Role: RoleUser, Content: strings.Repeat("abcd", 100)}, {Role: RoleUser, Images: []string{"x"}}})
	if n < 1100 || n > 1120 {
		t.Fatalf("EstimateTokens = %d", n)
	}
}

func TestContextLimitDropsOldTurnsAtSafeBoundaries(t *testing.T) {
	fake := NewFake(FakeText("ok"))
	mem := MemoryStore()
	_ = mem.Save(context.Background(), "c", longTurns(20)) // ~20k tokens of history
	agent := NewAgent("x").WithClient(fake.Client()).WithMemory(mem, "c").WithContextLimit(2000)
	if _, err := agent.Prompt(context.Background(), "latest question"); err != nil {
		t.Fatal(err)
	}
	sent := fake.Requests()[0].Messages
	if EstimateTokens(sent) > 2000 {
		t.Fatalf("sent %d tokens, over the 2000 budget", EstimateTokens(sent))
	}
	if sent[0].Role != RoleUser {
		t.Fatalf("history must start at a user turn, starts with %s", sent[0].Role)
	}
	if sent[len(sent)-1].Content != "latest question" {
		t.Fatal("the current question was dropped")
	}
	// Every tool result still follows its call.
	for i, m := range sent {
		if m.Role == RoleTool && (i == 0 || len(sent[i-1].ToolCalls) == 0) {
			t.Fatalf("tool result at %d is separated from its call", i)
		}
	}
	saved, _ := mem.Load(context.Background(), "c")
	if len(saved) >= 80 {
		t.Fatalf("compacted history was not saved: %d messages", len(saved))
	}
}

func TestContextLimitSummarizesRemovedTurns(t *testing.T) {
	fake := NewFake(FakeText("The user asked about refunds for order 42."), FakeText("ok"))
	mem := MemoryStore()
	_ = mem.Save(context.Background(), "c", longTurns(20))
	agent := NewAgent("x").WithClient(fake.Client()).WithMemory(mem, "c").WithContextLimit(3000, SummarizeOlder())
	if _, err := agent.Prompt(context.Background(), "and now?"); err != nil {
		t.Fatal(err)
	}
	reqs := fake.Requests()
	if len(reqs) != 2 || !strings.Contains(reqs[0].System, "Summarise this conversation") {
		t.Fatalf("expected a summary call first, got %d requests", len(reqs))
	}
	first := reqs[1].Messages[0]
	if first.Role != RoleSystem || !strings.Contains(first.Content, "refunds for order 42") {
		t.Fatalf("history should open with the summary: %+v", first)
	}
	if EstimateTokens(reqs[1].Messages) > 3000 {
		t.Fatalf("over budget: %d", EstimateTokens(reqs[1].Messages))
	}
}

func TestContextLimitLeavesShortConversationsAlone(t *testing.T) {
	fake := NewFake(FakeText("ok"))
	agent := NewAgent("x").WithClient(fake.Client()).WithMessages(longTurns(1)).WithContextLimit(100000, SummarizeOlder())
	if _, err := agent.Prompt(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	if len(fake.Requests()) != 1 || len(fake.Requests()[0].Messages) != 5 {
		t.Fatalf("history changed under the budget: %d requests", len(fake.Requests()))
	}
}
