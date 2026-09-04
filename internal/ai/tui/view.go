package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/CodeSyncr/nimbus/internal/version"
	"github.com/charmbracelet/lipgloss"
)

func (m Model) View() string {
	if !m.Ready || m.Width == 0 || m.Height == 0 {
		return sAccentBold.Render(glyphAI+" Nimbus AI") + sMuted.Render("  starting…")
	}

	headerView := renderHeader(&m)
	footerView := renderFooter(&m)

	var body string
	switch m.Mode {
	case ModeClarification:
		body = renderQuestionView(&m)
	case ModeCommandApproval:
		body = renderApprovalView(&m)
	case ModeSettings:
		body = renderSettingsView(&m)
	default:
		m.Viewport.Height = m.Height - lipgloss.Height(headerView) - lipgloss.Height(footerView)
		if m.Viewport.Height < 4 {
			m.Viewport.Height = 4
		}
		body = m.Viewport.View()
	}

	return lipgloss.JoinVertical(lipgloss.Left, headerView, body, footerView)
}

// scrollHint describes the transcript's position when it is not at the tail.
func scrollHint(m *Model) string {
	if m.Viewport.AtBottom() {
		return ""
	}
	switch m.Mode {
	case ModeChat, ModePlanning, ModeExecuting, ModeCompleted:
		return fmt.Sprintf("↑ scrolled back %d%% · Shift+↓ or PgDn to follow again",
			100-int(m.Viewport.ScrollPercent()*100))
	}
	return ""
}

func contentWidth(m *Model) int {
	w := m.Width - 2
	if w < 40 {
		w = 40
	}
	return w
}

func renderHeader(m *Model) string {
	width := contentWidth(m)

	vStr := "v" + strings.TrimPrefix(version.Nimbus, "v")
	left := " " + sAccentBold.Render(glyphAI+" Nimbus AI") + " " + sDim.Render(vStr)

	projName := "nimbus-app"
	branch := "main"
	var modules []string
	if m.Agent != nil && m.Agent.Context != nil {
		if m.Agent.Context.ProjectName != "" {
			projName = m.Agent.Context.ProjectName
		}
		if m.Agent.Context.GitBranch != "" {
			branch = m.Agent.Context.GitBranch
		}
		modules = m.Agent.Context.NimbusModules
	}
	info := sMuted.Render("  ·  ") + sBold.Render(projName) + sMuted.Render("  "+glyphBranch+" "+branch)
	if len(modules) > 0 {
		var pills []string
		for i, mod := range modules {
			if i >= 4 {
				pills = append(pills, sDim.Render(fmt.Sprintf("+%d", len(modules)-4)))
				break
			}
			pills = append(pills, sAccent.Render("["+mod+"]"))
		}
		info += "  " + strings.Join(pills, " ")
	}

	var badge string
	switch m.Mode {
	case ModePlanning:
		badge = sYellow.Bold(true).Render(m.Spinner.View() + " PLANNING")
	case ModeExecuting:
		badge = sGreen.Bold(true).Render(m.Spinner.View() + " EXECUTING")
	case ModePlanReview:
		badge = sAccentBold.Render(glyphDot + " REVIEW PLAN")
	case ModeClarification:
		badge = sBlue.Bold(true).Render(glyphDot + " QUESTION")
	case ModeCommandApproval:
		badge = sYellow.Bold(true).Render(glyphDot + " APPROVE COMMAND")
	default:
		badge = sMuted.Render(glyphDot + " CHAT")
	}

	if gauge := renderContextGauge(m); gauge != "" {
		badge = gauge + sDim.Render("  ") + badge
	}

	lineLeft := left + info
	space := width - lipgloss.Width(lineLeft) - lipgloss.Width(badge) - 1
	if space < 1 {
		space = 1
	}
	line := lineLeft + strings.Repeat(" ", space) + badge
	return line + "\n" + sDivider.Render(strings.Repeat("─", width))
}

