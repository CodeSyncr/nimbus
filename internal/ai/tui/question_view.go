package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func renderQuestionView(m *Model) string {
	if m.ClarificationPlan == nil || len(m.ClarificationPlan.Questions) == 0 {
		return sMuted.Render("  No questions pending.")
	}
	qIdx := m.CurrentQuestionIdx
	if qIdx < 0 || qIdx >= len(m.ClarificationPlan.Questions) {
		return ""
	}
	q := m.ClarificationPlan.Questions[qIdx]

	width := contentWidth(m)
	inner := width - 6
	if inner < 34 {
		inner = 34
	}
	card := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cBlue).
		Padding(1, 2).
		Width(width - 2)

	var sb strings.Builder
	sb.WriteString(sBlue.Bold(true).Render(fmt.Sprintf("Question %d of %d", qIdx+1, len(m.ClarificationPlan.Questions))))
	if summary := strings.TrimSpace(m.ClarificationPlan.Summary); summary != "" {
		sb.WriteString("  " + sMuted.Render(summary))
	}
	sb.WriteString("\n\n")
	sb.WriteString(lipgloss.NewStyle().Width(inner).Inherit(sBold).Render(q.Question) + "\n\n")

	for i, opt := range q.Options {
		selected := i == m.SelectedOptionIdx && !m.IsCustomInput
		marker := sDim.Render("○")
		label := sSoft.Render(opt)
		if selected {
			marker = sAccent.Render("●")
			label = sAccentBold.Render(opt)
		}
		def := ""
		if opt == q.Default {
			def = sDim.Render("  (default)")
		}
		sb.WriteString(fmt.Sprintf("  %s %s %s%s\n", marker, sDim.Render(fmt.Sprintf("%d.", i+1)), label, def))
	}

	sb.WriteString("\n")
	if m.IsCustomInput {
		sb.WriteString("  " + sAccent.Render("●") + " " + m.CustomInput.View() + "\n")
	} else {
		sb.WriteString("  " + sDim.Render("○") + " " + sMuted.Render("c  Something else…") + "\n")
	}

	if qIdx > 0 {
		sb.WriteString("\n" + sDim.Render("Answered") + "\n")
		for prev := 0; prev < qIdx; prev++ {
			pq := m.ClarificationPlan.Questions[prev]
			sb.WriteString("  " + sMuted.Render(pq.Question+": ") + sBlue.Render(m.ClarificationAnswers[pq.ID]) + "\n")
		}
	}

	return "\n" + card.Render(strings.TrimRight(sb.String(), "\n")) + "\n"
}
