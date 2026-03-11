package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/CodeSyncr/nimbus/cli"
)

func init() {
	cli.RegisterCommand(&DBMigrate{})
	cli.RegisterCommand(&DBCreate{})
	cli.RegisterCommand(&DBSeed{})
	cli.RegisterCommand(&DBRollback{})
}

func isNimbusApp(dir string) bool {
	modPath := filepath.Join(dir, "go.mod")
	mainPath := filepath.Join(dir, "main.go")
	mod, err := os.ReadFile(modPath)
	if err != nil {
		return false
	}
	if _, err := os.Stat(mainPath); err != nil {
		return false
	}
	return strings.Contains(string(mod), "CodeSyncr/nimbus") || strings.Contains(string(mod), "nimbus-framework/nimbus")
}

// -----------------------------------------------------------------------------
// DB Commands
// -----------------------------------------------------------------------------

type DBMigrate struct{}

func (c *DBMigrate) Name() string        { return "db:migrate" }
func (c *DBMigrate) Description() string { return "Run pending database migrations" }
func (c *DBMigrate) Args() int           { return 0 }
func (c *DBMigrate) Run(ctx *cli.Context) error {
	if !isNimbusApp(ctx.AppRoot) {
		ctx.UI.Errorf("Not a Nimbus app. Run 'nimbus db:migrate' from your app root.")
		return nil
	}
	cmd := exec.Command("go", "run", ".", "migrate")
	cmd.Dir = ctx.AppRoot
	cmd.Stdout = ctx.Stdout
	cmd.Stderr = ctx.Stderr
	return cmd.Run()
}

type DBCreate struct{}

func (c *DBCreate) Name() string        { return "db:create" }
func (c *DBCreate) Description() string { return "Create the database based on configuration" }
func (c *DBCreate) Args() int           { return 0 }
func (c *DBCreate) Run(ctx *cli.Context) error {
	if !isNimbusApp(ctx.AppRoot) {
		ctx.UI.Errorf("Not a Nimbus app. Run 'nimbus db:create' from your app root.")
		return nil
	}
	cmd := exec.Command("go", "run", ".", "db:create")
	cmd.Dir = ctx.AppRoot
	cmd.Stdout = ctx.Stdout
	cmd.Stderr = ctx.Stderr
	return cmd.Run()
}

type DBSeed struct{}

func (c *DBSeed) Name() string        { return "db:seed" }
func (c *DBSeed) Description() string { return "Seed the database with records" }
func (c *DBSeed) Args() int           { return 0 }
func (c *DBSeed) Run(ctx *cli.Context) error {
	if !isNimbusApp(ctx.AppRoot) {
		ctx.UI.Errorf("Not a Nimbus app. Run 'nimbus db:seed' from your app root.")
		return nil
	}
	cmd := exec.Command("go", "run", ".", "seed")
	cmd.Dir = ctx.AppRoot
	cmd.Stdout = ctx.Stdout
	cmd.Stderr = ctx.Stderr
	return cmd.Run()
}

type DBRollback struct{}

func (c *DBRollback) Name() string        { return "db:rollback" }
func (c *DBRollback) Description() string { return "Rollback the last database migration" }
func (c *DBRollback) Args() int           { return 0 }
func (c *DBRollback) Run(ctx *cli.Context) error {
	if !isNimbusApp(ctx.AppRoot) {
		ctx.UI.Errorf("Not a Nimbus app. Run 'nimbus db:rollback' from your app root.")
		return nil
	}
	fmt.Fprintln(ctx.Stdout, "Rollback: run migrator.Down() from your app (e.g. `go run . rollback`).")
	return nil
}
