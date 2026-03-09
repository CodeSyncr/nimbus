package command

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// ANSI escape codes (no external deps)
const (
	ansiReset   = "\033[0m"
	ansiRed     = "\033[31m"
	ansiGreen   = "\033[32m"
	ansiYellow  = "\033[33m"
	ansiBlue    = "\033[34m"
	ansiCyan    = "\033[36m"
	ansiGray    = "\033[90m"
	ansiBold    = "\033[1m"
	ansiDim     = "\033[2m"
	ansiBgGreen = "\033[42m"
	ansiBgRed   = "\033[41m"
)

// UI provides terminal output helpers (AdonisJS Ace Terminal UI style).
type UI struct {
	out io.Writer
	err io.Writer
}

// NewUI returns a UI writing to stdout/stderr.
func NewUI() *UI {
	return &UI{out: os.Stdout, err: os.Stderr}
}

// Logger returns a logger for this UI.
func (u *UI) Logger() *Logger {
	return &Logger{ui: u}
}

// Colors returns a color formatter.
func (u *UI) Colors() *Colors {
	return &Colors{}
}

// Table returns a new table.
func (u *UI) Table() *Table {
	return &Table{ui: u}
}

// Sticker returns a new sticker (boxed content).
func (u *UI) Sticker() *Sticker {
	return &Sticker{ui: u}
}

// Instructions returns a new instructions block (arrow-prefixed lines).
func (u *UI) Instructions() *Sticker {
	return &Sticker{ui: u, prefix: "> "}
}

// Logger provides log levels (debug, info, success, warning, error).
type Logger struct {
	ui *UI
}

// Debug writes a debug message (dim).
func (l *Logger) Debug(msg string) {
	fmt.Fprintln(l.ui.out, ansiDim+msg+ansiReset)
}

// Info writes an info message.
func (l *Logger) Info(msg string) {
	fmt.Fprintln(l.ui.out, msg)
}

// Success writes a success message (green).
func (l *Logger) Success(msg string) {
	fmt.Fprintln(l.ui.out, ansiGreen+msg+ansiReset)
}

// Warning writes a warning message (yellow).
func (l *Logger) Warning(msg string) {
	fmt.Fprintln(l.ui.out, ansiYellow+msg+ansiReset)
}

// Error writes an error message to stderr (red).
func (l *Logger) Error(err error) {
	if err != nil {
		fmt.Fprintln(l.ui.err, ansiRed+err.Error()+ansiReset)
	}
}

// Colors provides ANSI color formatting.
type Colors struct{}

func (c *Colors) Red(s string) string   { return ansiRed + s + ansiReset }
func (c *Colors) Green(s string) string { return ansiGreen + s + ansiReset }
func (c *Colors) Yellow(s string) string { return ansiYellow + s + ansiReset }
func (c *Colors) Cyan(s string) string  { return ansiCyan + s + ansiReset }
func (c *Colors) Gray(s string) string  { return ansiGray + s + ansiReset }
func (c *Colors) Bold(s string) string  { return ansiBold + s + ansiReset }
func (c *Colors) Dim(s string) string  { return ansiDim + s + ansiReset }

// BgGreen returns green background + white text.
func (c *Colors) BgGreen(s string) string { return ansiBgGreen + ansiBold + " " + s + " " + ansiReset }
// BgRed returns red background + white text.
func (c *Colors) BgRed(s string) string { return ansiBgRed + ansiBold + " " + s + " " + ansiReset }

// Table renders rows and columns.
type Table struct {
	ui    *UI
	head  []string
	rows  [][]string
}

// Head sets the table headers.
func (t *Table) Head(cols ...string) *Table {
	t.head = cols
	return t
}

// Row adds a row.
func (t *Table) Row(cols ...string) *Table {
	t.rows = append(t.rows, cols)
	return t
}

// Render outputs the table.
func (t *Table) Render() {
	if len(t.head) == 0 {
		return
	}
	widths := make([]int, len(t.head))
	for i, h := range t.head {
		widths[i] = len(stripANSI(h))
	}
	for _, row := range t.rows {
		for i, c := range row {
			if i < len(widths) {
				n := len(stripANSI(c))
				if n > widths[i] {
					widths[i] = n
				}
			}
		}
	}
	pad := func(s string, w int) string {
		n := len(stripANSI(s))
		if n >= w {
			return s
		}
		return s + strings.Repeat(" ", w-n)
	}
	var hdr []string
	for i, s := range t.head {
		hdr = append(hdr, pad(s, widths[i]))
	}
	fmt.Fprintln(t.ui.out, strings.Join(hdr, "  "))
	total := 0
	for _, w := range widths {
		total += w
	}
	fmt.Fprintln(t.ui.out, strings.Repeat("-", total+2*(len(widths)-1)))
	for _, row := range t.rows {
		var cells []string
		for i := range t.head {
			s := ""
			if i < len(row) {
				s = row[i]
			}
			cells = append(cells, pad(s, widths[i]))
		}
		fmt.Fprintln(t.ui.out, strings.Join(cells, "  "))
	}
}

// Sticker renders boxed content.
type Sticker struct {
	ui     *UI
	prefix string
	lines  []string
}

// Add appends a line.
func (s *Sticker) Add(line string) *Sticker {
	s.lines = append(s.lines, s.prefix+line)
	return s
}

// Render outputs the sticker.
func (s *Sticker) Render() {
	if len(s.lines) == 0 {
		return
	}
	maxLen := 0
	for _, l := range s.lines {
		// Strip ANSI for width
		plain := stripANSI(l)
		if len(plain) > maxLen {
			maxLen = len(plain)
		}
	}
	top := "┌" + strings.Repeat("─", maxLen+2) + "┐"
	bottom := "└" + strings.Repeat("─", maxLen+2) + "┘"
	fmt.Fprintln(s.ui.out, top)
	for _, l := range s.lines {
		plain := stripANSI(l)
		padded := l + strings.Repeat(" ", maxLen-len(plain))
		fmt.Fprintln(s.ui.out, "│ "+padded+" │")
	}
	fmt.Fprintln(s.ui.out, bottom)
}

func stripANSI(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		if r == '\033' {
			in = true
			continue
		}
		if in {
			if r == 'm' {
				in = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
