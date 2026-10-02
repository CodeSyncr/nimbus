package ai

import (
	"context"
	"strings"
	"testing"
)

func TestAgentAsTool(t *testing.T) {
	// The fake serves both agents in order: writer asks the researcher,
	// the researcher answers, then the writer replies.
	fake := NewFake(
		FakeToolCall("research", `{"input":"When was Go released?"}`),
		FakeText("Go 1.0 was released in March 2012."),
		FakeText("Go has been stable since 2012."),
	)
	researcher := NewAgent("You research facts.").WithClient(fake.Client())
	writer := NewAgent("You write replies.").WithClient(fake.Client()).
		WithToolObjects(researcher.AsTool("research", "Look up facts"))

	resp, err := writer.Prompt(context.Background(), "Write about Go's history")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "Go has been stable since 2012." {
		t.Fatalf("reply = %q", resp.Text)
	}
	reqs := fake.Requests()
	if reqs[1].System != "You research facts." || reqs[1].Messages[0].Content != "When was Go released?" {
		t.Fatalf("researcher got system=%q msgs=%+v", reqs[1].System, reqs[1].Messages)
	}
	if got := lastToolResult(reqs[2].Messages); !strings.Contains(got, "March 2012") {
		t.Fatalf("writer got tool result %q", got)
	}
}

func TestHandoffMovesTheConversation(t *testing.T) {
	refund, _ := NewTool("refund").Desc("refund").Handler(func(_ context.Context, in refundIn) (string, error) {
		return "refunded " + in.OrderID, nil
	}).Build()
	fake := NewFake(
		FakeToolCall("transfer_to_billing", `{"reason":"refund request"}`),
		FakeToolCall("refund", `{"order_id":"9"}`),
		FakeText("Done: order 9 is refunded."),
	)
	mem := MemoryStore()
	billing := NewAgent("You are billing.").WithClient(fake.Client()).Named("billing", "Invoices and refunds").
		WithToolObjects(refund)
	triage := NewAgent("Triage requests.").WithClient(fake.Client()).WithHandoffs(billing).WithMemory(mem, "c")

	resp, err := triage.Prompt(context.Background(), "Refund order 9")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Agent != "billing" || resp.Text != "Done: order 9 is refunded." {
		t.Fatalf("resp = %+v", resp)
	}
	reqs := fake.Requests()
	if reqs[0].System != "Triage requests." || !hasTool(reqs[0].Tools, "transfer_to_billing") || hasTool(reqs[0].Tools, "refund") {
		t.Fatalf("triage step = %+v", reqs[0].Tools)
	}
	if reqs[1].System != "You are billing." || !hasTool(reqs[1].Tools, "refund") {
		t.Fatalf("after handoff the billing agent should run: system=%q", reqs[1].System)
	}
	if !strings.Contains(lastToolResult(reqs[1].Messages), "Transferred to billing") {
		t.Fatal("handoff call was not answered")
	}
	// The conversation is saved to the first agent's memory.
	saved, _ := mem.Load(context.Background(), "c")
	if saved[len(saved)-1].Content != "Done: order 9 is refunded." {
		t.Fatalf("memory = %+v", saved[len(saved)-1])
	}
}

func TestHandoffTargetsMustBeNamed(t *testing.T) {
	fake := NewFake(FakeText("x"))
	_, err := NewAgent("x").WithClient(fake.Client()).WithHandoffs(NewAgent("anon")).Prompt(context.Background(), "hi")
	if err == nil || !strings.Contains(err.Error(), "Named") {
		t.Fatalf("err = %v", err)
	}
}

func hasTool(tools []ToolSpec, name string) bool {
	for _, t := range tools {
		if t.Name == name {
			return true
		}
	}
	return false
}
