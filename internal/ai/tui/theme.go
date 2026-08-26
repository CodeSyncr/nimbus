package tui

import "github.com/charmbracelet/lipgloss"

// Palette. Adaptive colours keep the UI legible on light terminals; the
// accent is the Nimbus orange used for prompts, bullets and highlights.
var (
	cAccent = lipgloss.Color("#D97757")
	cBlue   = lipgloss.Color("#38BDF8")
	cGreen  = lipgloss.Color("#4ADE80")
	cRed    = lipgloss.Color("#F87171")
	cYellow = lipgloss.Color("#FBBF24")
	cPurple = lipgloss.Color("#A78BFA")

	cText   = lipgloss.AdaptiveColor{Light: "#18181B", Dark: "#F4F4F5"}
	cSoft   = lipgloss.AdaptiveColor{Light: "#3F3F46", Dark: "#D4D4D8"}
	cMuted  = lipgloss.AdaptiveColor{Light: "#71717A", Dark: "#A1A1AA"}
	cDim    = lipgloss.AdaptiveColor{Light: "#A1A1AA", Dark: "#52525B"}
	cBorder = lipgloss.AdaptiveColor{Light: "#D4D4D8", Dark: "#3F3F46"}
	cPanel  = lipgloss.AdaptiveColor{Light: "#F4F4F5", Dark: "#18181B"}
	cCode   = lipgloss.AdaptiveColor{Light: "#E4E4E7", Dark: "#27272A"}
)

// Reusable styles.
var (
	sAccent     = lipgloss.NewStyle().Foreground(cAccent)
	sAccentBold = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	sText       = lipgloss.NewStyle().Foreground(cText)
	sBold       = lipgloss.NewStyle().Foreground(cText).Bold(true)
	sSoft       = lipgloss.NewStyle().Foreground(cSoft)
	sMuted      = lipgloss.NewStyle().Foreground(cMuted)
	sDim        = lipgloss.NewStyle().Foreground(cDim)
	sGreen      = lipgloss.NewStyle().Foreground(cGreen)
	sRed        = lipgloss.NewStyle().Foreground(cRed)
	sYellow     = lipgloss.NewStyle().Foreground(cYellow)
	sBlue       = lipgloss.NewStyle().Foreground(cBlue)
	sPurple     = lipgloss.NewStyle().Foreground(cPurple)
	sKey        = lipgloss.NewStyle().Foreground(cText).Bold(true)
	sDivider    = lipgloss.NewStyle().Foreground(cBorder)
)

// Glyphs. Kept to characters with wide terminal-font coverage (Windows
// Terminal, iTerm, GNOME Terminal); the console is switched to UTF-8 on
// Windows before the program starts.
const (
	glyphPrompt = "❯"
	glyphAI     = "✦"
	glyphDot    = "●"
	glyphPhase  = "◆"
	glyphOK     = "✓"
	glyphErr    = "✗"
	glyphBranch = "⎇"
	glyphArrow  = "↳"
	glyphBar    = "│"
)
