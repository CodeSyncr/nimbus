package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/CodeSyncr/nimbus/internal/ai"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

/*
The /settings screen.

Every option was previously a flag or an environment variable, which meant a
preference had to be retyped on every run and there was no way to see what the
current answers were. This shows them, changes them, and says where each one
came from.

The rows are generated from ai.SettingDefs, so a setting added to that registry
appears here, in the search, and in the file on disk without this file being
touched — the same arrangement as the slash-command palette.

Provenance is the part a plain list cannot do. "I set that to false, why is it
true?" is nearly always a project file overriding a user file, so each row says
which layer decided it.
*/

// settingsState is the screen's own state. It is grouped rather than spread
// across Model because none of it means anything outside this screen.
type settingsState struct {
	// values is the resolved configuration, re-read after every change so the
	// list shows what is actually in force rather than what was just typed.
	values ai.Settings
	// index is the highlighted row, within the filtered list.
	index int
	// search filters the list; searching is whether the box has focus.
	search    string
	searching bool
	// scope is where a change is written. Reading merges every layer; writing
	// has to pick one, and picking silently is how a change lands in a file
	// the user did not expect.
	scope ai.SettingsScope
	// notice reports the last write, or why it failed.
	notice string
}

// openSettings loads the current configuration and shows the screen.
func (m *Model) openSettings() {
	m.SettingsUI = settingsState{
		values: ai.LoadSettings(m.appRoot()),
		scope:  ai.ScopeUser,
	}
	m.Mode = ModeSettings
}

// appRoot is the project the settings apply to, empty when there is none.
func (m *Model) appRoot() string {
	if m.Agent != nil && m.Agent.Context != nil {
		return m.Agent.Context.AppRoot
	}
	return ""
}

// visibleSettings is the list after the search filter.
//
// The filter matches the label and the key, so both "compact" and
// "auto_compact" find the same row — the label is what is on screen, the key
// is what is in the file.
func (s settingsState) visibleSettings() []ai.SettingDef {
	needle := strings.ToLower(strings.TrimSpace(s.search))
	if needle == "" {
		return ai.SettingDefs
	}
	var out []ai.SettingDef
	for _, d := range ai.SettingDefs {
		if strings.Contains(strings.ToLower(d.Label), needle) ||
			strings.Contains(strings.ToLower(d.Key), needle) {
			out = append(out, d)
		}
	}
	return out
}

// clampIndex keeps the highlight on a row that exists, which it may not after
// the filter narrows.
func (s *settingsState) clampIndex() {
	n := len(s.visibleSettings())
	if n == 0 {
		s.index = 0
		return
	}
	if s.index >= n {
		s.index = n - 1
	}
	if s.index < 0 {
		s.index = 0
	}
}

// updateSettings handles a key while the settings screen is open.
func (m *Model) updateSettings(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := &m.SettingsUI

	// The search box takes ordinary typing; only Esc and Enter leave it, so a
	// setting named "clear" can be searched for without clearing anything.
	if s.searching {
		switch msg.Type {
		case tea.KeyEsc:
			s.searching = false
			s.search = ""
			s.clampIndex()
		case tea.KeyEnter:
			s.searching = false
			s.clampIndex()
		case tea.KeyBackspace:
			if s.search != "" {
				s.search = s.search[:len(s.search)-1]
				s.clampIndex()
			}
		case tea.KeyRunes, tea.KeySpace:
			// Space arrives as KeySpace carrying a rune in current bubbletea
			// and as a bare key type in older ones; taking the runes when
			// there are any covers both without doubling the character.
			if len(msg.Runes) > 0 {
				s.search += string(msg.Runes)
			} else if msg.Type == tea.KeySpace {
				s.search += " "
			}
			s.clampIndex()
		}
		return m, nil
	}

	rows := s.visibleSettings()

	switch strings.ToLower(msg.String()) {
	case "esc", "q":
		m.Mode = ModeChat
		return m, nil

	case "/":
		s.searching = true
		s.search = ""
		return m, nil

	case "up", "k":
		if s.index > 0 {
			s.index--
		}
		return m, nil

	case "down", "j":
		if s.index < len(rows)-1 {
			s.index++
		}
		return m, nil

	case "tab":
		// Cycle where a change is written. Shown in the footer, because a
		// change landing in an unexpected file is worse than no change.
		switch s.scope {
		case ai.ScopeUser:
			s.scope = ai.ScopeProject
		case ai.ScopeProject:
			s.scope = ai.ScopeLocal
		default:
			s.scope = ai.ScopeUser
		}
		return m, nil

	case "enter", " ", "right", "l":
		return m, m.changeSetting(rows, 1)

	case "left", "h":
		return m, m.changeSetting(rows, -1)
	}
	return m, nil
}

