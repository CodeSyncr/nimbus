package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CodeSyncr/nimbus/internal/ai"
	tea "github.com/charmbracelet/bubbletea"
)

// settingsModel builds a model over an isolated home and project, so a test
// never reads or writes the developer's real settings.
func settingsModel(t *testing.T) (*Model, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("NIMBUS_CONTEXT_LIMIT", "")

	app := t.TempDir()
	agent := ai.NewAgent(nil, ai.NewToolExecutor(app), &ai.ProjectContext{AppRoot: app}, ai.NewSession("optimal"))
	m := NewModel(agent, "", false)
	m.Width, m.Height, m.Ready = 100, 40, true
	return &m, app
}

func press(m *Model, key string) {
	var msg tea.KeyMsg
	switch key {
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	case "backspace":
		msg = tea.KeyMsg{Type: tea.KeyBackspace}
	case " ":
		msg = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	m.updateSettings(msg)
}

// /settings and /config both open the screen: the command was invisible
// before, and the two names are what people reach for.
func TestSettingsCommandOpensTheScreen(t *testing.T) {
	for _, cmd := range []string{"/settings", "/config"} {
		m, _ := settingsModel(t)
		handled, quit, _ := m.handleSlashCommand(cmd)
		if !handled || quit {
			t.Fatalf("%s was not handled", cmd)
		}
		if m.Mode != ModeSettings {
			t.Errorf("%s did not open the settings screen", cmd)
		}
	}
}

// Changing a row writes it and takes effect immediately, rather than at the
// next launch.
func TestChangingASettingSavesAndApplies(t *testing.T) {
	m, app := settingsModel(t)
	m.openSettings()

	// Find the auto-compact row and highlight it.
	rows := m.SettingsUI.visibleSettings()
	idx := -1
	for i, d := range rows {
		if d.Key == "auto_compact" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("auto_compact is not in the list")
	}
	m.SettingsUI.index = idx

	if !m.SettingsUI.values.AutoCompact {
		t.Fatal("auto_compact did not start true")
	}
	press(m, "enter")

	if m.SettingsUI.values.AutoCompact {
		t.Error("the screen still shows auto_compact as true")
	}
	if m.Agent.Settings().AutoCompact {
		t.Error("the change never reached the agent")
	}

	data, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".nimbus", "settings.json"))
	if err != nil {
		t.Fatalf("nothing was written to the user settings file: %v", err)
	}
	if !strings.Contains(string(data), `"auto_compact": false`) {
		t.Errorf("the file does not record the change:\n%s", data)
	}
	// Only the changed key belongs in the file; writing the whole resolved
	// struct would bake every inherited default into the user's layer.
	if strings.Contains(string(data), "permission_mode") {
		t.Errorf("unrelated settings were written too:\n%s", data)
	}
	_ = app
}

// Tab moves where a change lands, and the screen says where that is.
func TestWriteScopeIsSelectableAndVisible(t *testing.T) {
	m, app := settingsModel(t)
	m.openSettings()

	if m.SettingsUI.scope != ai.ScopeUser {
		t.Fatalf("scope starts at %q, want user", m.SettingsUI.scope)
	}
	if !strings.Contains(renderSettingsView(m), "writing to user") {
		t.Error("the screen does not say where a change will be written")
	}

	press(m, "tab")
	if m.SettingsUI.scope != ai.ScopeProject {
		t.Fatalf("Tab gave scope %q, want project", m.SettingsUI.scope)
	}

	press(m, "enter")
	if _, err := os.Stat(filepath.Join(app, ".nimbus", "settings.json")); err != nil {
		t.Errorf("the change did not land in the project file: %v", err)
	}
}

