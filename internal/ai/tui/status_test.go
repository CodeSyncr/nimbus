package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/CodeSyncr/nimbus/internal/ai"
)

func TestWordBank(t *testing.T) {
	if len(WordBank) < 50 {
		t.Errorf("expected WordBank to contain at least 50 verbs, got %d", len(WordBank))
	}

	seen := make(map[string]bool)
	for _, w := range WordBank {
		if seen[w] {
			t.Errorf("duplicate verb found in WordBank: %s", w)
		}
		seen[w] = true
		if !strings.HasSuffix(w, "ing") {
			t.Errorf("expected verb to be present-participle ending in -ing, got: %s", w)
		}
	}
}

func TestNextRandomVerb(t *testing.T) {
	last := "Pondering"
	for i := 0; i < 50; i++ {
		next := NextRandomVerb(last)
		if next == last {
			t.Errorf("expected NextRandomVerb not to repeat same word twice in a row, got %s twice", next)
		}
		last = next
	}
}

func TestRenderThinkingStatus(t *testing.T) {
	projCtx := &ai.ProjectContext{AppRoot: "/tmp/app"}
	session := ai.NewSession("optimal")
	agent := ai.NewAgent(nil, ai.NewToolExecutor("/tmp/app"), projCtx, session)

	m := NewModel(agent, "", false)
	m.IsThinking = false
	if out := RenderThinkingStatus(&m); out != "" {
		t.Errorf("expected empty string when not thinking, got: %s", out)
	}

	m.IsThinking = true
	m.ThinkingStartTime = time.Now().Add(-12 * time.Second)
	m.ThinkingVerb = "Percolating"
	m.ThinkingTokens = 340

	rendered := RenderThinkingStatus(&m)
	if !strings.Contains(rendered, "Percolating") {
		t.Errorf("expected rendered status to contain verb 'Percolating', got: %s", rendered)
	}
	if !strings.Contains(rendered, "12s") {
		t.Errorf("expected rendered status to contain '12s', got: %s", rendered)
	}
	if !strings.Contains(rendered, "340 tokens") {
		t.Errorf("expected rendered status to contain '340 tokens', got: %s", rendered)
	}
	if !strings.Contains(rendered, "☁") {
		t.Errorf("expected rendered status to contain cloud icon '☁', got: %s", rendered)
	}
}

func TestEstimateDeltaTokens(t *testing.T) {
	if n := EstimateDeltaTokens(""); n != 0 {
		t.Errorf("expected 0 tokens for empty string, got %d", n)
	}
	if n := EstimateDeltaTokens("hello world"); n < 2 {
		t.Errorf("expected at least 2 tokens for 'hello world', got %d", n)
	}
}
