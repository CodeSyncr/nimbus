package tui

import (
	"strings"
	"testing"

	"github.com/CodeSyncr/nimbus/internal/ai"
)

// The exact bug: a resumed session had its conversation in context but showed
// a blank window. The transcript must survive the round trip through the
// session file, where blocks arrive as generic JSON rather than typed structs.
func TestResumedSessionShowsThePreviousConversation(t *testing.T) {
	dir := t.TempDir()

	session := ai.NewSession("optimal")
	session.AppendUser("add a Post model")
	session.AppendAssistant([]ai.ContentBlock{
		{Type: "text", Text: "I'll create the model first."},
		{Type: "tool_use", ID: "t1", Name: "write_file", Input: map[string]any{"path": "app/models/post.go"}},
	})
	session.AppendToolResults([]ai.ContentBlock{
		{Type: "tool_result", ToolUseID: "t1", Content: "CREATED app/models/post.go"},
	})
	session.AppendAssistant([]ai.ContentBlock{{Type: "text", Text: "Created app/models/post.go."}})

	if err := ai.SaveSession(dir, session); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	loaded, err := ai.LoadSession(dir, session.ID)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}

	items := restoreTranscript(loaded)
	if len(items) == 0 {
		t.Fatal("a resumed session rendered an empty transcript")
	}

	var roles []string
	var text strings.Builder
	for _, it := range items {
		roles = append(roles, it.Role)
		text.WriteString(it.Role + ":" + it.Content + " " + it.ToolName + " " + it.Detail + "\n")
	}
	got := text.String()

	for _, want := range []string{"add a Post model", "I'll create the model first", "write_file", "Created app/models/post.go"} {
		if !strings.Contains(got, want) {
			t.Errorf("restored transcript is missing %q:\n%s", want, got)
		}
	}
	if roles[0] != "user" {
		t.Errorf("first item role = %q, want user", roles[0])
	}

	// The tool result belongs to its tool line, not to a line of its own.
	var toolItem *ChatItem
	for i := range items {
		if items[i].Role == "tool" {
			toolItem = &items[i]
		}
	}
	if toolItem == nil {
		t.Fatal("the tool call was not restored")
	}
	if toolItem.Detail == "" {
		t.Error("the tool result was not attached to its tool line")
	}
	if toolItem.Content != "app/models/post.go" {
		t.Errorf("tool target = %q, want the path", toolItem.Content)
	}
}

// The agent's own injected messages must not appear as things the user said.
func TestRestoredMachineryIsNotShownAsUserSpeech(t *testing.T) {
	session := ai.NewSession("optimal")
	session.AppendUser("fix the build")
	session.AppendUser("VERIFICATION FAILED. The project does not build:\n\nsyntax error")
	session.AppendUser("[earlier turns in this session were trimmed to fit the context window]")

	items := restoreTranscript(session)
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	if items[0].Role != "user" {
		t.Errorf("the real request should be a user line, got %q", items[0].Role)
	}
	if items[1].Role != "system" {
		t.Errorf("a verification failure is machinery, got role %q", items[1].Role)
	}
	if items[2].Role != "system" {
		t.Errorf("a trim notice is machinery, got role %q", items[2].Role)
	}
}

func TestRestoreHandlesAnEmptyOrMissingSession(t *testing.T) {
	if got := restoreTranscript(nil); len(got) != 0 {
		t.Errorf("nil session produced %d items", len(got))
	}
	if got := restoreTranscript(ai.NewSession("optimal")); len(got) != 0 {
		t.Errorf("fresh session produced %d items", len(got))
	}
}

