package commands

import (
	"os/exec"

	"github.com/CodeSyncr/nimbus/cli"
)

func init() {
	cli.RegisterCommand(&QueueWorkCommand{})
	cli.RegisterCommand(&ScheduleRunCommand{})
	cli.RegisterCommand(&ScheduleListCommand{})
}

type QueueWorkCommand struct{}

func (c *QueueWorkCommand) Name() string        { return "queue:work" }
func (c *QueueWorkCommand) Description() string { return "Start processing jobs on the queue" }
func (c *QueueWorkCommand) Args() int           { return 0 }
func (c *QueueWorkCommand) Run(ctx *cli.Context) error {
	if !isNimbusApp(ctx.AppRoot) {
		ctx.UI.Errorf("Not a Nimbus app. Run 'nimbus queue:work' from your app root.")
		return nil
	}
	cmd := exec.Command("go", "run", ".", "queue:work")
	cmd.Dir = ctx.AppRoot
	cmd.Stdin = ctx.Stdin
	cmd.Stdout = ctx.Stdout
	cmd.Stderr = ctx.Stderr
	return cmd.Run()
}

type ScheduleRunCommand struct{}

func (c *ScheduleRunCommand) Name() string        { return "schedule:run" }
func (c *ScheduleRunCommand) Description() string { return "Run the scheduled tasks" }
func (c *ScheduleRunCommand) Args() int           { return 0 }
func (c *ScheduleRunCommand) Run(ctx *cli.Context) error {
	if !isNimbusApp(ctx.AppRoot) {
		ctx.UI.Errorf("Not a Nimbus app. Run 'nimbus schedule:run' from your app root.")
		return nil
	}
	cmd := exec.Command("go", "run", ".", "schedule:run")
	cmd.Dir = ctx.AppRoot
	cmd.Stdin = ctx.Stdin
	cmd.Stdout = ctx.Stdout
	cmd.Stderr = ctx.Stderr
	return cmd.Run()
}

type ScheduleListCommand struct{}

func (c *ScheduleListCommand) Name() string        { return "schedule:list" }
func (c *ScheduleListCommand) Description() string { return "List all scheduled tasks" }
func (c *ScheduleListCommand) Args() int           { return 0 }
func (c *ScheduleListCommand) Run(ctx *cli.Context) error {
	if !isNimbusApp(ctx.AppRoot) {
		ctx.UI.Errorf("Not a Nimbus app. Run 'nimbus schedule:list' from your app root.")
		return nil
	}
	cmd := exec.Command("go", "run", ".", "schedule:list")
	cmd.Dir = ctx.AppRoot
	cmd.Stdin = ctx.Stdin
	cmd.Stdout = ctx.Stdout
	cmd.Stderr = ctx.Stderr
	return cmd.Run()
}
