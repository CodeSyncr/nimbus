package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/CodeSyncr/nimbus/internal/ai"
)

func chatModel(t *testing.T) Model {
	t.Helper()
	agent := ai.NewAgent(nil, ai.NewToolExecutor(t.TempDir()), &ai.ProjectContext{AppRoot: t.TempDir()}, ai.NewSession("optimal"))
	m := NewModel(agent, "", false)
	m.Width, m.Height, m.Ready = 100, 40, true
	m.TextInput.SetWidth(80)
	return m
}

func send(m Model, msg tea.Msg) Model {
	updated, _ := m.Update(msg)
	return updated.(Model)
}

func typeText(m Model, s string) Model {
	return send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
}

// The box was a single-line input, so a long prompt scrolled sideways and
// everything but the last line was invisible.
func TestInputGrowsWithTheText(t *testing.T) {
	m := chatModel(t)
	if got := m.TextInput.Height(); got != inputMinHeight {
		t.Fatalf("resting height = %d, want %d", got, inputMinHeight)
	}

	// Alt+Enter inserts a newline instead of submitting.
	for i := 0; i < 3; i++ {
		m = typeText(m, "a line of the prompt")
		m = send(m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	}
	if m.TextInput.Height() <= inputMinHeight {
		t.Errorf("input did not grow: height %d with %d lines", m.TextInput.Height(), m.TextInput.LineCount())
	}
	if m.TextInput.Height() > inputMaxHeight {
		t.Errorf("input grew past the cap: %d > %d", m.TextInput.Height(), inputMaxHeight)
	}
}

// Enter still sends; a newline needs alt+enter or ctrl+j.
func TestEnterSendsAndAltEnterAddsALine(t *testing.T) {
	m := chatModel(t)
	m = typeText(m, "first line")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	m = typeText(m, "second line")

	if m.TextInput.LineCount() < 2 {
		t.Fatalf("alt+enter did not insert a newline: %q", m.TextInput.Value())
	}
	if !strings.Contains(m.TextInput.Value(), "first line") {
		t.Errorf("text lost: %q", m.TextInput.Value())
	}
}

// Standard editing keys must reach the input rather than being swallowed.
func TestEditingShortcutsReachTheInput(t *testing.T) {
	m := chatModel(t)
	m = typeText(m, "delete all of this")
	if m.TextInput.Value() == "" {
		t.Fatal("typing did not register")
	}

	// ctrl+u clears to the start of the line.
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlU})
	if v := strings.TrimSpace(m.TextInput.Value()); v != "" {
		t.Errorf("ctrl+u did not clear the line, left %q", v)
	}

	// ctrl+w deletes the previous word.
	m = typeText(m, "keep this word")
	m = send(m, tea.KeyMsg{Type: tea.KeyCtrlW})
	if strings.Contains(m.TextInput.Value(), "word") {
		t.Errorf("ctrl+w did not delete the last word: %q", m.TextInput.Value())
	}
}

// Arrows move the cursor inside a multi-line prompt, and only recall history
// from the edges — otherwise editing line two jumps to an old prompt.
func TestArrowsEditMultilineBeforeRecallingHistory(t *testing.T) {
	m := chatModel(t)
	m.History = []string{"an earlier prompt"}
	m.HistoryIndex = 1

	m = typeText(m, "line one")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	m = typeText(m, "line two")

	// On the last line, Up should move within the text, not load history.
	m = send(m, tea.KeyMsg{Type: tea.KeyUp})
	if strings.Contains(m.TextInput.Value(), "an earlier prompt") {
		t.Error("Up recalled history while editing a multi-line prompt")
	}

	// From a single-line, first-line cursor, Up recalls history.
	fresh := chatModel(t)
	fresh.History = []string{"an earlier prompt"}
	fresh.HistoryIndex = 1
	fresh = send(fresh, tea.KeyMsg{Type: tea.KeyUp})
	if !strings.Contains(fresh.TextInput.Value(), "an earlier prompt") {
		t.Errorf("Up on an empty prompt should recall history, got %q", fresh.TextInput.Value())
	}
}

// Typing while the agent works was impossible: every key in a busy mode was
// swallowed, so a thought had to wait for the run to end.
func TestCanTypeWhileTheAgentIsWorking(t *testing.T) {
	m := chatModel(t)
	m.Mode = ModeExecuting

	m = typeText(m, "also add dark mode")
	if !strings.Contains(m.TextInput.Value(), "dark mode") {
		t.Fatalf("typing was swallowed while busy: %q", m.TextInput.Value())
	}

	// Enter queues rather than sending into the middle of a run.
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.QueuedPrompt != "also add dark mode" {
		t.Fatalf("Enter did not queue the message: %q", m.QueuedPrompt)
	}
	if m.TextInput.Value() != "" {
		t.Error("the box should clear once the message is queued")
	}
	if m.Mode != ModeExecuting {
		t.Error("queueing must not interrupt the running task")
	}

	// The transcript says what happened.
	var sawNotice bool
	for _, item := range m.Messages {
		if item.Role == "system" && strings.Contains(item.Content, "Queued") {
			sawNotice = true
		}
	}
	if !sawNotice {
		t.Error("no confirmation that the message was queued")
	}
}

// When the run finishes, the queued message goes out on its own.
func TestQueuedMessageIsSentWhenTheRunFinishes(t *testing.T) {
	m := chatModel(t)
	m.Mode = ModeExecuting
	m.QueuedPrompt = "now write the tests"

	updated, cmd := m.Update(execDoneMsg{summary: "Added the feature."})
	m = updated.(Model)

	if cmd == nil {
		t.Fatal("finishing a run with a queued message should start it")
	}
	if m.QueuedPrompt != "" {
		t.Error("the queue should be emptied once dispatched")
	}
	var sawUserTurn bool
	for _, item := range m.Messages {
		if item.Role == "user" && item.Content == "now write the tests" {
			sawUserTurn = true
		}
	}
	if !sawUserTurn {
		t.Error("the queued message should appear as the next user turn")
	}
	if len(m.History) == 0 || m.History[len(m.History)-1] != "now write the tests" {
		t.Error("a dispatched message should join the prompt history")
	}
}

// Interrupting hands the queued text back for editing instead of discarding it.
func TestInterruptReturnsQueuedTextForEditing(t *testing.T) {
	m := chatModel(t)
	m.Mode = ModeExecuting
	m.QueuedPrompt = "actually, use Postgres"

	m = send(m, tea.KeyMsg{Type: tea.KeyEsc})

	if m.QueuedPrompt != "" {
		t.Error("the queue should be cleared on interrupt")
	}
	if m.TextInput.Value() != "actually, use Postgres" {
		t.Errorf("queued text should return to the box, got %q", m.TextInput.Value())
	}
}

// Nothing queued means nothing extra happens when a run ends.
func TestNoQueuedMessageIsANoOp(t *testing.T) {
	m := chatModel(t)
	m.Mode = ModeExecuting

	updated, _ := m.Update(execDoneMsg{summary: "Done."})
	m = updated.(Model)

	if m.Mode != ModeChat {
		t.Errorf("mode after completion = %v, want chat", m.Mode)
	}
	for _, item := range m.Messages {
		if item.Role == "user" {
			t.Errorf("a user turn appeared from nowhere: %q", item.Content)
		}
	}
}
