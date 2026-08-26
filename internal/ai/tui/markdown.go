package tui

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	reBold       = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reInlineCode = regexp.MustCompile("`([^`]+)`")
	reNumbered   = regexp.MustCompile(`^(\d+)\.\s+(.*)$`)
)

// RenderMarkdown parses basic markdown constructs and converts them into Lipgloss styled strings.
func RenderMarkdown(input string, maxWidth int) string {
	if strings.TrimSpace(input) == "" {
		return ""
	}

	lines := strings.Split(input, "\n")
	var sb strings.Builder

	inCodeBlock := false
	codeBlockLang := ""
	var codeLines []string

	h1Style := sAccentBold
	h2Style := sBold
	h3Style := lipgloss.NewStyle().Foreground(cSoft).Bold(true)
	bulletStyle := sAccent
	numStyle := sMuted
	quoteStyle := sMuted.Italic(true)
	quoteBar := sAccent.Render(glyphBar + " ")
	codeBlockStyle := lipgloss.NewStyle().Background(cPanel).Foreground(cText).Padding(0, 1)
	langTagStyle := lipgloss.NewStyle().Foreground(cAccent).Background(cCode).Bold(true).Padding(0, 1)

	for i := 0; i < len(lines); i++ {
		line := lines[i]

		// Fenced code block boundary
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			if !inCodeBlock {
				inCodeBlock = true
				codeBlockLang = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "```"))
				codeLines = nil
				continue
			}
			inCodeBlock = false
			header := ""
			if codeBlockLang != "" {
				header = langTagStyle.Render(codeBlockLang) + "\n"
			}
			sb.WriteString(header + codeBlockStyle.Render(strings.Join(codeLines, "\n")) + "\n")
			codeLines = nil
			codeBlockLang = ""
			continue
		}
		if inCodeBlock {
			codeLines = append(codeLines, line)
			continue
		}

		trimmed := strings.TrimSpace(line)

		// Horizontal rule
		if trimmed == "---" || trimmed == "***" || trimmed == "___" {
			width := 40
			if maxWidth > 10 {
				width = maxWidth - 8
			}
			if width > 60 {
				width = 60
			}
			sb.WriteString(sDivider.Render(strings.Repeat("─", width)) + "\n")
			continue
		}

		// Headings
		switch {
		case strings.HasPrefix(line, "# "):
			sb.WriteString(h1Style.Render(styleInline(strings.TrimPrefix(line, "# "))) + "\n")
			continue
		case strings.HasPrefix(line, "## "):
			sb.WriteString(h2Style.Render(styleInline(strings.TrimPrefix(line, "## "))) + "\n")
			continue
		case strings.HasPrefix(line, "### "), strings.HasPrefix(line, "#### "):
			sb.WriteString(h3Style.Render(styleInline(strings.TrimLeft(line, "# "))) + "\n")
			continue
		}

		// Blockquotes
		if strings.HasPrefix(trimmed, "> ") {
			sb.WriteString(quoteBar + quoteStyle.Render(styleInline(strings.TrimPrefix(trimmed, "> "))) + "\n")
			continue
		}

		// Bullet lists
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			indent := len(line) - len(strings.TrimLeft(line, " \t"))
			sb.WriteString(strings.Repeat(" ", indent) + bulletStyle.Render("• ") + styleInline(trimmed[2:]) + "\n")
			continue
		}

		// Numbered lists
		if m := reNumbered.FindStringSubmatch(trimmed); m != nil {
			indent := len(line) - len(strings.TrimLeft(line, " \t"))
			sb.WriteString(strings.Repeat(" ", indent) + numStyle.Render(m[1]+". ") + styleInline(m[2]) + "\n")
			continue
		}

		sb.WriteString(styleInline(line) + "\n")
	}

	if inCodeBlock && len(codeLines) > 0 {
		sb.WriteString(codeBlockStyle.Render(strings.Join(codeLines, "\n")) + "\n")
	}

	return strings.TrimRight(sb.String(), "\n")
}

// styleInline parses **bold** and `code` inline elements.
func styleInline(text string) string {
	boldStyle := sBold
	codeStyle := lipgloss.NewStyle().Foreground(cText).Background(cCode)

	text = reBold.ReplaceAllStringFunc(text, func(m string) string {
		return boldStyle.Render(m[2 : len(m)-2])
	})
	text = reInlineCode.ReplaceAllStringFunc(text, func(m string) string {
		return codeStyle.Render(" " + m[1:len(m)-1] + " ")
	})
	return text
}
