package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func renderQuestionView(m *Model) string {
	if m.ClarificationPlan == nil || len(m.ClarificationPlan.Questions) == 0 {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#71717A")).Render("No questions currently pending.")
	}

	qIdx := m.CurrentQuestionIdx
	if qIdx < 0 || qIdx >= len(m.ClarificationPlan.Questions) {
		return ""
	}

	q := m.ClarificationPlan.Questions[qIdx]
	width := m.Width - 4
	if width < 40 {
		width = 40
	}

	cardStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#3F3F46")).
		Padding(1, 2).
		MarginBottom(1).
		Width(width)

	headerTitle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#D97757")).
		Render(fmt.Sprintf("🤔 ARCHITECTURAL CLARIFICATION (Step %d of %d)", qIdx+1, len(m.ClarificationPlan.Questions)))

	var sb strings.Builder
	sb.WriteString(headerTitle + "\n\n")

	// Display question
	qStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#F4F4F5")).
		MarginBottom(1)
	sb.WriteString(qStyle.Render(fmt.Sprintf("❯ %s", q.Question)) + "\n\n")

	// Render Options
	for i, opt := range q.Options {
		isCursor := i == m.SelectedOptionIdx && !m.IsCustomInput

		radio := "( ) "
		if isCursor {
			radio = "(*) "
		}

		numPrefix := fmt.Sprintf("%d. ", i+1)

		if isCursor {
			itemStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#D97757")).
				Bold(true)
			sb.WriteString(itemStyle.Render(fmt.Sprintf("  %s%s%s", radio, numPrefix, opt)) + "\n")
		} else {
			itemStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#A1A1AA"))
			sb.WriteString(itemStyle.Render(fmt.Sprintf("  %s%s%s", radio, numPrefix, opt)) + "\n")
		}
	}

	// Custom Answer Option
	sb.WriteString("\n")
	if m.IsCustomInput {
		customHeader := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#D97757")).
			Bold(true).
			Render("  (*) [c] Custom Input: ")
		sb.WriteString(customHeader + m.CustomInput.View() + "\n")
	} else {
		customStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#71717A"))
		sb.WriteString(customStyle.Render("  ( ) [c] Type a custom response...") + "\n")
	}

	// Previous Answers summary if past step 1
	if qIdx > 0 && len(m.ClarificationAnswers) > 0 {
		sb.WriteString("\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("#52525B")).Render("── Previous Choices ──") + "\n")
		for prevIdx := 0; prevIdx < qIdx; prevIdx++ {
			prevQ := m.ClarificationPlan.Questions[prevIdx]
			ans := m.ClarificationAnswers[prevQ.ID]
			sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#71717A")).Render(fmt.Sprintf("  • %s: ", prevQ.Question)))
			sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#38BDF8")).Bold(true).Render(ans) + "\n")
		}
	}

	sb.WriteString("\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("#3F3F46")).Render(strings.Repeat("─", width-6)) + "\n")

	// Action bar
	actionBar := lipgloss.NewStyle().Foreground(lipgloss.Color("#71717A")).Render(
		"[↑/↓] Select    [1-9] Quick Select    [c] Custom Answer    [Enter] Next / Confirm    [Esc] Cancel",
	)
	sb.WriteString(actionBar)

	return cardStyle.Render(sb.String())
}
