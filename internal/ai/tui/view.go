package tui

import (
	"fmt"
	"strings"

	"github.com/CodeSyncr/nimbus/internal/version"
	"github.com/charmbracelet/lipgloss"
)

func (m Model) View() string {
	if !m.Ready || m.Width == 0 || m.Height == 0 {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("#D97757")).
			Bold(true).
			Render("✦ Initializing Nimbus AI Copilot...")
	}

	headerView := renderHeader(&m)
	footerView := renderFooter(&m)

	headerHeight := lipgloss.Height(headerView)
	footerHeight := lipgloss.Height(footerView)

	var elements []string
	elements = append(elements, headerView)

	switch m.Mode {
	case ModeClarification:
		questionView := renderQuestionView(&m)
		elements = append(elements, questionView)

	case ModePlanReview:
		planView := renderPlanView(&m)
		elements = append(elements, planView)

	default: // ModeChat, ModePlanning, ModeExecuting, ModeCompleted
		vpHeight := m.Height - headerHeight - footerHeight
		if vpHeight < 4 {
			vpHeight = 4
		}
		m.Viewport.Height = vpHeight
		elements = append(elements, m.Viewport.View())
	}

	elements = append(elements, footerView)

	return lipgloss.JoinVertical(lipgloss.Left, elements...)
}

func renderHeader(m *Model) string {
	width := m.Width - 4
	if width < 40 {
		width = 40
	}

	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#38BDF8")).
		Bold(true)

	subStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#71717A"))

	projStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#F4F4F5")).
		Bold(true)

	branchStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#A1A1AA")).
		Background(lipgloss.Color("#27272A")).
		Padding(0, 1)

	modeBadge := lipgloss.NewStyle().
		Bold(true).
		Padding(0, 1)

	switch m.Mode {
	case ModeChat:
		modeBadge = modeBadge.Background(lipgloss.Color("#27272A")).Foreground(lipgloss.Color("#38BDF8")).SetString("● CHAT")
	case ModePlanning:
		modeBadge = modeBadge.Background(lipgloss.Color("#451A03")).Foreground(lipgloss.Color("#FBBF24")).SetString(fmt.Sprintf("%s PLANNING", m.Spinner.View()))
	case ModePlanReview:
		modeBadge = modeBadge.Background(lipgloss.Color("#78350F")).Foreground(lipgloss.Color("#FEF3C7")).SetString("⚙ PLAN REVIEW")
	case ModeExecuting:
		modeBadge = modeBadge.Background(lipgloss.Color("#064E3B")).Foreground(lipgloss.Color("#4ADE80")).SetString(fmt.Sprintf("%s EXECUTING", m.Spinner.View()))
	case ModeCompleted:
		modeBadge = modeBadge.Background(lipgloss.Color("#14532D")).Foreground(lipgloss.Color("#86EFAC")).SetString("✓ COMPLETED")
	}

	branch := m.Agent.Context.GitBranch
	if branch == "" {
		branch = "main"
	}

	projName := m.Agent.Context.ProjectName
	if projName == "" {
		projName = "nimbus-app"
	}

	vStr := strings.TrimPrefix(version.Nimbus, "v")
	vStr = "v" + vStr

	line1Left := fmt.Sprintf("%s %s", titleStyle.Render("☁ Nimbus AI"), subStyle.Render("("+vStr+")"))
	line1Right := modeBadge.Render()

	spacerLen := width - lipgloss.Width(line1Left) - lipgloss.Width(line1Right)
	if spacerLen < 1 {
		spacerLen = 1
	}
	line1 := line1Left + strings.Repeat(" ", spacerLen) + line1Right

	modules := ""
	if len(m.Agent.Context.NimbusModules) > 0 {
		var pills []string
		for _, mod := range m.Agent.Context.NimbusModules {
			pills = append(pills, lipgloss.NewStyle().Foreground(lipgloss.Color("#D97757")).Render("["+mod+"]"))
		}
		modules = " · " + strings.Join(pills, " ")
	}

	line2 := fmt.Sprintf(
		"%s %s  %s%s",
		subStyle.Render("Workspace:"),
		projStyle.Render(projName),
		branchStyle.Render("⎇ "+branch),
		modules,
	)

	divider := lipgloss.NewStyle().Foreground(lipgloss.Color("#27272A")).Render(strings.Repeat("─", width))

	return line1 + "\n" + line2 + "\n" + divider
}

