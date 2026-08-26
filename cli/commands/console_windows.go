//go:build windows

package commands

import (
	"golang.org/x/sys/windows"
)

// enableConsoleUTF8 switches the attached console to the UTF-8 code page so
// the TUI's box-drawing and status glyphs render instead of mojibake in
// cmd.exe / conhost (Windows Terminal already defaults to UTF-8). It also
// turns on virtual-terminal processing for stdout so ANSI styling works in
// legacy consoles. Failures are ignored: without a console there is nothing
// to configure.
func enableConsoleUTF8() {
	const cpUTF8 = 65001
	_ = windows.SetConsoleOutputCP(cpUTF8)
	_ = windows.SetConsoleCP(cpUTF8)

	stdout := windows.Handle(windows.Stdout)
	var mode uint32
	if err := windows.GetConsoleMode(stdout, &mode); err == nil {
		_ = windows.SetConsoleMode(stdout, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
	}
}
