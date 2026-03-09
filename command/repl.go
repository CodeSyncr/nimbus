package command

import (
	"github.com/CodeSyncr/nimbus/internal/repl"
)

// ReplCommand returns a command that starts an interactive Go REPL.
// Register it to enable `go run . repl` in your app:
//
//	command.Register(command.ReplCommand())
//
// The REPL runs in your app's module context. Use :quit or Ctrl-D to exit.
func ReplCommand() *Command {
	return New("repl", "Start an interactive Go REPL").
		Long("Starts an interactive Go REPL. Use :quit or Ctrl-D to exit.").
		RunE(func(ctx *Ctx) error {
			return repl.Run()
		})
}
