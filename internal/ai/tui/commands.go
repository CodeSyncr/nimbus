package tui

import "strings"

/*
The slash-command palette.

Commands existed but were invisible: you had to already know the name to type
it, and the list lived in two places — the dispatch switch and the /help text —
which drift apart the moment one is edited.

This is the single registry. It drives the menu that opens when you type "/",
the completion, and the help output, so a command added here appears in all
three at once.
*/

// SlashCommand is one entry in the palette.
type SlashCommand struct {
	Name    string   // canonical form, with the leading slash
	Aliases []string // other spellings that dispatch to it
	Summary string   // one line, shown in the menu
}

// slashCommands is the catalogue, in the order the menu shows them: the ones
// reached for most often first.
var slashCommands = []SlashCommand{
	{Name: "/help", Aliases: []string{"help", "?"}, Summary: "commands and keyboard shortcuts"},
	{Name: "/context", Summary: "what I know about this project"},
	{Name: "/compact", Summary: "summarise earlier turns to free up context"},
	{Name: "/skills", Summary: "agent skills available for this request"},
	{Name: "/settings", Aliases: []string{"/config"}, Summary: "change and save how Nimbus AI behaves"},
	{Name: "/session", Summary: "session id, memory, and how to resume it"},
	{Name: "/sessions", Summary: "earlier sessions in this project, and how to resume one"},
	{Name: "/clear", Aliases: []string{"clear"}, Summary: "clear the transcript on screen"},
	{Name: "/exit", Aliases: []string{"/quit", "exit", "quit", "q"}, Summary: "quit Nimbus AI"},
}

// matchCommands returns the commands matching what has been typed so far.
//
// An exact command with arguments after it matches nothing: the user has
// finished choosing and is now typing, so the menu gets out of the way.
func matchCommands(input string) []SlashCommand {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, "/") || strings.Contains(trimmed, " ") {
		return nil
	}

	needle := strings.ToLower(trimmed)
	var out []SlashCommand
	for _, c := range slashCommands {
		if strings.HasPrefix(c.Name, needle) {
			out = append(out, c)
			continue
		}
		for _, alias := range c.Aliases {
			if strings.HasPrefix("/"+strings.TrimPrefix(alias, "/"), needle) {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

// helpLines renders the catalogue for /help, so the two never disagree.
func helpLines() []string {
	lines := []string{"Commands"}
	width := 0
	for _, c := range slashCommands {
		if len(c.Name) > width {
			width = len(c.Name)
		}
	}
	for _, c := range slashCommands {
		lines = append(lines, "  "+c.Name+strings.Repeat(" ", width-len(c.Name)+3)+c.Summary)
	}
	return append(lines,
		"",
		"Keys",
		"  Enter send · Alt+Enter newline · ↑/↓ history · Tab complete a command",
		"  Scroll: mouse wheel · Shift+↑/↓ a line · PgUp/PgDn a page",
		"  Ctrl+O expand diffs · Esc interrupt · Ctrl+C quit",
		"",
		"Type / to open the command menu.",
	)
}
