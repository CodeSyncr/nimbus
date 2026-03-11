package ui

import (
	"fmt"
	"io"

	"github.com/charmbracelet/lipgloss"
)

// StyleSet holds the base styles used across Nimbus CLI commands.
type StyleSet struct {
	Success lipgloss.Style
	Warn    lipgloss.Style
	Error   lipgloss.Style
	Info    lipgloss.Style
	Panel   lipgloss.Style
	Title   lipgloss.Style
}

// UI provides styled output helpers for CLI commands.
type UI struct {
	in     io.Reader
	out    io.Writer
	errOut io.Writer
	style  StyleSet
}

// NewUI constructs a UI with sensible defaults for styling.
func NewUI(in io.Reader, out, errOut io.Writer) *UI {
	base := lipgloss.NewStyle().Padding(0).Margin(0)
	styles := StyleSet{
		Success: base.Foreground(lipgloss.Color("10")),
		Warn:    base.Foreground(lipgloss.Color("220")),
		Error:   base.Foreground(lipgloss.Color("196")).Bold(true),
		Info:    base.Foreground(lipgloss.Color("39")),
		Panel:   base.Border(lipgloss.RoundedBorder()).Padding(1, 2),
		Title:   base.Bold(true),
	}
	return &UI{
		in:     in,
		out:    out,
		errOut: errOut,
		style:  styles,
	}
}

func (ui *UI) Successf(format string, args ...any) {
	fmt.Fprintln(ui.out, ui.style.Success.Render("✓ "+fmt.Sprintf(format, args...)))
}

func (ui *UI) Warnf(format string, args ...any) {
	fmt.Fprintln(ui.out, ui.style.Warn.Render("⚠ "+fmt.Sprintf(format, args...)))
}

func (ui *UI) Errorf(format string, args ...any) {
	fmt.Fprintln(ui.errOut, ui.style.Error.Render("✖ "+fmt.Sprintf(format, args...)))
}

func (ui *UI) Infof(format string, args ...any) {
	fmt.Fprintln(ui.out, ui.style.Info.Render("ℹ "+fmt.Sprintf(format, args...)))
}

// Panel renders a titled panel using the panel and title styles.
func (ui *UI) Panel(title, body string) {
	header := ui.style.Title.Render(title)
	box := ui.style.Panel.Render(header + "\n\n" + body)
	fmt.Fprintln(ui.out, box)
}

