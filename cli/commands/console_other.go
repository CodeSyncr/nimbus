//go:build !windows

package commands

// enableConsoleUTF8 is a no-op outside Windows: Unix terminals are UTF-8.
func enableConsoleUTF8() {}