// changeSetting advances the highlighted setting and writes it.
//
// The value is written and then the whole configuration is re-read rather than
// assumed: if a stronger layer overrides what was just set, the list has to
// show the value that is actually in force, and the row's source says which
// file is responsible. Reporting the change as applied when a project file
// overrules it would be a lie the user only discovers later.
func (m *Model) changeSetting(rows []ai.SettingDef, delta int) tea.Cmd {
	s := &m.SettingsUI
	if len(rows) == 0 {
		return nil
	}
	def := rows[s.index]

	next := s.values
	def.Next(&next, delta)
	value := def.Value(&next)

	if err := ai.SaveSetting(m.appRoot(), s.scope, def.Key, value); err != nil {
		s.notice = "could not save: " + err.Error()
		return nil
	}

	s.values = ai.LoadSettings(m.appRoot())
	m.applySettings(s.values)

	if got := def.Value(&s.values); got != value {
		s.notice = fmt.Sprintf("%s written to %s, but %s overrides it",
			def.Key, s.scope, ai.SettingSource(m.appRoot(), def.Key))
	} else {
		s.notice = fmt.Sprintf("%s = %v  (saved to %s)", def.Key, value, s.scope)
	}
	return nil
}

// applySettings pushes a change into the running session immediately, so a
// setting takes effect on the next turn rather than the next launch.
func (m *Model) applySettings(s ai.Settings) {
	if m.Agent != nil {
		m.Agent.ApplySettings(s)
	}
	m.ExpandDiffs = s.ExpandDiffs
	m.PlanFirst = s.PlanFirst
}

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

func renderSettingsView(m *Model) string {
	s := &m.SettingsUI
	width := contentWidth(m)
	rows := s.visibleSettings()

	var b strings.Builder
	b.WriteString(sBold.Render(" Settings"))
	b.WriteString(sDim.Render("  ·  " + strings.TrimSuffix(m.appRoot(), "/")))
	b.WriteString("\n\n")

	// Search box, shown while filtering and while a filter is in force.
	if s.searching || s.search != "" {
		box := sMuted.Render(" search ") + sText.Render(s.search)
		if s.searching {
			box += sAccent.Render("▏")
		}
		b.WriteString(box + "\n\n")
	}

	if len(rows) == 0 {
		b.WriteString(sMuted.Render("  nothing matches " + strconv.Quote(s.search)))
		b.WriteString("\n")
		return b.String()
	}

	labelWidth := 0
	for _, d := range rows {
		if len(d.Label) > labelWidth {
			labelWidth = len(d.Label)
		}
	}

	for i, d := range rows {
		selected := i == s.index
		cursor := "  "
		label := sSoft.Render(d.Label)
		if selected {
			cursor = sAccent.Render(" " + glyphPrompt)
			label = sBold.Render(d.Label)
		}

		pad := strings.Repeat(" ", labelWidth-len(d.Label)+3)
		value := d.Display(&s.values)

		valueStyle := sText
		switch value {
		case "true":
			valueStyle = sGreen
		case "false":
			valueStyle = sDim
		}

		line := cursor + " " + label + pad + valueStyle.Render(value)

		// Only a value that did not come from a file is left unannotated;
		// saying "default" on every untouched row is noise.
		if src := ai.SettingSource(m.appRoot(), d.Key); src != ai.ScopeDefault {
			line += sDim.Render("  " + string(src))
		}
		b.WriteString(line + "\n")
	}

	// The highlighted row's help sits below the list rather than beside it:
	// a full sentence per row would not fit a narrow terminal.
	b.WriteString("\n")
	if s.index < len(rows) && rows[s.index].Help != "" {
		b.WriteString(sMuted.Render("  " + wrapTo(rows[s.index].Help, width-4)))
		b.WriteString("\n")
	}
	if s.notice != "" {
		b.WriteString(sYellow.Render("  "+s.notice) + "\n")
	}

	b.WriteString("\n")
	b.WriteString(sDim.Render("  writing to " + string(s.scope) + " · Tab changes where"))
	b.WriteString("\n")
	for _, f := range ai.SettingsFileList(m.appRoot()) {
		b.WriteString(sDim.Render("    "+f) + "\n")
	}
	b.WriteString("\n")
	b.WriteString(sMuted.Render("  Enter/Space change · ←/→ adjust · / search · Esc close"))
	b.WriteString("\n")

	return lipgloss.NewStyle().Width(width).Render(b.String())
}

// wrapTo breaks a line of help text to the available width.
func wrapTo(text string, width int) string {
	if width < 20 || len(text) <= width {
		return text
	}
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		if line != "" && len(line)+1+len(word) > width {
			lines = append(lines, line)
			line = word
			continue
		}
		if line == "" {
			line = word
		} else {
			line += " " + word
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n  ")
}
