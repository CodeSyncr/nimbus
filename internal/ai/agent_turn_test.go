package ai

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scriptedTurnClient plays back canned responses per mode, in order, and
// records every TurnRequest so tests can assert on what the model was shown.
type scriptedTurnClient struct {
	mockAIClient
	responses map[TurnMode][]*MessageResponse
	calls     map[TurnMode]int
	requests  []*TurnRequest
}

func (s *scriptedTurnClient) Turn(ctx context.Context, req *TurnRequest, onDelta StreamHandler) (*MessageResponse, error) {
	if s.calls == nil {
		s.calls = map[TurnMode]int{}
	}
	s.requests = append(s.requests, req)
	queue := s.responses[req.Mode]
	idx := s.calls[req.Mode]
	s.calls[req.Mode]++
	if idx >= len(queue) {
		return &MessageResponse{Role: "assistant", Content: []ContentBlock{{Type: "text", Text: "Done."}}}, nil
	}
	return queue[idx], nil
}

func text(t string) *MessageResponse {
	return &MessageResponse{Role: "assistant", Content: []ContentBlock{{Type: "text", Text: t}}}
}

func toolUse(id, name string, input map[string]any) *MessageResponse {
	return &MessageResponse{Role: "assistant", Content: []ContentBlock{{Type: "tool_use", ID: id, Name: name, Input: input}}}
}

func lastUserText(req *TurnRequest) string {
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			if s, ok := req.Messages[i].Content.(string); ok {
				return s
			}
		}
	}
	return ""
}

func TestAgentExploresPlansExecutesAndRepairs(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	client := &scriptedTurnClient{
		responses: map[TurnMode][]*MessageResponse{
			TurnModeExplore: {
				toolUse("t1", "read_file", map[string]any{"path": "main.go"}),
				text("FINDINGS: main.go has an empty main; add greet.go"),
			},
			TurnModePlan: {
				text("```json\n{\"summary\":\"Add greet\",\"steps\":[{\"id\":1,\"action\":\"create_file\",\"target\":\"greet.go\",\"description\":\"Add Greet\",\"risk\":\"low\"}]}\n```"),
			},
			TurnModeExecute: {
				toolUse("t2", "write_file", map[string]any{"path": "greet.go", "content": "package main\n\nfunc Greet() string { return \"hi\" }\n"}),
				text("Created greet.go"),
				// After the injected build failure the model "fixes" and finishes.
				text("Fixed the build. Created greet.go"),
			},
		},
	}

	agent := NewAgent(client, NewToolExecutor(dir), &ProjectContext{AppRoot: dir, ProjectName: "t"}, NewSession("optimal"))
	verifyCalls := 0
	agent.Verifier = func(ctx context.Context) (string, bool) {
		verifyCalls++
		if verifyCalls == 1 {
			return "./greet.go:3: undefined: x", false
		}
		return "", true
	}

	plan, err := agent.GeneratePlan(context.Background(), "add a greet function")
	if err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].Target != "greet.go" {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	if !strings.Contains(agent.Session.Findings, "empty main") {
		t.Errorf("findings not recorded: %q", agent.Session.Findings)
	}
	// The read_file result must reach the model in full, not truncated.
	explore2 := client.requests[1]
	blocks, ok := explore2.Messages[len(explore2.Messages)-1].Content.([]ContentBlock)
	if !ok || len(blocks) == 0 || !strings.Contains(blocks[0].Content, "func main() {}") {
		t.Errorf("tool result not fed back to the model: %+v", explore2.Messages)
	}
	// Planning is told what exploration found.
	if !strings.Contains(lastUserText(client.requests[2]), "FINDINGS") {
		t.Errorf("plan brief missing findings")
	}

	summary, err := agent.ExecuteApprovedPlan(context.Background(), plan)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "greet.go")); err != nil {
		t.Errorf("greet.go was not written")
	}
	if verifyCalls != 2 {
		t.Errorf("expected verify → fail → repair → verify, got %d verifier calls", verifyCalls)
	}
	if !strings.Contains(summary, "Fixed") {
		t.Errorf("expected the post-repair summary, got %q", summary)
	}
	// The repair prompt carried the build error.
	var sawRepair bool
	for _, r := range client.requests {
		if r.Mode == TurnModeExecute && strings.Contains(lastUserText(r), "undefined: x") {
			sawRepair = true
		}
	}
	if !sawRepair {
		t.Errorf("build errors were not fed back to the model")
	}
	// Conversation memory records the turn and the touched files.
	if len(agent.Session.Turns) != 1 || len(agent.Session.Turns[0].FilesChanged) != 1 || agent.Session.Turns[0].FilesChanged[0] != "greet.go" {
		t.Errorf("turn memory not recorded: %+v", agent.Session.Turns)
	}

	// A follow-up request sees the earlier turn in its brief.
	client.responses[TurnModeExplore] = []*MessageResponse{text("FINDINGS: nothing new")}
	client.responses[TurnModePlan] = []*MessageResponse{text("It already exists.")}
	client.calls = nil
	plan2, err := agent.GeneratePlan(context.Background(), "is greet done?")
	if err != nil {
		t.Fatalf("follow-up plan: %v", err)
	}
	if len(plan2.Steps) != 0 || !strings.Contains(plan2.Summary, "already exists") {
		t.Errorf("expected conversational answer, got %+v", plan2)
	}
	if !strings.Contains(lastUserText(client.requests[len(client.requests)-2]), "add a greet function") {
		t.Errorf("follow-up explore brief lacks conversation memory")
	}
}

