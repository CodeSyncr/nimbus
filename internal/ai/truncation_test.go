package ai

import (
	"strings"
	"testing"
)

// The reported symptom: a plan cut off at the model's output limit was echoed
// to the user as a wall of broken JSON, presented as though it were the answer.
func TestTruncatedPlanIsReportedNotEchoed(t *testing.T) {
	truncated := `{
  "summary": "Reconcile the landing page so markup, CSS and JS agree.",
  "overview": "The component JS was written against a different naming scheme",
  "phases": [
    {"name": "Phase 6: Rewire stats", "description": "Markup has #lstats with .lstat-num[data-count] (no data`

	if !looksLikeTruncatedJSON(truncated) {
		t.Fatal("a plan cut mid-string was not recognised as truncated")
	}

	// Complete JSON, and prose, must not be mistaken for it.
	complete := `{"summary":"done","steps":[]}`
	if looksLikeTruncatedJSON(complete) {
		t.Error("complete JSON flagged as truncated")
	}
	prose := "This project is a Go web app. Routing lives in start/routes.go."
	if looksLikeTruncatedJSON(prose) {
		t.Error("a conversational answer flagged as truncated")
	}
	fenced := "```json\n{\"summary\":\"a\",\"steps\":[]}\n```"
	if looksLikeTruncatedJSON(fenced) {
		t.Error("a fenced complete plan flagged as truncated")
	}

	// A brace inside a string must not be counted as structure.
	stringBrace := `{"summary":"use {curly} braces","steps":[]}`
	if looksLikeTruncatedJSON(stringBrace) {
		t.Error("braces inside a string confused the balance check")
	}
}

// The whole path: a truncated plan surfaces as a clear error rather than as
// the model's "answer".
func TestPlanGenerationSurfacesTruncationClearly(t *testing.T) {
	truncated := `{"summary":"Rewire everything","phases":[{"name":"Phase 1","description":"start`

	client := &scriptedTurnClient{
		responses: map[TurnMode][]*MessageResponse{
			TurnModeExplore: {text("FINDINGS: the components disagree.")},
			TurnModePlan:    {text(truncated)},
		},
	}
	agent := NewAgent(client, NewToolExecutor(t.TempDir()), &ProjectContext{AppRoot: t.TempDir()}, NewSession("optimal"))
	agent.Verifier = nil

	_, err := agent.GeneratePlan(t.Context(), "reconcile the landing page")
	if err == nil {
		t.Fatal("a truncated plan should be an error, not an answer")
	}
	msg := err.Error()
	if !strings.Contains(msg, "incomplete") || !strings.Contains(msg, "AI_MAX_TOKENS") {
		t.Errorf("the error should explain the cause and the fix, got: %v", err)
	}
	if strings.Contains(msg, `"phases"`) {
		t.Error("the raw JSON was echoed back into the error")
	}
}
