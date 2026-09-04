package ai

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runAgent builds an agent over a temp workspace with a scripted model.
func runAgent(t *testing.T, client AIClient) (*Agent, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	tools := NewToolExecutor(dir)
	agent := NewAgent(client, tools, &ProjectContext{AppRoot: dir}, NewSession("optimal"))
	agent.Verifier = nil // no build in tests unless a case asks for it
	return agent, dir
}

// A question must be answered, with no plan and no approval gate: the
// staged pipeline used to force both.
func TestRunAnswersAQuestionWithoutPlanningOrApproval(t *testing.T) {
	client := &scriptedTurnClient{
		responses: map[TurnMode][]*MessageResponse{
			TurnModeAgent: {text("This is a Nimbus web app; routing lives in start/routes.go.")},
		},
	}
	agent, _ := runAgent(t, client)

	res, err := agent.Run(context.Background(), "what does this project do?")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(res.Text, "Nimbus web app") {
		t.Errorf("answer = %q", res.Text)
	}
	if agent.Session.Plan != nil {
		t.Error("a question produced a plan")
	}
	if client.calls[TurnModePlan] != 0 || client.calls[TurnModeExplore] != 0 {
		t.Errorf("question went through the staged pipeline: %+v", client.calls)
	}
	if len(res.ChangedFiles) != 0 {
		t.Errorf("a question changed files: %v", res.ChangedFiles)
	}
}

// The whole dialogue must reach the model, so "continue" refers to what just
// happened instead of starting a new investigation.
func TestRunKeepsTheConversationSoContinueResumes(t *testing.T) {
	client := &scriptedTurnClient{
		responses: map[TurnMode][]*MessageResponse{
			TurnModeAgent: {
				text("I added the Post model. Next I would wire the routes."),
				text("Routes wired."),
			},
		},
	}
	agent, _ := runAgent(t, client)

	if _, err := agent.Run(context.Background(), "add a Post model"); err != nil {
		t.Fatal(err)
	}
	if _, err := agent.Run(context.Background(), "continue"); err != nil {
		t.Fatal(err)
	}

	if len(client.requests) != 2 {
		t.Fatalf("expected 2 turns, got %d", len(client.requests))
	}

	// The second turn must carry the first request, the assistant's reply and
	// the new message.
	second := client.requests[1]
	var transcript []string
	for _, m := range second.Messages {
		if s, ok := m.Content.(string); ok {
			transcript = append(transcript, s)
		}
		if blocks, ok := m.Content.([]ContentBlock); ok {
			for _, b := range blocks {
				transcript = append(transcript, b.Text)
			}
		}
	}
	joined := strings.Join(transcript, "\n")

	for _, want := range []string{"add a Post model", "I added the Post model", "continue"} {
		if !strings.Contains(joined, want) {
			t.Errorf("second turn is missing %q from the conversation:\n%s", want, joined)
		}
	}
}

// Tool calls run and their results are fed back into the same conversation.
func TestRunExecutesToolsAndFeedsResultsBack(t *testing.T) {
	client := &scriptedTurnClient{
		responses: map[TurnMode][]*MessageResponse{
			TurnModeAgent: {
				toolUse("t1", "write_file", map[string]any{
					"path": "app/models/post.go", "content": "package models\n\ntype Post struct{}\n",
				}),
				text("Created app/models/post.go with the Post model."),
			},
		},
	}
	agent, dir := runAgent(t, client)

	res, err := agent.Run(context.Background(), "create a Post model")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if _, statErr := os.Stat(filepath.Join(dir, "app/models/post.go")); statErr != nil {
		t.Fatalf("the file was not written: %v", statErr)
	}
	if res.ToolCalls != 1 {
		t.Errorf("ToolCalls = %d, want 1", res.ToolCalls)
	}
	if len(res.ChangedFiles) != 1 || !strings.Contains(res.ChangedFiles[0], "post.go") {
		t.Errorf("ChangedFiles = %v", res.ChangedFiles)
	}

	// The follow-up turn must include the tool result.
	if len(client.requests) < 2 {
		t.Fatal("the tool result was never sent back to the model")
	}
	var sawToolResult bool
	for _, m := range client.requests[1].Messages {
		if blocks, ok := m.Content.([]ContentBlock); ok {
			for _, b := range blocks {
				if b.Type == "tool_result" {
					sawToolResult = true
				}
			}
		}
	}
	if !sawToolResult {
		t.Error("no tool_result block in the follow-up turn")
	}
}