func renderFooter(m *Model) string {
	width := contentWidth(m)
	var parts []string

	parts = append(parts, sDivider.Render(strings.Repeat("─", width)))

	if m.IsThinking {
		if line := RenderThinkingStatus(m); line != "" {
			parts = append(parts, " "+line)
		}
	} else if m.StatusText != "" {
		parts = append(parts, " "+sMuted.Render(m.StatusText))
	}

	// Scrolled back: say so. The transcript stops following the tail while a
	// reader is looking at something further up, and without a word here that
	// is indistinguishable from the agent having gone quiet.
	if scrollHint(m) != "" {
		parts = append(parts, " "+sYellow.Render(scrollHint(m)))
	}

	switch m.Mode {
	case ModePlanReview:
		parts = append(parts, " "+strings.Join([]string{
			sKey.Render("Enter") + sMuted.Render(" approve & run"),
			sKey.Render("Esc") + sMuted.Render(" reject"),
			sKey.Render("↑/↓") + sMuted.Render(" scroll"),
		}, sDim.Render("   ·   ")))
	case ModeCommandApproval:
		parts = append(parts, " "+strings.Join([]string{
			sKey.Render("y") + sMuted.Render(" run it once"),
			sKey.Render("n") + sMuted.Render(" refuse"),
			sKey.Render("Esc") + sMuted.Render(" refuse"),
		}, sDim.Render("   ·   ")))
	case ModeClarification:
		parts = append(parts, " "+strings.Join([]string{
			sKey.Render("↑/↓") + sMuted.Render(" choose"),
			sKey.Render("1-9") + sMuted.Render(" quick pick"),
			sKey.Render("c") + sMuted.Render(" custom"),
			sKey.Render("Enter") + sMuted.Render(" next"),
			sKey.Render("Esc") + sMuted.Render(" cancel"),
		}, sDim.Render("   ·   ")))
	default:
		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cAccent).
			Padding(0, 1).
			Width(width - 2)
		if m.Mode.busy() {
			box = box.BorderForeground(cBorder)
		}
		if palette := renderCommandPalette(m); palette != "" {
			parts = append(parts, palette)
		}
		parts = append(parts, box.Render(m.TextInput.View()))

		var hints string
		if m.Mode.busy() {
			hints = sMuted.Render("Esc") + sDim.Render(" interrupt   ") + sMuted.Render("PgUp/PgDn") + sDim.Render(" scroll")
			if m.QueuedPrompt != "" {
				hints += sDim.Render("   ") + sYellow.Render("1 queued")
			} else {
				hints += sDim.Render("   type to queue a follow-up")
			}
		} else {
			hints = sMuted.Render("/help") + sDim.Render("  ·  ") + sMuted.Render("/context") + sDim.Render("  ·  ") + sMuted.Render("/compact") + sDim.Render("  ·  ") + sMuted.Render("/clear") + sDim.Render("  ·  ") + sMuted.Render("/exit")
		}
		right := sDim.Render("↵ send  ↑/↓ history")
		space := width - lipgloss.Width(hints) - lipgloss.Width(right) - 2
		if space < 1 {
			space = 1
		}
		parts = append(parts, " "+hints+strings.Repeat(" ", space)+right)
	}

	return strings.Join(parts, "\n")
}

const (
	// maxInlineDiffLines is how much of a diff is shown inline once the user
	// expands it (ctrl+o).
	maxInlineDiffLines = 40
	// collapsedDiffLines is the default. A change is announced with its shape
	// — "+12 −3" and a few representative lines — rather than pasted in full,
	// so a run of edits stays readable instead of scrolling the transcript
	// away. The whole diff is one keystroke away.
	collapsedDiffLines = 6
)

