package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/CodeSyncr/nimbus/cli"
)

func init() {
	cli.RegisterCommand(&InstallCommand{})
}

type InstallCommand struct{}

func (c *InstallCommand) Name() string        { return "install" }
func (c *InstallCommand) Description() string { return "Install project dependencies (Go and frontend)" }
func (c *InstallCommand) Args() int           { return 0 }

func (c *InstallCommand) Run(ctx *cli.Context) error {
	if !isNimbusApp(ctx.AppRoot) {
		return fmt.Errorf("not a Nimbus project")
	}

	ctx.UI.Infof("Running go mod tidy...")
	if err := runInApp(ctx, "go", "mod", "tidy"); err != nil {
		ctx.UI.Errorf("go mod tidy failed: %v", err)
		return err
	}
	ctx.UI.Successf("Go dependencies installed")

	if !isInertiaInstallApp(ctx.AppRoot) {
		ctx.UI.Infof("No Inertia frontend detected; skipping JS dependency install")
		return nil
	}

	if _, err := exec.LookPath("pnpm"); err == nil {
		ctx.UI.Infof("Running pnpm install...")
		if err := runInApp(ctx, "pnpm", "install"); err != nil {
			ctx.UI.Errorf("pnpm install failed: %v", err)
			return err
		}
		ctx.UI.Successf("Frontend dependencies installed (pnpm)")
		return nil
	}

	ctx.UI.Warnf("pnpm not found; falling back to npm install")
	if err := runInApp(ctx, "npm", "install"); err != nil {
		ctx.UI.Errorf("npm install failed: %v", err)
		return err
	}
	ctx.UI.Successf("Frontend dependencies installed (npm)")
	return nil
}

func runInApp(ctx *cli.Context, bin string, args ...string) error {
	cmd := exec.Command(bin, args...)
	cmd.Dir = ctx.AppRoot
	cmd.Stdout = ctx.Stdout
	cmd.Stderr = ctx.Stderr
	return cmd.Run()
}

func isInertiaInstallApp(appRoot string) bool {
	if _, err := os.Stat(filepath.Join(appRoot, "inertia")); err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(appRoot, "package.json")); err != nil {
		return false
	}
	return true
}

