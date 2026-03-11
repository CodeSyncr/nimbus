package commands

import (
	"github.com/CodeSyncr/nimbus/cli"
	"github.com/CodeSyncr/nimbus/internal/repl"
)

func init() {
	cli.RegisterCommand(&ReplCommand{})
}

type ReplCommand struct{}

func (c *ReplCommand) Name() string        { return "repl" }
func (c *ReplCommand) Description() string { return "Start a Nimbus REPL session" }
func (c *ReplCommand) Args() int           { return 0 }
func (c *ReplCommand) Run(ctx *cli.Context) error {
	return repl.Run()
}
