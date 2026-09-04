package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/CodeSyncr/nimbus/internal/ai"
	tea "github.com/charmbracelet/bubbletea"
)

// scrollModel builds a chat with more transcript than fits on screen.
func scrollModel(t *testing.T, lines int) Model {
	t.Helper()
	dir := t.TempDir()
	agent := ai.NewAgent(nil, ai.NewToolExecutor(dir), &ai.ProjectContext{AppRoot: dir}, ai.NewSession("optimal"))
	m := NewModel(agent, "", false)
	m.Width, m.Height, m.Ready = 100, 20, true
	m.Viewport.Width, m.Viewport.Height = 100, 10

	for i := 0; i < lines; i++ {
		m.say(ChatItem{Role: "assistant", Content: fmt.Sprintf("transcript line %d", i), Timestamp: time.Now()})
	}
	m.updateViewportContent()
	return m
}

// The wheel is the obvious way to scroll, and the alt screen has no terminal
// scrollback to fall back on, so it has to reach the viewport.
func TestMouseWheelScrollsTheTranscript(t *testing.T) {
	m := scrollModel(t, 80)
	if !m.Viewport.AtBottom() {
		t.Fatal("the transcript did not start at the tail")
	}

	for i := 0; i < 3; i++ {
		m = send(m, tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	}
	if m.Viewport.AtBottom() {
		t.Error("the wheel did not scroll the transcript")
	}

	for i := 0; i < 10; i++ {
		m = send(m, tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	}
	if !m.Viewport.AtBottom() {
		t.Error("scrolling back down did not reach the tail")
	}
}

// A click is not a scroll: leaving other mouse events alone keeps the
// terminal's own behaviour intact.
func TestNonWheelMouseEventsDoNotScroll(t *testing.T) {
	m := scrollModel(t, 80)
	m.Viewport.LineUp(5)
	before := m.Viewport.YOffset

	m = send(m, tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = send(m, tea.MouseMsg{Button: tea.MouseButtonNone, Action: tea.MouseActionMotion})

	if m.Viewport.YOffset != before {
		t.Errorf("a click moved the transcript from %d to %d", before, m.Viewport.YOffset)
	}
}

// The transcript used to jump back to the tail on every new line, so reading
// anything while the agent worked was impossible.
func TestNewOutputDoesNotYankAScrolledReaderDown(t *testing.T) {
	m := scrollModel(t, 80)
	m.Viewport.LineUp(6)
	parked := m.Viewport.YOffset
	if m.Viewport.AtBottom() {
		t.Fatal("the fixture is still at the tail")
	}

	// Whatever arrives next must not move the reader.
	m.say(ChatItem{Role: "tool", Content: "read_file app/models/user.go", Timestamp: time.Now()})
	m.updateViewportContent()

	if m.Viewport.YOffset != parked {
		t.Errorf("new output moved the view from %d to %d", parked, m.Viewport.YOffset)
	}
}

// Following the tail is still the default: a reader who has not scrolled back
// keeps seeing the newest output.
func TestTailIsFollowedWhenAlreadyAtTheBottom(t *testing.T) {
	m := scrollModel(t, 80)
	for i := 0; i < 5; i++ {
		m.say(ChatItem{Role: "assistant", Content: fmt.Sprintf("later line %d", i), Timestamp: time.Now()})
		m.updateViewportContent()
	}
	if !m.Viewport.AtBottom() {
		t.Error("the transcript stopped following the tail")
	}
	if !strings.Contains(m.Viewport.View(), "later line 4") {
		t.Error("the newest line is not on screen")
	}
}

// Sending a message says the reader is done looking at history.
func TestSubmittingReturnsToTheTail(t *testing.T) {
	m := scrollModel(t, 80)
	m.Viewport.LineUp(8)
	if m.Viewport.AtBottom() {
		t.Fatal("the fixture is still at the tail")
	}

	m.TextInput.SetValue("carry on")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})

	if !m.Viewport.AtBottom() {
		t.Error("sending a message left the view scrolled back")
	}
}

// Shift+arrows scroll a line at a time; plain arrows still recall history, and
// letters still type.
func TestShiftArrowsScrollWithoutStealingTheArrowKeys(t *testing.T) {
	m := scrollModel(t, 80)
	m.History = []string{"an earlier prompt"}
	m.HistoryIndex = 1

	shiftUp := tea.KeyMsg{Type: tea.KeyShiftUp}
	if shiftUp.String() != "shift+up" {
		t.Fatalf("this bubbletea spells the key %q, which the handler does not match", shiftUp.String())
	}

	before := m.Viewport.YOffset
	m = send(m, shiftUp)
	if m.Viewport.YOffset >= before {
		t.Errorf("shift+up did not scroll: %d -> %d", before, m.Viewport.YOffset)
	}
	if m.TextInput.Value() != "" {
		t.Errorf("shift+up recalled history instead of scrolling: %q", m.TextInput.Value())
	}

	// A plain up-arrow is still history recall.
	m = send(m, tea.KeyMsg{Type: tea.KeyUp})
	if m.TextInput.Value() != "an earlier prompt" {
		t.Errorf("plain ↑ no longer recalls history, got %q", m.TextInput.Value())
	}
}

// A reader who has scrolled back needs to be told why output stopped moving.
func TestFooterExplainsBeingScrolledBack(t *testing.T) {
	m := scrollModel(t, 80)
	if scrollHint(&m) != "" {
		t.Error("a transcript at the tail should say nothing about scrolling")
	}

	m.Viewport.LineUp(20)
	hint := scrollHint(&m)
	if hint == "" {
		t.Fatal("a scrolled-back transcript gives the reader no explanation")
	}
	if !strings.Contains(hint, "follow again") {
		t.Errorf("the hint does not say how to get back: %q", hint)
	}
	if !strings.Contains(renderFooter(&m), "follow again") {
		t.Error("the hint never reaches the footer")
	}
}
