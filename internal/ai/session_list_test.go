package ai

import (
	"strings"
	"testing"
	"time"
)

// A session is recognised by what was asked, not by its id — an id is only
// useful once you already know which session you want.
func TestSessionDescribesItselfByItsPrompt(t *testing.T) {
	now := time.Now()
	s := NewSession("optimal")
	s.InitialQuery = "add a comments   resource\nto posts"
	s.UpdatedAt = now.Add(-90 * time.Minute)
	s.Turns = []TurnRecord{{Prompt: "a"}, {Prompt: "b"}}
	s.Usage = SessionUsage{InputTokens: 12000, OutputTokens: 3000}

	got := s.Describe(now)
	if !strings.Contains(got, "add a comments resource to posts") {
		t.Errorf("the prompt is not readable in the line: %q", got)
	}
	if !strings.Contains(got, "1h ago") {
		t.Errorf("the age is missing: %q", got)
	}
	if !strings.Contains(got, "2 turns") {
		t.Errorf("the amount of work is missing: %q", got)
	}
	if !strings.Contains(got, s.ID) {
		t.Errorf("the id is missing, so it cannot be resumed: %q", got)
	}
}

// A session with no prompt still has to render.
func TestSessionWithoutAPromptStillDescribes(t *testing.T) {
	s := NewSession("optimal")
	s.UpdatedAt = time.Now()
	got := s.Describe(time.Now())
	if !strings.Contains(got, "(no prompt)") {
		t.Errorf("got %q", got)
	}
	if !strings.Contains(got, "0 turns") {
		t.Errorf("a fresh session should report no turns: %q", got)
	}

	// One turn is singular, more than one is not.
	s.Turns = []TurnRecord{{Prompt: "only"}}
	one := s.Describe(time.Now())
	if !strings.Contains(one, "1 turn") || strings.Contains(one, "1 turns") {
		t.Errorf("turn count is not singular: %q", one)
	}
}

// --continue needs the most recent session without anyone typing an id.
func TestLatestSessionPicksTheNewest(t *testing.T) {
	dir := t.TempDir()

	if got, err := LatestSession(dir); err != nil || got != nil {
		t.Fatalf("an empty project gave (%v, %v), want (nil, nil)", got, err)
	}

	older := NewSession("optimal")
	older.InitialQuery = "the older one"
	if err := SaveSession(dir, older); err != nil {
		t.Fatal(err)
	}

	newer := NewSession("optimal")
	newer.InitialQuery = "the newer one"
	if err := SaveSession(dir, newer); err != nil {
		t.Fatal(err)
	}
	// SaveSession stamps UpdatedAt, so make the ordering unambiguous.
	newer.UpdatedAt = time.Now().Add(time.Hour)
	if err := SaveSession(dir, newer); err != nil {
		t.Fatal(err)
	}

	got, err := LatestSession(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ID != newer.ID {
		t.Errorf("resumed the wrong session: %+v", got)
	}

	all, err := ListSessions(dir)
	if err != nil || len(all) != 2 {
		t.Fatalf("ListSessions gave %d sessions, err %v", len(all), err)
	}
}

// The turn count is singular for one and plural otherwise; the age reads the
// way it would be spoken.
func TestAgeReadsNaturally(t *testing.T) {
	cases := map[time.Duration]string{
		20 * time.Second: "just now",
		25 * time.Minute: "25m ago",
		5 * time.Hour:    "5h ago",
		50 * time.Hour:   "2d ago",
	}
	for d, want := range cases {
		if got := humaniseSince(d); got != want {
			t.Errorf("humaniseSince(%s) = %q, want %q", d, got, want)
		}
	}
}
