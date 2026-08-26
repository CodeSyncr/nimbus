package tui

import (
	"fmt"
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
	default:
		m.Viewport.Height = m.Height - lipgloss.Height(headerView) - lipgloss.Height(footerView)
		if m.Viewport.Height < 4 {
			m.Viewport.Height = 4
		}
		body = m.Viewport.View()
	}

	return lipgloss.JoinVertical(lipgloss.Left, headerView, body, footerView)
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
	default:
		badge = sMuted.Render(glyphDot + " CHAT")
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

	switch m.Mode {
	case ModePlanReview:
		parts = append(parts, " "+strings.Join([]string{
			sKey.Render("Enter") + sMuted.Render(" approve & run"),
			sKey.Render("Esc") + sMuted.Render(" reject"),
			sKey.Render("↑/↓") + sMuted.Render(" scroll"),
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
		parts = append(parts, box.Render(m.TextInput.View()))

		var hints string
		if m.Mode.busy() {
			hints = sMuted.Render("Esc") + sDim.Render(" interrupt   ") + sMuted.Render("PgUp/PgDn") + sDim.Render(" scroll")
		} else {
			hints = sMuted.Render("/help") + sDim.Render("  ·  ") + sMuted.Render("/context") + sDim.Render("  ·  ") + sMuted.Render("/clear") + sDim.Render("  ·  ") + sMuted.Render("/exit")
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

const maxInlineDiffLines = 24

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
				label += sDim.Render(fmt.Sprintf("  %s", fmtDuration(item.Elapsed)))
			}
			sb.WriteString("\n  " + sMuted.Render(glyphPhase+" ") + sMuted.Italic(true).Render(label) + "\n")

		case "tool":
			sb.WriteString(renderToolLine(item, textWidth))

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
func renderToolLine(item ChatItem, textWidth int) string {
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

	line := "  " + dot + " " + sBold.Render(verb) + " " + sText.Render(target)
	if item.Detail != "" {
		if item.IsError {
			line += "  " + sRed.Render(item.Detail)
		} else {
			line += "  " + sDim.Render(item.Detail)
		}
	}
	out := line + "\n"

	if item.Diff != "" {
		out += indentLines(renderCompactDiff(item.Diff, maxInlineDiffLines), 4, false) + "\n"
	}
	return out
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
