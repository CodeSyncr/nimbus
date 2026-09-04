package tui

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

/*
Clickable file paths.

Terminals have supported hyperlinks since 2017 through OSC 8: text is wrapped
in an escape sequence carrying a URL, and the terminal makes it clickable while
showing only the text. Pointing those links at file:// URLs turns every path
the agent touches into something you can open, instead of something you have to
copy and paste into an editor.

Terminals that do not understand OSC 8 render the escape as nothing at all —
but a few older ones print the raw sequence, which would be worse than no link.
So this is opt-in by terminal, with an env override either way.
*/

// hyperlinkEnv forces links on ("1", "true") or off ("0", "false").
const hyperlinkEnv = "NIMBUS_HYPERLINKS"

var (
	linkOnce       sync.Once
	linksSupported bool
)

// supportsHyperlinks reports whether the terminal renders OSC 8 links.
func supportsHyperlinks() bool {
	linkOnce.Do(func() { linksSupported = detectHyperlinkSupport() })
	return linksSupported
}

func detectHyperlinkSupport() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(hyperlinkEnv))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}

	// A dumb terminal or a redirected stream gets no escapes.
	if os.Getenv("TERM") == "dumb" || os.Getenv("CI") != "" {
		return false
	}

	// Terminals known to support OSC 8. Apple Terminal is deliberately absent:
	// it prints the escape as visible junk.
	switch os.Getenv("TERM_PROGRAM") {
	case "iTerm.app", "WezTerm", "vscode", "ghostty", "Hyper", "rio", "Tabby":
		return true
	}
	if os.Getenv("WT_SESSION") != "" { // Windows Terminal
		return true
	}
	if os.Getenv("KITTY_WINDOW_ID") != "" || strings.Contains(os.Getenv("TERM"), "kitty") {
		return true
	}
	if os.Getenv("VTE_VERSION") != "" { // GNOME Terminal and relatives
		return true
	}
	return false
}

// linkFile renders label as a clickable link to path, when the workspace root
// is known and the terminal supports it. Otherwise it returns label unchanged,
// so callers can use it unconditionally.
func linkFile(appRoot, path, label string) string {
	if label == "" {
		label = path
	}
	if !supportsHyperlinks() || appRoot == "" || path == "" {
		return label
	}
	// Only real paths are worth linking: a bash command or a glob pattern in
	// the same position would produce a dead link.
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(appRoot, path)
	}
	if _, err := os.Stat(abs); err != nil {
		return label
	}
	return osc8("file://"+filepath.ToSlash(abs), label)
}

// osc8 wraps text in a hyperlink escape sequence.
func osc8(url, text string) string {
	const (
		start = "\x1b]8;;"
		sep   = "\x1b\\"
	)
	return start + url + sep + text + start + sep
}
