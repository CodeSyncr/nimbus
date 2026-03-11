package cli

import "github.com/spf13/cobra"

// Command represents a high-level Nimbus CLI command (Artisan-style).
type Command interface {
	Name() string
	Description() string
	Run(ctx *Context) error
}

// CommandWithFlags allows a command to define its own flags.
type CommandWithFlags interface {
	Command
	Flags(cmd *cobra.Command)
}

// CommandWithAliases allows a command to define aliases.
type CommandWithAliases interface {
	Command
	Aliases() []string
}

// CommandWithArgs allows a command to specify required positional arguments.
type CommandWithArgs interface {
	Command
	Args() int // Returns the exact number of required arguments, or -1 for any
}

var registry []Command

// RegisterCommand adds a command to the global CLI registry. Framework
// code and user applications can call this from init() to auto-register
// commands before the root executes.
func RegisterCommand(c Command) {
	registry = append(registry, c)
}