// The wiring that was missing: NewModel must seed its visible transcript from
// the agent's session, not start blank.
func TestNewModelShowsARestoredSession(t *testing.T) {
	session := ai.NewSession("optimal")
	session.AppendUser("what does this project do?")
	session.AppendAssistant([]ai.ContentBlock{{Type: "text", Text: "It is a Go web app."}})

	agent := ai.NewAgent(nil, ai.NewToolExecutor(t.TempDir()), &ai.ProjectContext{AppRoot: t.TempDir()}, session)

	m := NewModel(agent, "", false)
	if len(m.Messages) == 0 {
		t.Fatal("NewModel started with a blank transcript for a resumed session")
	}

	var joined strings.Builder
	for _, it := range m.Messages {
		joined.WriteString(it.Content + "\n")
	}
	for _, want := range []string{"what does this project do?", "It is a Go web app."} {
		if !strings.Contains(joined.String(), want) {
			t.Errorf("missing %q from the restored view:\n%s", want, joined.String())
		}
	}

	// The greeting belongs above the restored history, not below it.
	if m.Messages[0].Role != "system" {
		t.Errorf("first line should be the welcome banner, got %q: %q", m.Messages[0].Role, m.Messages[0].Content)
	}
	if m.Messages[1].Content != "what does this project do?" {
		t.Errorf("history should follow the banner in order, got %q", m.Messages[1].Content)
	}

	// A fresh session shows only the banner.
	fresh := ai.NewAgent(nil, ai.NewToolExecutor(t.TempDir()), &ai.ProjectContext{AppRoot: t.TempDir()}, ai.NewSession("optimal"))
	if got := NewModel(fresh, "", false).Messages; len(got) != 1 {
		t.Errorf("a new session should show only the welcome banner, got %d items", len(got))
	}
}

// The reported bug: after compaction — which rewrites the model's context —
// reopening the session showed almost nothing, and the notices the agent
// printed were gone entirely.
func TestTranscriptSurvivesCompactionAndReopen(t *testing.T) {
	dir := t.TempDir()
	agent := ai.NewAgent(nil, ai.NewToolExecutor(dir), &ai.ProjectContext{AppRoot: dir}, ai.NewSession("optimal"))
	m := NewModel(agent, "", false)
	m.Width, m.Height, m.Ready = 100, 40, true

	// A session's worth of activity, including a notice that never goes to
	// the model.
	m.say(ChatItem{Role: "user", Content: "add a Post model"})
	m.say(ChatItem{Role: "tool", ToolName: "write_file", Content: "app/models/post.go", Detail: "created"})
	m.say(ChatItem{Role: "assistant", Content: "Created app/models/post.go."})
	m.say(ChatItem{Role: "system", Content: "Compacted 34 messages into a summary — about 18.2k tokens reclaimed."})

	// Compaction rewrites the model's context; the transcript must not follow.
	agent.Session.Messages = []ai.Message{{Role: "user", Content: "[summary of earlier turns]"}}

	if err := ai.SaveSession(dir, agent.Session); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	loaded, err := ai.LoadSession(dir, agent.Session.ID)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}

	reopened := NewModel(
		ai.NewAgent(nil, ai.NewToolExecutor(dir), &ai.ProjectContext{AppRoot: dir}, loaded),
		"", false,
	)

	var all strings.Builder
	for _, item := range reopened.Messages {
		all.WriteString(item.Role + ":" + item.Content + " " + item.ToolName + " " + item.Detail + "\n")
	}
	got := all.String()

	for _, want := range []string{
		"add a Post model",
		"write_file",
		"Created app/models/post.go.",
		"Compacted 34 messages", // the notice the model never saw
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reopened session is missing %q:\n%s", want, got)
		}
	}
}

// The banner is regenerated each time, so storing it would stack up one copy
// per reopen.
func TestWelcomeBannerIsNotRecorded(t *testing.T) {
	dir := t.TempDir()
	session := ai.NewSession("optimal")
	agent := ai.NewAgent(nil, ai.NewToolExecutor(dir), &ai.ProjectContext{AppRoot: dir}, session)

	NewModel(agent, "", false)
	NewModel(agent, "", false)
	NewModel(agent, "", false)

	if len(session.Transcript) != 0 {
		t.Errorf("opening the session recorded %d entries; the banner should be display-only", len(session.Transcript))
	}
}

// A late-arriving diff must reach the stored copy of its tool line.
func TestDiffIsRecordedOnTheStoredToolLine(t *testing.T) {
	dir := t.TempDir()
	session := ai.NewSession("optimal")
	agent := ai.NewAgent(nil, ai.NewToolExecutor(dir), &ai.ProjectContext{AppRoot: dir}, session)
	m := NewModel(agent, "", false)
	m.Width, m.Height, m.Ready = 100, 40, true

	m.say(ChatItem{Role: "tool", ToolName: "edit_file", Content: "main.go"})
	updated, _ := m.Update(diffMsg{path: "main.go", diff: "--- a\n+++ b\n+added line\n"})
	m = updated.(Model)

	var stored bool
	for _, e := range session.Transcript {
		if e.Role == "tool" && strings.Contains(e.Diff, "added line") {
			stored = true
		}
	}
	if !stored {
		t.Errorf("the diff never reached the stored transcript: %+v", session.Transcript)
	}
}
