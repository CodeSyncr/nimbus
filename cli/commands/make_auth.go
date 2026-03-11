package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/CodeSyncr/nimbus/cli"
	"github.com/CodeSyncr/nimbus/cli/generators"
)

func init() {
	cli.RegisterCommand(&MakeAuth{})
}

type MakeAuth struct{}

func (c *MakeAuth) Name() string { return "make:auth" }
func (c *MakeAuth) Description() string {
	return "Scaffold basic authentication (models, controllers, views)"
}
func (c *MakeAuth) Args() int { return 0 }
func (c *MakeAuth) Run(ctx *cli.Context) error {
	_ = os.MkdirAll("app/models", 0755)
	if err := generators.RenderToFile("auth/user.tmpl", "app/models/user.go", generators.Data{}); err != nil {
		return err
	}

	ts := cli.Timestamp()
	_ = os.MkdirAll("database/migrations", 0755)
	migName := ts + "_create_users.go"
	if err := generators.RenderToFile("auth/migration.tmpl", filepath.Join("database/migrations", migName), generators.Data{}); err != nil {
		return err
	}

	// Auto-register users migration in registry.go
	registryPath := filepath.Join("database", "migrations", "registry.go")
	if regData, err := os.ReadFile(registryPath); err == nil {
		regStr := string(regData)
		if !strings.Contains(regStr, migName[:len(migName)-3]) {
			// Find return []database.Migration{
			lines := strings.Split(regStr, "\n")
			start := -1
			for i, line := range lines {
				if strings.Contains(line, "return []database.Migration{") {
					start = i
					break
				}
			}
			closeIdx := -1
			if start >= 0 {
				for i := start + 1; i < len(lines); i++ {
					if strings.TrimSpace(lines[i]) == "}" {
						closeIdx = i
						break
					}
				}
			}
			if closeIdx >= 0 {
				newLine := fmt.Sprintf("\t\t{Name: %q, Up: (&CreateUsers{}).Up, Down: (&CreateUsers{}).Down},", migName[:len(migName)-3])
				newLines := append(lines[:closeIdx], newLine)
				newLines = append(newLines, lines[closeIdx:]...)
				_ = os.WriteFile(registryPath, []byte(strings.Join(newLines, "\n")), 0644)
			}
		}
	}

	// Read module from go.mod
	module := "nimbus-starter"
	if modData, err := os.ReadFile("go.mod"); err == nil {
		for _, line := range strings.Split(string(modData), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "module ") {
				module = strings.TrimPrefix(line, "module ")
				break
			}
		}
	}

	_ = os.MkdirAll("app/controllers", 0755)
	if err := generators.RenderToFile("auth/controller.tmpl", "app/controllers/auth_controller.go", generators.Data{"Module": module}); err != nil {
		return err
	}

	_ = os.MkdirAll("resources/views/auth", 0755)
	if err := generators.RenderToFile("auth/login.tmpl", "resources/views/auth/login.nimbus", generators.Data{}); err != nil {
		return err
	}
	if err := generators.RenderToFile("auth/register.tmpl", "resources/views/auth/register.nimbus", generators.Data{}); err != nil {
		return err
	}

	ctx.UI.Successf("Created auth scaffold:")
	ctx.UI.Infof("  - app/models/user.go")
	ctx.UI.Infof("  - database/migrations/%s", migName)
	ctx.UI.Infof("  - app/controllers/auth_controller.go")
	ctx.UI.Infof("  - resources/views/auth/login.nimbus, resources/views/auth/register.nimbus")

	ctx.UI.Panel("Next steps",
		"1. Add migration to database/migrations/registry.go\n"+
			"2. Add session middleware and auth routes in start/routes.go and start/kernel.go\n"+
			"3. Run: nimbus db:migrate",
	)

	return nil
}
