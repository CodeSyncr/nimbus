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
	cli.RegisterCommand(&MakeAPIToken{})
}

// MakeAPIToken scaffolds the personal_access_tokens migration and an example
// API token controller so developers can quickly add API key authentication.
type MakeAPIToken struct{}

func (c *MakeAPIToken) Name() string { return "make:api-token" }
func (c *MakeAPIToken) Description() string {
	return "Scaffold API token authentication (migration + controller)"
}
func (c *MakeAPIToken) Args() int { return 0 }

func (c *MakeAPIToken) Run(ctx *cli.Context) error {
	// 1. Create migration
	ts := cli.Timestamp()
	_ = os.MkdirAll("database/migrations", 0755)
	migBaseName := ts + "_create_personal_access_tokens"
	migFileName := migBaseName + ".go"
	migPath := filepath.Join("database", "migrations", migFileName)

	data := generators.Data{
		"MigrationName": migBaseName,
	}
	if err := generators.RenderToFile("auth/api_token_migration.tmpl", migPath, data); err != nil {
		ctx.UI.Errorf("Failed to create migration: %v", err)
		return err
	}
	ctx.UI.Successf("Created migration at %s", migPath)

	// Auto-register migration in registry.go
	registryPath := filepath.Join("database", "migrations", "registry.go")
	if regData, err := os.ReadFile(registryPath); err == nil {
		regStr := string(regData)
		if !strings.Contains(regStr, "CreatePersonalAccessTokens") {
			lines := strings.Split(regStr, "\n")
			closeIdx := -1
			for i, line := range lines {
				if strings.Contains(line, "return []database.Migration{") {
					for j := i + 1; j < len(lines); j++ {
						if strings.TrimSpace(lines[j]) == "}" {
							closeIdx = j
							break
						}
					}
					break
				}
			}
			if closeIdx >= 0 {
				newLine := fmt.Sprintf("\t\t{Name: %q, Up: (&CreatePersonalAccessTokens{}).Up, Down: (&CreatePersonalAccessTokens{}).Down},", migBaseName)
				newLines := append(lines[:closeIdx], newLine)
				newLines = append(newLines, lines[closeIdx:]...)
				_ = os.WriteFile(registryPath, []byte(strings.Join(newLines, "\n")), 0644)
				ctx.UI.Successf("Auto-registered migration in %s", registryPath)
			}
		}
	}

	// 2. Create API token controller
	module := "nimbus-app"
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
	controllerPath := "app/controllers/api_token_controller.go"
	if _, err := os.Stat(controllerPath); os.IsNotExist(err) {
		if err := generators.RenderToFile("auth/api_token_controller.tmpl", controllerPath, generators.Data{"Module": module}); err != nil {
			ctx.UI.Errorf("Failed to create controller: %v", err)
			return err
		}
		ctx.UI.Successf("Created controller at %s", controllerPath)
	}

	ctx.UI.Panel("API Token Auth setup",
		"1. Run: nimbus db:migrate\n"+
			"2. Register the TokenGuard in start/kernel.go:\n"+
			"     tokenGuard := auth.NewTokenGuard(db, userLoader)\n"+
			"3. Add API routes in start/routes.go:\n"+
			"     api := app.Group(\"/api\")\n"+
			"     api.Use(auth.RequireToken(tokenGuard))\n"+
			"     api.GET(\"/tokens\", ctrl.ListTokens)\n"+
			"     api.POST(\"/tokens\", ctrl.CreateToken)\n"+
			"     api.DELETE(\"/tokens/:id\", ctrl.RevokeToken)\n"+
			"4. Use auth.RequireAbility(\"scope\") for fine-grained access",
	)

	return nil
}