func renderChatHistory(m *Model) string {
	var sb strings.Builder
	width := contentWidth(m)
	textWidth := width - 4
	if textWidth < 30 {
		textWidth = 30
	}
	wrap := lipgloss.NewStyle().Width(textWidth)

	for _, item := range m.Messages {
		switch item.Role {
		case "user":
			sb.WriteString("\n")
			sb.WriteString(sAccentBold.Render(glyphPrompt+" ") + wrap.Inherit(sBold).Render(item.Content))
			sb.WriteString("\n")

		case "phase":
			label := item.Content
			if item.Elapsed > 0 {
				// "Thought for 8s" reads as a record of what just happened;
				// other phases keep their label and a trailing duration.
				if label == "Thought" {
					label = fmt.Sprintf("Thought for %s", fmtDuration(item.Elapsed))
				} else {
					label += sDim.Render(fmt.Sprintf("  %s", fmtDuration(item.Elapsed)))
				}
			}
			sb.WriteString("\n  " + sMuted.Render(glyphPhase+" ") + sMuted.Italic(true).Render(label) + "\n")

		case "tool":
			sb.WriteString(renderToolLine(item, textWidth, m.AppRoot(), m.ExpandDiffs))

		case "assistant":
			sb.WriteString("\n")
			body := RenderMarkdown(item.Content, textWidth)
			sb.WriteString(sAccentBold.Render(glyphAI+" ") + indentLines(wrap.Render(body), 2, true))
			sb.WriteString("\n")
			for _, d := range item.Diffs {
				sb.WriteString(indentLines(RenderColorizedDiff(d), 2, false) + "\n")
			}

		case "error":
			sb.WriteString("\n" + sRed.Bold(true).Render(glyphErr+" ") + wrap.Inherit(sRed).Render(item.Content) + "\n")

		case "system":
			sb.WriteString("\n" + indentLines(wrap.Inherit(sMuted).Render(item.Content), 2, false) + "\n")
		}
	}

	// Text the model is streaming right now.
	if m.Mode.busy() && m.StreamBuffer != nil && strings.TrimSpace(m.StreamBuffer.String()) != "" {
		sb.WriteString("\n" + sAccentBold.Render(glyphAI+" ") + indentLines(wrap.Render(RenderMarkdown(m.StreamBuffer.String(), textWidth)), 2, true) + "\n")
	}

	return strings.TrimRight(sb.String(), "\n") + "\n"
}

// renderToolLine renders "  ● Read main.go  120 lines" plus an inline diff
// for file changes, in the style of Claude Code's tool activity.
func renderToolLine(item ChatItem, textWidth int, appRoot string, expanded bool) string {
	verb := toolVerb(item.ToolName, item.ToolArgs)
	target := item.Content
	if target == "" {
		target = toolTarget(item.ToolArgs)
	}

	dot := sGreen.Render(glyphDot)
	if item.IsError {
		dot = sRed.Render(glyphDot)
	} else if verb == "Read" || verb == "Search" || verb == "Glob" || verb == "List" || verb == "Skill" {
		dot = sBlue.Render(glyphDot)
	} else if verb == "Bash" {
		dot = sPurple.Render(glyphDot)
	}

	maxTarget := textWidth - len(verb) - 24
	if maxTarget < 20 {
		maxTarget = 20
	}
	if lipgloss.Width(target) > maxTarget {
		target = target[:maxTarget-1] + "…"
	}

	// The path is clickable where the terminal supports it: link the escape
	// around the styled text, after truncation, so widths stay correct.
	shown := sText.Render(target)
	if isFileTool(item.ToolName) {
		shown = linkFile(appRoot, rawToolPath(item), shown)
	}
	line := "  " + dot + " " + sBold.Render(verb) + " " + shown
	if item.Detail != "" {
		if item.IsError {
			line += "  " + sRed.Render(item.Detail)
		} else {
			line += "  " + sDim.Render(item.Detail)
		}
	}
	out := line + "\n"

	if item.Diff != "" {
		limit := collapsedDiffLines
		if expanded {
			limit = maxInlineDiffLines
		}
		added, removed, total := diffShape(item.Diff)
		summary := fmt.Sprintf("%s %s", sGreen.Render("+"+strconv.Itoa(added)), sRed.Render("−"+strconv.Itoa(removed)))
		if !expanded && total > limit {
			summary += sDim.Render(fmt.Sprintf("  · %d lines, ctrl+o to expand", total))
		}
		out += "    " + summary + "\n"
		out += indentLines(renderCompactDiff(item.Diff, limit), 4, false) + "\n"
	}
	return out
}

// isFileTool reports whether a tool's target is a path worth linking.
func isFileTool(name string) bool {
	switch strings.ToLower(name) {
	case "read_file", "read", "write_file", "create_file", "create", "write",
		"edit_file", "edit", "delete_file", "delete", "list_dir":
		return true
	}
	return false
}

// rawToolPath returns the untruncated path from the tool's arguments, which is
// what a link must point at even when the display text is elided.
func rawToolPath(item ChatItem) string {
	if p, ok := item.ToolArgs["path"].(string); ok && p != "" {
		return p
	}
	return item.Content
}

// diffShape counts added and removed lines.
func diffShape(diff string) (added, removed, total int) {
	for _, l := range strings.Split(strings.TrimRight(diff, "\n"), "\n") {
		if strings.HasPrefix(l, "+++") || strings.HasPrefix(l, "---") {
			continue
		}
		switch {
		case strings.HasPrefix(l, "+"):
			added++
			total++
		case strings.HasPrefix(l, "-"):
			removed++
			total++
		default:
			total++
		}
	}
	return added, removed, total
}

