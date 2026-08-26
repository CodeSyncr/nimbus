package tui

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// WordBank contains ~50 present-participle verbs mixing coding-flavored actions
// with whimsical generic thinking states.
var WordBank = []string{
	// Whimsical & Thoughtful
	"Pondering", "Noodling", "Percolating", "Marinating", "Ruminating", "Brewing",
	"Cogitating", "Simmering", "Brainstorming", "Contemplating", "Meditating", "Mulling",
	"Deliberating", "Daydreaming", "Envisioning", "Incubating", "Conjuring", "Weaving",
	"Cooking", "Fermenting", "Stewing", "Musing", "Formulating", "Scheming", "Ideating",

	// Coding & Architecture
	"Compiling", "Refactoring", "Untangling", "Scaffolding", "Synthesizing", "Architecting",
	"Parsing", "Optimizing", "Indexing", "Wiring", "Inspecting", "Analyzing", "Structuring",
	"Composing", "Crafting", "Benchmarking", "Validating", "Assembling", "Decoupling",
	"Vectorizing", "Generating", "Calibrating", "Tracing", "Transforming", "Polishing",
	"Resolving", "Orchestrating", "Refining", "Linting", "Provisioning", "Connecting", "Formatting",
}

type thinkingTickMsg struct {
	t time.Time
}

func thinkingTickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return thinkingTickMsg{t: t}
	})
}

// NextRandomVerb returns a random verb from WordBank that does not match lastVerb.
func NextRandomVerb(lastVerb string) string {
	if len(WordBank) == 0 {
		return "Pondering"
	}
	for i := 0; i < 15; i++ {
		candidate := WordBank[rand.Intn(len(WordBank))]
		if candidate != lastVerb {
			return candidate
		}
	}
	for _, w := range WordBank {
		if w != lastVerb {
			return w
		}
	}
	return WordBank[0]
}

// RenderThinkingStatus formats the animated activity line shown above the
// input while the agent works, e.g.
// "☁ ✦ Exploring the codebase… (12s · 340 tokens · esc to interrupt)".
// The agent's current phase is used when known; otherwise a random verb.
func RenderThinkingStatus(m *Model) string {
	if !m.IsThinking {
		return ""
	}

	label := strings.TrimRight(m.Phase, "…. ")
	if label == "" {
		label = m.ThinkingVerb
		if label == "" {
			label = "Pondering"
		}
	}
	if m.StatusText != "" {
		label += sDim.Render(" · " + m.StatusText)
	}

	elapsed := int(time.Since(m.ThinkingStartTime).Seconds())
	if elapsed < 0 {
		elapsed = 0
	}

	parts := []string{fmt.Sprintf("%ds", elapsed)}
	if m.ThinkingTokens > 0 {
		parts = append(parts, fmt.Sprintf("%d tokens", m.ThinkingTokens))
	}
	if m.ToolCalls > 0 {
		parts = append(parts, fmt.Sprintf("%d tool calls", m.ToolCalls))
	}
	parts = append(parts, "esc to interrupt")

	return fmt.Sprintf("%s %s %s", m.Spinner.View(), sMuted.Render(label+"…"), sDim.Render("("+strings.Join(parts, " · ")+")"))
}

// EstimateDeltaTokens approximates token count for a stream delta string.
func EstimateDeltaTokens(delta string) int {
	if len(delta) == 0 {
		return 0
	}
	words := len(strings.Fields(delta))
	byChar := (len(delta) + 3) / 4
	if words > byChar {
		return words
	}
	if byChar < 1 {
		return 1
	}
	return byChar
}
