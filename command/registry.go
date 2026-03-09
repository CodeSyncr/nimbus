package command

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var list []*Command

// Register adds a command to the registry. Called from init() in each command file.
func Register(c *Command) {
	list = append(list, c)
}

// All returns all registered commands.
func All() []*Command {
	return list
}

// Run executes the CLI with the given args. Use from main.go when len(os.Args) > 1.
// The first arg is the command name; remaining args are passed to that command.
func Run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("no command specified")
	}
	root := &cobra.Command{
		Use:   "app",
		Short: "Nimbus application",
	}
	for _, c := range list {
		root.AddCommand(c.cobra())
	}
	root.SetArgs(args)
	return root.Execute()
}

// RunOrExit runs the CLI and exits with code 1 on error.
func RunOrExit(args []string) {
	if err := Run(args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
