package commands

import (
	"os/exec"

	"github.com/CodeSyncr/nimbus/cli"
	"github.com/spf13/cobra"
)

func init() {
	cli.RegisterCommand(&HorizonForgetCommand{})
	cli.RegisterCommand(&HorizonClearCommand{})
}

type HorizonForgetCommand struct {
	all bool
}

func (c *HorizonForgetCommand) Name() string        { return "horizon:forget" }
func (c *HorizonForgetCommand) Description() string { return "Forget completed or failed jobs" }
func (c *HorizonForgetCommand) Args() int           { return -1 }

func (c *HorizonForgetCommand) Flags(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&c.all, "all", false, "Forget all jobs")
}

func (c *HorizonForgetCommand) Run(ctx *cli.Context) error {
	if !isNimbusApp(ctx.AppRoot) {
		ctx.UI.Errorf("Not a Nimbus app. Run from your app root.")
		return nil
	}
	runArgs := []string{"run", ".", "horizon", "forget"}
	if c.all {
		runArgs = append(runArgs, "--all")
	} else if len(ctx.Args) > 0 {
		runArgs = append(runArgs, ctx.Args[0])
	}
	cmd := exec.Command("go", runArgs...)
	cmd.Dir = ctx.AppRoot
	cmd.Stdin = ctx.Stdin
	cmd.Stdout = ctx.Stdout
	cmd.Stderr = ctx.Stderr
	return cmd.Run()
}

type HorizonClearCommand struct {
	queue string
}

func (c *HorizonClearCommand) Name() string        { return "horizon:clear" }
func (c *HorizonClearCommand) Description() string { return "Clear all jobs from a queue" }
func (c *HorizonClearCommand) Args() int           { return 0 }

func (c *HorizonClearCommand) Flags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&c.queue, "queue", "default", "The name of the queue to clear")
}

func (c *HorizonClearCommand) Run(ctx *cli.Context) error {
	if !isNimbusApp(ctx.AppRoot) {
		ctx.UI.Errorf("Not a Nimbus app. Run from your app root.")
		return nil
	}
	cmd := exec.Command("go", "run", ".", "horizon", "clear", "--queue="+c.queue)
	cmd.Dir = ctx.AppRoot
	cmd.Stdin = ctx.Stdin
	cmd.Stdout = ctx.Stdout
	cmd.Stderr = ctx.Stderr
	return cmd.Run()
}
