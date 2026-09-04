package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Typing "/" opens the menu: commands were previously invisible unless you
// already knew their names.
func TestSlashOpensTheCommandMenu(t *testing.T) {
	all := matchCommands("/")
	if len(all) != len(slashCommands) {
		t.Fatalf("a bare slash listed %d of %d commands", len(all), len(slashCommands))
	}

	// Typing narrows it.
	co := matchCommands("/co")
	if len(co) == 0 {
		t.Fatal("/co matched nothing")
	}
	// A match is justified by the name or by any alias: "/co" reaches
	// /settings through /config, which is the point of having aliases.
	for _, c := range co {
		if !matchedBy(c, "/co") {
			t.Errorf("%q matched /co on neither its name nor an alias", c.Name)
		}
	}
	names := commandNames(co)
	for _, want := range []string{"/context", "/compact"} {
		if !strings.Contains(names, want) {
			t.Errorf("/co should offer %s, got %s", want, names)
		}
	}
}

// The menu is for choosing a command; once one is chosen and arguments are
// being typed it must get out of the way.
func TestMenuClosesForNonCommandsAndArguments(t *testing.T) {
	for _, in := range []string{"", "add a model", "  ", "/compact now", "not/a/command"} {
		if got := matchCommands(in); len(got) != 0 {
			t.Errorf("%q should not open the menu, got %s", in, commandNames(got))
		}
	}
	if got := matchCommands("/zzz"); len(got) != 0 {
		t.Errorf("an unknown command should match nothing, got %s", commandNames(got))
	}
}

// Aliases resolve to their canonical command, so "quit" finds /exit.
func TestAliasesAreDiscoverable(t *testing.T) {
	if got := matchCommands("/qu"); len(got) == 0 || got[0].Name != "/exit" {
		t.Errorf("/qu should find /exit via its alias, got %s", commandNames(got))
	}
}

// Arrow keys move the highlight and Tab completes it.
func TestPaletteNavigationAndCompletion(t *testing.T) {
	m := chatModel(t)
	m = typeText(m, "/c")

	matches := matchCommands(m.TextInput.Value())
	if len(matches) < 2 {
		t.Fatalf("/c should match several commands, got %s", commandNames(matches))
	}

	m = send(m, tea.KeyMsg{Type: tea.KeyDown})
	if m.PaletteIndex != 1 {
		t.Errorf("Down did not move the highlight: %d", m.PaletteIndex)
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyUp})
	if m.PaletteIndex != 0 {
		t.Errorf("Up did not move the highlight back: %d", m.PaletteIndex)
	}

	m = send(m, tea.KeyMsg{Type: tea.KeyTab})
	if m.TextInput.Value() != matches[0].Name {
		t.Errorf("Tab completed to %q, want %q", m.TextInput.Value(), matches[0].Name)
	}
	if m.PaletteIndex != 0 {
		t.Error("the highlight should reset after completing")
	}
}

// Esc dismisses the menu by clearing the half-typed command.
func TestEscapeDismissesThePalette(t *testing.T) {
	m := chatModel(t)
	m = typeText(m, "/comp")
	m = send(m, tea.KeyMsg{Type: tea.KeyEsc})

	if m.TextInput.Value() != "" {
		t.Errorf("Esc should clear the partial command, got %q", m.TextInput.Value())
	}
	if len(matchCommands(m.TextInput.Value())) != 0 {
		t.Error("the menu is still open after Esc")
	}
}

// Typing after moving the highlight restarts the selection, so Enter cannot
// run a command left highlighted from an earlier, wider list.
func TestTypingResetsTheHighlight(t *testing.T) {
	m := chatModel(t)
	m = typeText(m, "/c")
	m = send(m, tea.KeyMsg{Type: tea.KeyDown})
	if m.PaletteIndex == 0 {
		t.Fatal("highlight did not move")
	}
	m = typeText(m, "o")
	if m.PaletteIndex != 0 {
		t.Errorf("typing should reset the highlight, got %d", m.PaletteIndex)
	}
}

// The help output and the menu come from one registry, so they cannot drift.
func TestHelpListsEveryCommand(t *testing.T) {
	help := strings.Join(helpLines(), "\n")
	for _, c := range slashCommands {
		if !strings.Contains(help, c.Name) {
			t.Errorf("/help does not mention %s", c.Name)
		}
		if !strings.Contains(help, c.Summary) {
			t.Errorf("/help does not describe %s", c.Name)
		}
	}
}

func commandNames(cs []SlashCommand) string {
	var names []string
	for _, c := range cs {
		names = append(names, c.Name)
	}
	return strings.Join(names, ", ")
}

// matchedBy reports whether a command answers to a prefix under any of its
// spellings.
func matchedBy(c SlashCommand, prefix string) bool {
	if strings.HasPrefix(c.Name, prefix) {
		return true
	}
	for _, a := range c.Aliases {
		if strings.HasPrefix("/"+strings.TrimPrefix(a, "/"), prefix) {
			return true
		}
	}
	return false
}