// renderCompactDiff shows only changed lines (with a little context), capped.
func renderCompactDiff(diff string, maxLines int) string {
	var lines []string
	for _, l := range strings.Split(strings.TrimRight(diff, "\n"), "\n") {
		if strings.HasPrefix(l, "+++") || strings.HasPrefix(l, "---") {
			continue
		}
		switch {
		case strings.HasPrefix(l, "+"):
			lines = append(lines, sGreen.Render(l))
		case strings.HasPrefix(l, "-"):
			lines = append(lines, sRed.Render(l))
		default:
			lines = append(lines, sDim.Render(l))
		}
	}
	if len(lines) > maxLines {
		rest := len(lines) - maxLines
		lines = append(lines[:maxLines], sDim.Render(fmt.Sprintf("… %d more lines", rest)))
	}
	return strings.Join(lines, "\n")
}

func indentLines(s string, n int, skipFirst bool) string {
	pad := strings.Repeat(" ", n)
	lines := strings.Split(s, "\n")
	for i := range lines {
		if i == 0 && skipFirst {
			continue
		}
		lines[i] = pad + lines[i]
	}
	return strings.Join(lines, "\n")
}

func fmtDuration(d time.Duration) string {
	if d < time.Second {
		return "<1s"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
}

// renderApprovalView asks about a command the policy flagged. It shows the
// exact command and why it was stopped, so the answer is an informed one
// rather than a reflex "yes".
func renderApprovalView(m *Model) string {
	width := contentWidth(m)
	p := m.PendingApproval
	if p == nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n  " + sYellow.Bold(true).Render("The agent wants to run a command that needs your approval") + "\n\n")
	sb.WriteString("  " + sMuted.Render("Command") + "\n")
	sb.WriteString("  " + sYellow.Render(p.Command) + "\n\n")
	sb.WriteString("  " + sMuted.Render("Why it stopped") + "\n")
	sb.WriteString("  " + sText.Render(p.Reason) + "\n\n")
	sb.WriteString("  " + sDim.Render("Approval covers this one command. Refusing tells the agent to find another way.") + "\n")

	return lipgloss.NewStyle().Width(width).Render(sb.String())
}

// contextRing steps through the quarter-filled circles, so how full the
// context is reads at a glance without taking a column of the header.
var contextRing = []string{"○", "◔", "◑", "◕", "●"}

// renderContextGauge shows how much of the context window is in use and how
// close the conversation is to being compacted automatically.
func renderContextGauge(m *Model) string {
	if m.Agent == nil || m.Agent.Session == nil {
		return ""
	}
	usage := m.Agent.Session.ContextUsage()
	if usage.Tokens == 0 {
		return ""
	}

	pct := usage.Percent()
	glyph := contextRing[len(contextRing)*pct/101]

	style := sDim
	switch {
	case pct >= usage.Threshold():
		style = sYellow // compaction is imminent
	case pct >= 60:
		style = sMuted
	}
	return style.Render(fmt.Sprintf("%s %d%%", glyph, pct))
}

// renderCommandPalette draws the command menu above the input while what has
// been typed still looks like a command being chosen.
func renderCommandPalette(m *Model) string {
	if m.Mode != ModeChat {
		return ""
	}
	matches := matchCommands(m.TextInput.Value())
	if len(matches) == 0 {
		return ""
	}

	width := contentWidth(m) - 2
	nameWidth := 0
	for _, c := range matches {
		if len(c.Name) > nameWidth {
			nameWidth = len(c.Name)
		}
	}

	var rows []string
	for i, c := range matches {
		name := c.Name + strings.Repeat(" ", nameWidth-len(c.Name))
		row := "  " + name + "   " + c.Summary
		if i == m.PaletteIndex {
			rows = append(rows, sAccentBold.Render(" "+glyphDot+" "+name)+sDim.Render("   ")+sText.Render(c.Summary))
			continue
		}
		rows = append(rows, sMuted.Render(row))
	}
	rows = append(rows, sDim.Render("  ↑/↓ choose · Tab or Enter to pick · Esc dismiss"))

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cBorder).
		Padding(0, 1).
		Width(width).
		Render(strings.Join(rows, "\n"))
}