func renderFooter(m *Model) string {
	width := m.Width - 4
	if width < 40 {
		width = 40
	}

	var elements []string

	// Render Claude-Code-style thinking status indicator above input box
	if m.IsThinking {
		statusLine := RenderThinkingStatus(m)
		if statusLine != "" {
			elements = append(elements, "  "+statusLine)
		}
	}

	inputBorder := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#D97757")).
		Padding(0, 1).
		Width(width)

	if m.Mode != ModeChat {
		inputBorder = inputBorder.BorderForeground(lipgloss.Color("#3F3F46"))
	}

	inputView := inputBorder.Render(m.TextInput.View())
	elements = append(elements, inputView)

	keyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#D97757")).
		Bold(true)

	cmdStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#A1A1AA"))

	dimStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#52525B"))

	var helpText string
	if m.Mode == ModePlanReview {
		helpText = fmt.Sprintf(
			"%s Navigate  %s Toggle  %s Approve All  %s Edit  %s Execute  %s Reject",
			keyStyle.Render("↑/↓"),
			keyStyle.Render("Space"),
			keyStyle.Render("a"),
			keyStyle.Render("e"),
			keyStyle.Render("Enter"),
			keyStyle.Render("Esc"),
		)
	} else {
		helpText = fmt.Sprintf(
			"%s Send   %s History   %s %s %s %s %s %s %s",
			keyStyle.Render("Enter"),
			keyStyle.Render("↑/↓"),
			cmdStyle.Render("/help"),
			dimStyle.Render("·"),
			cmdStyle.Render("/context"),
			dimStyle.Render("·"),
			cmdStyle.Render("/clear"),
			dimStyle.Render("·"),
			cmdStyle.Render("/exit"),
		)
	}

	elements = append(elements, helpText)

	return strings.Join(elements, "\n")
}

func renderChatHistory(m *Model) string {
	var sb strings.Builder

	userPrompt := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#D97757")).
		Bold(true).
		Render("❯ ")

	userTextStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#F4F4F5")).
		Bold(true)

	aiLabel := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#D97757")).
		Bold(true).
		Render("✦ Nimbus")

	timeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#52525B"))

	for _, item := range m.Messages {
		timeStr := timeStyle.Render(item.Timestamp.Format("15:04:05"))

		switch item.Role {
		case "user":
			sb.WriteString(fmt.Sprintf("%s%s  %s\n\n", userPrompt, userTextStyle.Render(item.Content), timeStr))

		case "tool":
			badgeColor := "#4ADE80"
			actionText := "CREATED "
			upper := strings.ToUpper(item.ToolName)
			switch {
			case strings.Contains(upper, "CREATE") || upper == "WRITE_FILE":
				badgeColor = "#4ADE80" // Green
				actionText = "CREATED "
			case strings.Contains(upper, "MODIF") || strings.Contains(upper, "EDIT"):
				badgeColor = "#D97757" // Orange
				actionText = "MODIFIED"
			case strings.Contains(upper, "SKILL"):
				badgeColor = "#F59E0B" // Amber
				actionText = "SKILL   "
			case strings.Contains(upper, "ANALYZ") || strings.Contains(upper, "READ") || strings.Contains(upper, "GREP") || strings.Contains(upper, "LIST"):
				badgeColor = "#38BDF8" // Cyan
				actionText = "ANALYZED"
			case strings.Contains(upper, "EXEC") || strings.Contains(upper, "BASH") || strings.Contains(upper, "RUN"):
				badgeColor = "#A78BFA" // Purple
				actionText = "EXECUTED"
			case strings.Contains(upper, "DELET"):
				badgeColor = "#F87171" // Red
				actionText = "DELETED "
			default:
				badgeColor = "#38BDF8"
				actionText = fmt.Sprintf("%-8s", upper)
			}

			if item.IsError {
				badgeColor = "#EF4444"
			}

			badge := lipgloss.NewStyle().
				Foreground(lipgloss.Color(badgeColor)).
				Bold(true).
				Render(actionText)

			target := item.Content
			if target == "" {
				if path, ok := item.ToolArgs["path"].(string); ok {
					target = path
				} else if cmd, ok := item.ToolArgs["command"].(string); ok {
					target = cmd
				}
			}

			targetStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#F4F4F5")).
				Bold(true).
				Render(target)

			sb.WriteString(fmt.Sprintf("  ✦ %s  %s\n\n", badge, targetStyle))

		case "assistant":
			header := fmt.Sprintf("%s  %s", aiLabel, timeStr)
			formattedContent := RenderMarkdown(item.Content, m.Width)

			if len(item.Diffs) > 0 {
				formattedContent += "\n\n" + lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#4ADE80")).Render("📄 Applied Changes:")
				for _, d := range item.Diffs {
					formattedContent += "\n" + RenderColorizedDiff(d)
				}
			}

			if item.IsError {
				errCard := lipgloss.NewStyle().
					Border(lipgloss.NormalBorder(), false, false, false, true).
					BorderForeground(lipgloss.Color("#EF4444")).
					Padding(0, 1).
					Foreground(lipgloss.Color("#FCA5A5")).
					Render(fmt.Sprintf("✖ %s\n\n%s", header, formattedContent))
				sb.WriteString(errCard + "\n\n")
			} else {
				sb.WriteString(header + "\n\n" + formattedContent + "\n\n")
			}
		}
	}

	return strings.TrimRight(sb.String(), "\n")
}
