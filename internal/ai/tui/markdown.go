package tui

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	reBold       = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reItalic     = regexp.MustCompile(`\*([^*]+?)\*`)
	reInlineCode = regexp.MustCompile("`([^`]+)`")
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

	h1Style := lipgloss.NewStyle().Foreground(lipgloss.Color("#D97757")).Bold(true)
	h2Style := lipgloss.NewStyle().Foreground(lipgloss.Color("#F4F4F5")).Bold(true)
	h3Style := lipgloss.NewStyle().Foreground(lipgloss.Color("#E4E4E7")).Bold(true)
	bulletStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#D97757"))
	numStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#A1A1AA"))
	quoteStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF")).Italic(true)
	quoteBar := lipgloss.NewStyle().Foreground(lipgloss.Color("#D97757")).Render("│ ")
	codeBlockStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("#18181B")).
		Foreground(lipgloss.Color("#E4E4E7")).
		Padding(0, 1)
	langTagStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#D97757")).
		Background(lipgloss.Color("#27272A")).
		Bold(true).
		Padding(0, 1)
	hrStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#3F3F46"))

	for i := 0; i < len(lines); i++ {
		line := lines[i]

		// Handle fenced code block boundary
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			if !inCodeBlock {
				inCodeBlock = true
				codeBlockLang = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "```"))
				codeLines = nil
				continue
			} else {
				inCodeBlock = false
				// Flush code block
				header := ""
				if codeBlockLang != "" {
					header = langTagStyle.Render(codeBlockLang) + "\n"
				}
				codeBody := strings.Join(codeLines, "\n")
				renderedCode := codeBlockStyle.Render(codeBody)
				sb.WriteString(header + renderedCode + "\n")
				codeLines = nil
				codeBlockLang = ""
				continue
			}
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
			sb.WriteString(hrStyle.Render(strings.Repeat("─", width)) + "\n")
			continue
		}

		// Headings
		if strings.HasPrefix(line, "# ") {
			text := strings.TrimPrefix(line, "# ")
			sb.WriteString(h1Style.Render(styleInline(text)) + "\n")
			continue
		}
		if strings.HasPrefix(line, "## ") {
			text := strings.TrimPrefix(line, "## ")
			sb.WriteString(h2Style.Render(styleInline(text)) + "\n")
			continue
		}
		if strings.HasPrefix(line, "### ") {
			text := strings.TrimPrefix(line, "### ")
			sb.WriteString(h3Style.Render(styleInline(text)) + "\n")
			continue
		}

		// Blockquotes
		if strings.HasPrefix(trimmed, "> ") {
			text := strings.TrimPrefix(trimmed, "> ")
			sb.WriteString(quoteBar + quoteStyle.Render(styleInline(text)) + "\n")
			continue
		}

		// Bullet lists
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			indent := len(line) - len(strings.TrimLeft(line, " \t"))
			prefix := strings.Repeat(" ", indent)
			text := trimmed[2:]
			sb.WriteString(prefix + bulletStyle.Render("• ") + styleInline(text) + "\n")
			continue
		}

		// Numbered lists (e.g. 1. , 2. )
		if len(trimmed) > 3 && (trimmed[1] == '.' || trimmed[2] == '.') && (trimmed[0] >= '0' && trimmed[0] <= '9') {
			parts := strings.SplitN(trimmed, ". ", 2)
			if len(parts) == 2 {
				indent := len(line) - len(strings.TrimLeft(line, " \t"))
				prefix := strings.Repeat(" ", indent)
				sb.WriteString(prefix + numStyle.Render(parts[0]+". ") + styleInline(parts[1]) + "\n")
				continue
			}
		}

		// Regular line with inline markdown
		sb.WriteString(styleInline(line) + "\n")
	}

	// If code block left open
	if inCodeBlock && len(codeLines) > 0 {
		codeBody := strings.Join(codeLines, "\n")
		sb.WriteString(codeBlockStyle.Render(codeBody) + "\n")
	}

	return strings.TrimRight(sb.String(), "\n")
}

// styleInline parses **bold**, *italic*, and `code` inline elements.
func styleInline(text string) string {
	boldStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF"))
	codeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E4E4E7")).
		Background(lipgloss.Color("#27272A"))

	// Replace bold
	text = reBold.ReplaceAllStringFunc(text, func(m string) string {
		inner := m[2 : len(m)-2]
		return boldStyle.Render(inner)
	})

	// Replace inline code
	text = reInlineCode.ReplaceAllStringFunc(text, func(m string) string {
		inner := m[1 : len(m)-1]
		return codeStyle.Render(" " + inner + " ")
	})

	return text
}
