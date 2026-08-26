package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const maxDiffLines = 60

// RenderColorizedDiff formats unified diff strings with addition/deletion
// styling. Long diffs are capped so a big generated file doesn't swamp the
// transcript.
func RenderColorizedDiff(diff string) string {
	diff = strings.TrimSpace(diff)
	if diff == "" {
		return ""
	}

	fileStyle := sBlue.Bold(true)
	addStyle := sGreen
	delStyle := sRed
	hunkStyle := sMuted.Italic(true)
	ctxStyle := sDim

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cBorder).
		Padding(0, 1)

	lines := strings.Split(diff, "\n")
	var out []string
	for i, line := range lines {
		if i >= maxDiffLines {
			out = append(out, ctxStyle.Render(fmt.Sprintf("… %d more lines", len(lines)-i)))
			break
		}
		switch {
		case strings.HasPrefix(line, "---"), strings.HasPrefix(line, "+++"):
			out = append(out, fileStyle.Render(line))
		case strings.HasPrefix(line, "diff --git"), strings.HasPrefix(line, "Index:"):
			out = append(out, fileStyle.Render(line))
		case strings.HasPrefix(line, "@@"):
			out = append(out, hunkStyle.Render(line))
		case strings.HasPrefix(line, "+"):
			out = append(out, addStyle.Render(line))
		case strings.HasPrefix(line, "-"):
			out = append(out, delStyle.Render(line))
		default:
			out = append(out, ctxStyle.Render(line))
		}
	}

	result := strings.Join(out, "\n")
	if len(lines) > 3 {
		return box.Render(result)
	}
	return fmt.Sprintf("\n%s\n", result)
}
