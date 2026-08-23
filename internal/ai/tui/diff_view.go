package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// RenderColorizedDiff formats unified diff strings with clean addition and deletion styling.
func RenderColorizedDiff(diff string) string {
	diff = strings.TrimSpace(diff)
	if diff == "" {
		return ""
	}

	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#38BDF8")).
		Bold(true)

	addStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#4ADE80")).
		Background(lipgloss.Color("#0D2818"))

	delStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#F87171")).
		Background(lipgloss.Color("#2A1215"))

	hunkStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#A1A1AA")).
		Italic(true)

	ctxStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#71717A"))

	diffBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#3F3F46")).
		Padding(0, 1).
		MarginBottom(1)

	lines := strings.Split(diff, "\n")
	var sb strings.Builder

	for _, line := range lines {
		if strings.HasPrefix(line, "---") {
			sb.WriteString(delStyle.Render(line) + "\n")
		} else if strings.HasPrefix(line, "+++") {
			sb.WriteString(addStyle.Render(line) + "\n")
		} else if strings.HasPrefix(line, "diff --git") || strings.HasPrefix(line, "Index:") {
			sb.WriteString(headerStyle.Render(line) + "\n")
		} else if strings.HasPrefix(line, "@@") {
			sb.WriteString(hunkStyle.Render(line) + "\n")
		} else if strings.HasPrefix(line, "+") {
			sb.WriteString(addStyle.Render(line) + "\n")
		} else if strings.HasPrefix(line, "-") {
			sb.WriteString(delStyle.Render(line) + "\n")
		} else {
			sb.WriteString(ctxStyle.Render(line) + "\n")
		}
	}

	result := strings.TrimRight(sb.String(), "\n")
	if len(lines) > 3 {
		return diffBox.Render(result)
	}
	return fmt.Sprintf("\n%s\n", result)
}
