package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/CodeSyncr/nimbus/cli"
)

func init() {
	cli.RegisterCommand(&MakeDeployConfig{})
}

type MakeDeployConfig struct{}

func (c *MakeDeployConfig) Name() string        { return "make:deploy-config" }
func (c *MakeDeployConfig) Description() string { return "Create deploy.yaml for nimbus deploy" }
func (c *MakeDeployConfig) Args() int           { return 0 }
func (c *MakeDeployConfig) Run(ctx *cli.Context) error {
	path := filepath.Join(ctx.AppRoot, "deploy.yaml")
	if _, err := os.Stat(path); err == nil {
		ctx.UI.Errorf("deploy.yaml already exists")
		return fmt.Errorf("deploy.yaml already exists")
	}

	appName := filepath.Base(ctx.AppRoot)
	if appName == "." || appName == "" {
		appName = "my-app"
	}

	// We'll write the raw string to the file directly
	content := strings.Replace(deployYAMLExample, "my-app", appName, 1)

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		ctx.UI.Errorf("Failed to write deploy.yaml: %v", err)
		return err
	}

	ctx.UI.Successf("Created deploy.yaml")
	ctx.UI.Infof("Edit this file and then run: nimbus deploy fly")
	return nil
}

const deployYAMLExample = `# Nimbus Deployment Configuration
# Current supported targets: fly
# See documentation for detailed setup instructions.

app_name: my-app # The name of your app on the deployment platform
region: ord    # Preferred region (e.g. ord, iad, sfo)

env:
  # Additional environment variables to set on deploy
  # APP_ENV and PORT are set automatically
  # DB_CONNECTION and DB_DATABASE should be set dynamically via secrets

build:
  # Instructions for the Go build
  # Typically this doesn't need to change for basic Nimbus apps
  dockerfile: Dockerfile
`