// A failing build is handed back for repair instead of being reported as done.
func TestRunVerifiesAndRepairsAfterChanges(t *testing.T) {
	client := &scriptedTurnClient{
		responses: map[TurnMode][]*MessageResponse{
			TurnModeAgent: {
				toolUse("t1", "write_file", map[string]any{"path": "broken.go", "content": "package main\n"}),
				text("Done."),            // claims completion; build fails
				text("Fixed the build."), // repair turn
			},
		},
	}
	agent, _ := runAgent(t, client)

	var verifierCalls int
	agent.Verifier = func(ctx context.Context) (string, bool) {
		verifierCalls++
		if verifierCalls == 1 {
			return "broken.go:1: syntax error", false
		}
		return "ok", true
	}

	res, err := agent.Run(context.Background(), "add a file")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if verifierCalls < 2 {
		t.Errorf("verifier ran %d times; the failure was not re-checked", verifierCalls)
	}
	if !strings.Contains(res.Text, "Fixed") {
		t.Errorf("final text = %q, want the repaired summary", res.Text)
	}
	if !res.Verified {
		t.Error("Verified should be true once the build passes")
	}
}

// The conversation is persisted, so --resume continues an existing session.
func TestRunPersistsConversationForResume(t *testing.T) {
	client := &scriptedTurnClient{
		responses: map[TurnMode][]*MessageResponse{
			TurnModeAgent: {text("Looked at the router.")},
		},
	}
	agent, dir := runAgent(t, client)

	if _, err := agent.Run(context.Background(), "explain the router"); err != nil {
		t.Fatal(err)
	}
	if err := SaveSession(dir, agent.Session); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	loaded, err := LoadSession(dir, agent.Session.ID)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if len(loaded.Messages) < 2 {
		t.Fatalf("resumed session has %d messages, want the conversation", len(loaded.Messages))
	}
	if s, _ := loaded.Messages[0].Content.(string); !strings.Contains(s, "explain the router") {
		t.Errorf("first message = %v", loaded.Messages[0].Content)
	}
}

// An older server that rejects the agent mode must be retried in a mode it
// knows, not surfaced as a failure.
func TestRunFallsBackWhenServerRejectsAgentMode(t *testing.T) {
	client := &modeRejectingClient{}
	agent, _ := runAgent(t, client)

	res, err := agent.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Text != "answered in chat mode" {
		t.Errorf("text = %q", res.Text)
	}
	if !client.sawChat {
		t.Error("never retried in chat mode")
	}
}

// modeRejectingClient rejects "agent" the way a pre-agent server does.
type modeRejectingClient struct {
	mockAIClient
	sawChat bool
}

func (m *modeRejectingClient) Turn(ctx context.Context, req *TurnRequest, onDelta StreamHandler) (*MessageResponse, error) {
	if req.Mode == TurnModeAgent {
		return nil, errors.New(`unknown mode "agent"`)
	}
	if req.Mode == TurnModeChat {
		m.sawChat = true
		return text("answered in chat mode"), nil
	}
	return text("unexpected mode"), nil
}

// Long conversations are trimmed rather than growing until the model refuses
// them, and the trim never orphans a tool call from its result.
func TestSessionPrunesLongConversations(t *testing.T) {
	s := NewSession("optimal")
	s.AppendUser("the original request")

	for i := 0; i < maxSessionMessages+40; i++ {
		s.AppendAssistant([]ContentBlock{{Type: "tool_use", ID: "t", Name: "read_file", Input: map[string]any{"path": "x"}}})
		s.AppendToolResults([]ContentBlock{{Type: "tool_result", Text: "contents"}})
	}

	if len(s.Messages) > maxSessionMessages+1 {
		t.Errorf("conversation grew to %d messages", len(s.Messages))
	}
	if first, _ := s.Messages[0].Content.(string); !strings.Contains(first, "original request") {
		t.Errorf("the original request was pruned away: %v", s.Messages[0].Content)
	}
	// The first message after the trim marker must not be a bare tool result.
	for i, m := range s.Messages {
		if str, ok := m.Content.(string); ok && strings.Contains(str, "trimmed") {
			if i+1 < len(s.Messages) && isToolResultMessage(s.Messages[i+1]) {
				t.Error("trim left a tool result with no matching tool call")
			}
			break
		}
	}
}

// A session whose client never resolved must fail with a message, not crash
// the terminal with a nil dereference from a background goroutine.
func TestRunWithoutAClientErrorsInsteadOfPanicking(t *testing.T) {
	agent := NewAgent(nil, NewToolExecutor(t.TempDir()), &ProjectContext{AppRoot: t.TempDir()}, NewSession("optimal"))

	_, err := agent.Run(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected an error with no client configured")
	}
	if !strings.Contains(err.Error(), "no AI client") {
		t.Errorf("error should explain the cause: %v", err)
	}
}
