package cli

import "github.com/spf13/cobra"

// Root wraps the Cobra root command for the Nimbus CLI.
type Root struct {
	*cobra.Command
}

// NewRoot constructs a new Root from an existing Cobra root command.
// It is intended to be called from cmd/nimbus/main.go with the rootCmd
// defined there, so that the cli package can attach additional behavior
// (like auto-registered commands) before executing.
func NewRoot(root *cobra.Command) *Root {
	return &Root{Command: root}
}

// Execute attaches all registered commands to the given root and runs it.
func (r *Root) Execute() error {
	for _, c := range registry {
		cmd := &cobra.Command{
			Use:   c.Name(),
			Short: c.Description(),
			RunE: func(cmd *cobra.Command, args []string) error {
				ctx := NewContext(cmd, args)
				return c.Run(ctx)
			},
		}

		if cf, ok := c.(CommandWithFlags); ok {
			cf.Flags(cmd)
		}
		if ca, ok := c.(CommandWithAliases); ok {
			cmd.Aliases = ca.Aliases()
		}
		if cargs, ok := c.(CommandWithArgs); ok {
			if cargs.Args() >= 0 {
				cmd.Args = cobra.ExactArgs(cargs.Args())
			}
		}

		r.AddCommand(cmd)
	}
	return r.Command.Execute()
}
