package ai

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
)

type refundIn struct {
	OrderID string `json:"order_id"`
}

func refundTool(t *testing.T, ran *int32) *Tool {
	t.Helper()
	tool, err := NewTool("create_refund").Desc("refund").RequireApproval().
		Handler(func(_ context.Context, in refundIn) (string, error) {
			atomic.AddInt32(ran, 1)
			return "refunded " + in.OrderID, nil
		}).Build()
	if err != nil {
		t.Fatal(err)
	}
	return tool
}

func lastToolResult(msgs []Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == RoleTool {
			return msgs[i].Content
		}
	}
	return ""
}

func TestApprovalHookDecidesOnTheSpot(t *testing.T) {
	var ran int32
	fake := NewFake(FakeToolCall("create_refund", `{"order_id":"42"}`), FakeText("Refund declined."))
	var asked ApprovalRequest
	_, err := NewAgent("x").WithClient(fake.Client()).WithToolObjects(refundTool(t, &ran)).
		OnApproval(func(_ context.Context, r ApprovalRequest) (Decision, error) {
			asked = r
			return Decision{Approved: false, Reason: "over the limit"}, nil
		}).Prompt(context.Background(), "refund 42")
	if err != nil {
		t.Fatal(err)
	}
	if ran != 0 || asked.Call.Name != "create_refund" || asked.Tool == nil {
		t.Fatalf("ran=%d asked=%+v", ran, asked)
	}
	if got := lastToolResult(fake.Requests()[1].Messages); !strings.Contains(got, "did not approve") || !strings.Contains(got, "over the limit") {
		t.Fatalf("model was told %q", got)
	}
}

func TestPauseAndResumeAcrossRequestsWithMemory(t *testing.T) {
	var ran int32
	mem := MemoryStore()
	fake := NewFake(
		FakeToolCalls(ToolCall{ID: "c1", Name: "create_refund", Args: []byte(`{"order_id":"42"}`)},
			ToolCall{ID: "c2", Name: "echo", Args: []byte(`{"q":"hi"}`)}),
		FakeText("Refunded order 42."),
	)
	newAgent := func() *Agent { // a fresh agent per HTTP request, sharing memory
		return NewAgent("x").WithClient(fake.Client()).
			WithToolObjects(refundTool(t, &ran), echoTool(t)).WithMemory(mem, "conv-1")
	}

	_, err := newAgent().Prompt(context.Background(), "refund 42")
	var pending *ApprovalRequiredError
	if !errors.As(err, &pending) || len(pending.Pending) != 1 || pending.Pending[0].ID != "c1" {
		t.Fatalf("err = %v", err)
	}
	if ran != 0 {
		t.Fatal("a call needing approval ran before it was approved")
	}
	// The call that needed no approval already ran.
	if got, _ := newAgent().Pending(context.Background()); len(got) != 1 || got[0].ID != "c1" {
		t.Fatalf("Pending = %+v", got)
	}

	resp, err := newAgent().Resume(context.Background(), Approve("c1"))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "Refunded order 42." || ran != 1 {
		t.Fatalf("resp=%q ran=%d", resp.Text, ran)
	}
	msgs := fake.Requests()[1].Messages
	var results []string
	for _, m := range msgs {
		if m.Role == RoleTool {
			results = append(results, m.ToolCallID+"="+m.Content)
		}
	}
	if len(results) != 2 || !strings.Contains(strings.Join(results, " "), `c2="echo:hi"`) || !strings.Contains(strings.Join(results, " "), `c1="refunded 42"`) {
		t.Fatalf("tool results sent = %v", results)
	}
	if _, err := newAgent().Resume(context.Background()); !errors.Is(err, ErrNothingToResume) {
		t.Fatalf("second resume = %v", err)
	}
}

func TestResumeDeniesCallsWithoutADecision(t *testing.T) {
	var ran int32
	fake := NewFake(FakeToolCall("create_refund", `{"order_id":"7"}`), FakeText("ok"))
	agent := NewAgent("x").WithClient(fake.Client()).WithToolObjects(refundTool(t, &ran)) // no memory: kept in the agent
	if _, err := agent.Prompt(context.Background(), "refund 7"); err == nil {
		t.Fatal("expected a pause")
	}
	if _, err := agent.Resume(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ran != 0 {
		t.Fatal("an undecided call ran")
	}
	if got := lastToolResult(fake.Requests()[1].Messages); !strings.Contains(got, "no decision") {
		t.Fatalf("model was told %q", got)
	}
}

func TestStreamPausesForApproval(t *testing.T) {
	var ran int32
	fake := NewFake(FakeToolCall("create_refund", `{"order_id":"1"}`))
	st, err := NewAgent("x").WithClient(fake.Client()).WithToolObjects(refundTool(t, &ran)).Stream(context.Background(), "refund")
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.Collect(context.Background())
	var pending *ApprovalRequiredError
	if !errors.As(err, &pending) || ran != 0 {
		t.Fatalf("err=%v ran=%d", err, ran)
	}
}