func TestAgentFallsBackToLegacyEndpoints(t *testing.T) {
	dir := t.TempDir()
	legacy := &mockAIClient{
		PlanResponse: &PlanSummary{Summary: "legacy", Steps: []PlanStep{{ID: 1, Action: "create_file", Target: "a.go", Description: "x"}}},
		ExecuteResponse: &MessageResponse{Role: "assistant", Content: []ContentBlock{
			{Type: "tool_use", ID: "c1", Name: "write_file", Input: map[string]any{"path": "a.go", "content": "package a\n"}},
		}},
	}
	agent := NewAgent(legacy, NewToolExecutor(dir), &ProjectContext{AppRoot: dir}, NewSession(""))
	agent.Verifier = nil
	plan, err := agent.GeneratePlan(context.Background(), "make a")
	if err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}
	if plan.Summary != "legacy" {
		t.Fatalf("legacy plan not used: %+v", plan)
	}
	// Legacy execute mock always returns a tool call; the loop must terminate.
	if _, err := agent.ExecuteApprovedPlan(context.Background(), plan); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.go")); err != nil {
		t.Errorf("a.go not written via legacy path")
	}
}

func TestParsePlanJSONVariants(t *testing.T) {
	cases := map[string]struct {
		in       string
		steps    int
		question bool
		wantErr  bool
	}{
		"fenced":        {in: "Here you go:\n```json\n{\"summary\":\"s\",\"steps\":[{\"id\":1,\"action\":\"create_file\",\"target\":\"a\"}]}\n```", steps: 1},
		"clarification": {in: `{"summary":"?","needs_clarification":true,"questions":[{"id":"q","question":"Which?"}],"steps":[]}`, question: true},
		"answer-only":   {in: `{"summary":"Routing works via start/routes.go","steps":[]}`},
		"prose":         {in: "Just an explanation with no braces", wantErr: true},
	}
	for name, c := range cases {
		plan, err := parsePlanJSON(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("%s: expected error", name)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if len(plan.Steps) != c.steps || plan.NeedsClarification != c.question {
			t.Errorf("%s: unexpected plan %+v", name, plan)
		}
	}
}

func TestCompactMessagesKeepsRecentToolOutput(t *testing.T) {
	big := strings.Repeat("x", 5000)
	var msgs []Message
	for i := 0; i < keepFullToolResults+3; i++ {
		msgs = append(msgs,
			Message{Role: "assistant", Content: []ContentBlock{{Type: "tool_use", ID: "id", Name: "write_file", Input: map[string]any{"path": "f.go", "content": big}}}},
			Message{Role: "user", Content: []ContentBlock{{Type: "tool_result", ToolUseID: "id", Content: big}}},
		)
	}
	// One skill result early in history must survive intact.
	msgs = append([]Message{{Role: "user", Content: []ContentBlock{{Type: "tool_result", ToolUseID: "s", Content: "# Skill: x\n" + big}}}}, msgs...)

	out := compactMessagesForWire(msgs)

	last := out[len(out)-1].Content.([]ContentBlock)[0]
	if len(last.Content) != len(big) {
		t.Errorf("newest tool result was truncated to %d chars", len(last.Content))
	}
	first := out[1].Content.([]ContentBlock)[0]
	if len(first.Content) >= len(big) {
		t.Errorf("oldest tool result was not elided")
	}
	skill := out[0].Content.([]ContentBlock)[0]
	if len(skill.Content) < len(big) {
		t.Errorf("skill result must not be elided")
	}
	newestWrite := out[len(out)-2].Content.([]ContentBlock)[0]
	if newestWrite.Input["content"] != big {
		t.Errorf("newest write_file content should be kept")
	}
	oldWrite := out[1+0].Content.([]ContentBlock) // index 1 is the first assistant write
	if oldWrite[0].Type == "tool_use" && oldWrite[0].Input["content"] == big {
		t.Errorf("old write_file content should be elided")
	}
}