// A user setting that a project file overrules must not be reported as
// applied — that is the failure the source column exists to explain.
func TestOverriddenWriteIsReportedNotClaimed(t *testing.T) {
	m, app := settingsModel(t)

	if err := os.MkdirAll(filepath.Join(app, ".nimbus"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, ".nimbus", "settings.json"),
		[]byte(`{"model":"deep"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m.openSettings()

	rows := m.SettingsUI.visibleSettings()
	for i, d := range rows {
		if d.Key == "model" {
			m.SettingsUI.index = i
		}
	}
	press(m, "enter") // writes to the user scope, which project overrides

	if m.SettingsUI.values.Model != "deep" {
		t.Errorf("model = %q, want the project's value to still win", m.SettingsUI.values.Model)
	}
	if !strings.Contains(m.SettingsUI.notice, "overrides") {
		t.Errorf("the screen claimed the change applied: %q", m.SettingsUI.notice)
	}
	if !strings.Contains(renderSettingsView(m), "project") {
		t.Error("the row does not say which layer decided it")
	}
}

// Search narrows the list, and typing goes to the box rather than to the
// shortcuts — a setting called "auto-compact" must be searchable without the
// "c" doing something else.
func TestSearchFiltersAndSwallowsTyping(t *testing.T) {
	m, _ := settingsModel(t)
	m.openSettings()
	all := len(m.SettingsUI.visibleSettings())

	press(m, "/")
	if !m.SettingsUI.searching {
		t.Fatal("/ did not focus the search box")
	}
	for _, r := range "compact" {
		press(m, string(r))
	}
	if m.SettingsUI.search != "compact" {
		t.Fatalf("search box holds %q", m.SettingsUI.search)
	}
	if m.Mode != ModeSettings {
		t.Error("typing in the search box left the screen")
	}

	got := m.SettingsUI.visibleSettings()
	if len(got) == 0 || len(got) >= all {
		t.Fatalf("filter matched %d of %d rows", len(got), all)
	}
	for _, d := range got {
		if !strings.Contains(strings.ToLower(d.Label+d.Key), "compact") {
			t.Errorf("%q does not match the search", d.Key)
		}
	}

	press(m, "backspace")
	if m.SettingsUI.search != "compac" {
		t.Errorf("backspace left %q", m.SettingsUI.search)
	}

	// Esc leaves the search, a second Esc closes the screen.
	press(m, "esc")
	if m.SettingsUI.searching || m.Mode != ModeSettings {
		t.Error("Esc should leave the search box without closing the screen")
	}
	press(m, "esc")
	if m.Mode != ModeChat {
		t.Error("Esc did not close the settings screen")
	}
}

// The highlight has to stay on a row that exists once the filter narrows.
func TestHighlightSurvivesAFilter(t *testing.T) {
	m, _ := settingsModel(t)
	m.openSettings()

	for i := 0; i < len(ai.SettingDefs)-1; i++ {
		press(m, "down")
	}
	last := m.SettingsUI.index

	press(m, "/")
	for _, r := range "model" {
		press(m, string(r))
	}
	press(m, "enter") // leave the box, keep the filter

	rows := m.SettingsUI.visibleSettings()
	if m.SettingsUI.index >= len(rows) {
		t.Fatalf("highlight is on row %d of %d after filtering (was %d)",
			m.SettingsUI.index, len(rows), last)
	}
	// Rendering must not panic on the narrowed list either.
	if out := renderSettingsView(m); out == "" {
		t.Error("the filtered screen rendered nothing")
	}
}

// Every row in the registry has to render, with its value and its help.
func TestScreenRendersEverySetting(t *testing.T) {
	m, _ := settingsModel(t)
	m.openSettings()
	out := renderSettingsView(m)

	for _, d := range ai.SettingDefs {
		if !strings.Contains(out, d.Label) {
			t.Errorf("%q is missing from the screen", d.Label)
		}
	}
	if !strings.Contains(out, "Esc close") {
		t.Error("the key hints are missing")
	}
}

// A space in the search box must insert one character, not two: bubbletea
// delivers space as its own key type that also carries the rune.
func TestSearchAcceptsASingleSpace(t *testing.T) {
	m, _ := settingsModel(t)
	m.openSettings()
	press(m, "/")
	for _, r := range []string{"m", "a", "x", " ", "c"} {
		press(m, r)
	}
	if m.SettingsUI.search != "max c" {
		t.Errorf("search box holds %q, want %q", m.SettingsUI.search, "max c")
	}
	if len(m.SettingsUI.visibleSettings()) != 1 {
		t.Errorf("%q matched %d rows, want the one command-output row",
			m.SettingsUI.search, len(m.SettingsUI.visibleSettings()))
	}
}
