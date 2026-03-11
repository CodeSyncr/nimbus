package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/CodeSyncr/nimbus/cli"
)

func init() {
	cli.RegisterCommand(&PluginInstallCommand{})
	cli.RegisterCommand(&PluginListCommand{})
}

// PluginDef describes how to install a plugin.
type PluginDef struct {
	Name         string
	ImportPath   string
	PackageName  string
	ImportAlias  string
	Description  string
	ServerInsert string
	KernelImport string
	KernelInsert string
	EnvVars      []string
}

var pluginRegistry = map[string]PluginDef{
	"telescope": {
		Name:         "telescope",
		ImportPath:   "github.com/CodeSyncr/nimbus/plugins/telescope",
		PackageName:  "telescope",
		Description:  "Debugging and introspection tool",
		ServerInsert: "\tapp.Use(telescope.New())\n",
		KernelImport: "\t\"github.com/CodeSyncr/nimbus/plugins/telescope\"\n",
		KernelInsert: `
	if te := app.Plugin("telescope"); te != nil {
		if t, ok := te.(*telescope.Plugin); ok {
			app.Router.Use(t.RequestWatcher())
		}
	}
`,
	},
	"horizon": {
		Name:         "horizon",
		ImportPath:   "github.com/CodeSyncr/nimbus/plugins/horizon",
		PackageName:  "horizon",
		Description:  "Queue dashboard",
		ServerInsert: "\tapp.Use(horizon.New())\n",
	},
	"inertia": {
		Name:        "inertia",
		ImportPath:  "github.com/CodeSyncr/nimbus/plugins/inertia",
		PackageName: "inertia",
		Description: "Inertia.js for Vue/React/Svelte SPAs",
		ServerInsert: `	app.Use(inertia.New(inertia.Config{
		URL:          "http://localhost:3333",
		RootTemplate: "resources/views/inertia_layout.nimbus",
		Version:      "1",
	}))

`,
		EnvVars: []string{"VITE_APP_NAME={{.AppName}}"},
	},
	"unpoly": {
		Name:         "unpoly",
		ImportPath:   "github.com/CodeSyncr/nimbus/plugins/unpoly",
		PackageName:  "unpoly",
		Description:  "Unpoly.js for progressive enhancement",
		ServerInsert: "\tapp.Use(unpoly.New())\n",
		KernelImport: "\t\"github.com/CodeSyncr/nimbus/plugins/unpoly\"\n",
		KernelInsert: "\t\tunpoly.ServerProtocol(),\n",
	},
	"ai": {
		Name:         "ai",
		ImportPath:   "github.com/CodeSyncr/nimbus/plugins/ai",
		PackageName:  "ai",
		Description:  "AI integration (OpenAI, Ollama)",
		ServerInsert: "\tapp.Use(ai.New())\n",
		EnvVars: []string{
			"AI_PROVIDER=",
			"OPENAI_API_KEY=",
		},
	},
	"mcp": {
		Name:         "mcp",
		ImportPath:   "github.com/CodeSyncr/nimbus/plugins/mcp",
		PackageName:  "mcp",
		Description:  "Model Context Protocol for AI clients",
		ServerInsert: "\tapp.Use(mcp.New())\n",
	},
	"drive": {
		Name:         "drive",
		ImportPath:   "github.com/CodeSyncr/nimbus/plugins/drive",
		PackageName:  "drive",
		Description:  "File storage (fs, S3, GCS)",
		ServerInsert: "\tapp.Use(drive.New(nil))\n",
		EnvVars: []string{
			"DRIVE_DISK=fs",
		},
	},
	"transmit": {
		Name:         "transmit",
		ImportPath:   "github.com/CodeSyncr/nimbus/plugins/transmit",
		PackageName:  "transmit",
		Description:  "SSE (Server-Sent Events) for real-time streaming",
		ServerInsert: "\tapp.Use(transmit.New(nil))\n",
	},
}

var defaultPlugins = []string{"drive", "redis", "transmit"}

type PluginInstallCommand struct{}

func (c *PluginInstallCommand) Name() string        { return "plugin:install" }
func (c *PluginInstallCommand) Description() string { return "Install a new Nimbus plugin" }
func (c *PluginInstallCommand) Args() int           { return 1 }
func (c *PluginInstallCommand) Run(ctx *cli.Context) error {
	name := strings.ToLower(strings.TrimSpace(ctx.Args[0]))
	def, ok := pluginRegistry[name]
	if !ok {
		ctx.UI.Errorf("Unknown plugin %q. Run 'nimbus plugin list' for available plugins", name)
		return nil
	}

	if !isNimbusApp(ctx.AppRoot) {
		ctx.UI.Errorf("Not a Nimbus project.")
		return nil
	}

	appName := filepath.Base(ctx.AppRoot)

	ctx.UI.Infof("Installing %s...", def.Name)
	runCmd := exec.Command("go", "get", def.ImportPath)
	runCmd.Dir = ctx.AppRoot
	runCmd.Stdout = ctx.Stdout
	runCmd.Stderr = ctx.Stderr
	if err := runCmd.Run(); err != nil {
		ctx.UI.Errorf("go get failed: %v", err)
		return err
	}

	serverPath := filepath.Join(ctx.AppRoot, "bin", "server.go")
	if err := patchServerGo(serverPath, def, appName); err != nil {
		ctx.UI.Errorf("failed to patch bin/server.go: %v", err)
		return err
	}
	ctx.UI.Successf("bin/server.go updated")

	if def.KernelImport != "" || def.KernelInsert != "" {
		kernelPath := filepath.Join(ctx.AppRoot, "start", "kernel.go")
		if err := patchKernelGo(kernelPath, def); err != nil {
			ctx.UI.Errorf("failed to patch start/kernel.go: %v", err)
			return err
		}
		ctx.UI.Successf("start/kernel.go updated")
	}

	if len(def.EnvVars) > 0 {
		envPath := filepath.Join(ctx.AppRoot, ".env.example")
		if err := appendEnvVars(envPath, def.EnvVars, appName); err != nil {
			ctx.UI.Errorf("failed to update .env.example: %v", err)
			return err
		}
		ctx.UI.Successf(".env.example updated")
	}

	ctx.UI.Successf("Plugin %q installed successfully.", def.Name)

	runCmd2 := exec.Command("go", "mod", "tidy")
	runCmd2.Dir = ctx.AppRoot
	_ = runCmd2.Run()
	return nil
}

type PluginListCommand struct{}

func (c *PluginListCommand) Name() string        { return "plugin:list" }
func (c *PluginListCommand) Description() string { return "List available plugins" }
func (c *PluginListCommand) Args() int           { return 0 }
func (c *PluginListCommand) Run(ctx *cli.Context) error {
	ctx.UI.Infof("Available plugins:")
	fmt.Fprintln(ctx.Stdout)
	for name, def := range pluginRegistry {
		fmt.Fprintf(ctx.Stdout, "  %-12s %s\n", name, def.Description)
	}
	fmt.Fprintln(ctx.Stdout)
	ctx.UI.Infof("Install with: nimbus plugin install <name>")
	return nil
}

func patchServerGo(path string, def PluginDef, appName string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := string(content)

	importBlock := regexp.MustCompile(`import \(\n([\s\S]*?)\)`)
	imports := importBlock.FindStringSubmatch(s)
	if len(imports) < 2 {
		return fmt.Errorf("could not find import block in bin/server.go")
	}
	importContent := imports[1]
	if strings.Contains(importContent, def.ImportPath) {
		return nil
	}
	var newImport string
	if def.ImportAlias != "" {
		newImport = "\t" + def.ImportAlias + " \"" + def.ImportPath + "\"\n"
	} else {
		newImport = "\t\"" + def.ImportPath + "\"\n"
	}
	var newImportContent string
	nimbusImport := regexp.MustCompile(`(?m)^\t"github\.com/CodeSyncr/nimbus[^"]*"\s*$`)
	matches := nimbusImport.FindAllStringIndex(importContent, -1)
	if len(matches) > 0 {
		insertPos := matches[len(matches)-1][1]
		newImportContent = importContent[:insertPos] + newImport + importContent[insertPos:]
	} else {
		newImportContent = importContent + newImport
	}
	s = strings.Replace(s, imports[0], "import (\n"+newImportContent+")", 1)

	appNew := "app := nimbus.New()"
	newApp := appNew + "\n\n" + def.ServerInsert
	refName := def.PackageName
	if def.ImportAlias != "" {
		refName = def.ImportAlias
	}
	if strings.Contains(s, refName+".New()") {
		return nil
	}
	s = strings.Replace(s, appNew, newApp, 1)

	return os.WriteFile(path, []byte(s), 0644)
}

func patchKernelGo(path string, def PluginDef) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := string(content)

	if def.KernelImport != "" && !strings.Contains(s, def.ImportPath) {
		importBlock := regexp.MustCompile(`import \(\n([\s\S]*?)\)`)
		imports := importBlock.FindStringSubmatch(s)
		if len(imports) >= 2 {
			importContent := imports[1]
			lastImport := regexp.MustCompile(`(?m)^\t"[^"]+"\s*$`)
			matches := lastImport.FindAllStringIndex(importContent, -1)
			insertPos := len(importContent)
			if len(matches) > 0 {
				insertPos = matches[len(matches)-1][1]
			}
			newImportContent := importContent[:insertPos] + def.KernelImport + importContent[insertPos:]
			s = strings.Replace(s, imports[0], "import (\n"+newImportContent+")", 1)
		}
	}

	if def.KernelInsert != "" {
		if strings.Contains(def.KernelInsert, "app.Plugin") {
			if strings.Contains(s, "telescope.Plugin") {
				return nil
			}
			target := "middleware.Recover(),\n\t)"
			insert := "middleware.Recover(),\n\t)"
			insert += def.KernelInsert
			s = strings.Replace(s, target, insert, 1)
		} else {
			target := "middleware.Recover(),"
			insert := target + "\n" + strings.TrimSpace(def.KernelInsert)
			if strings.Contains(s, "unpoly.ServerProtocol") {
				return nil
			}
			s = strings.Replace(s, target, insert, 1)
		}
	}

	return os.WriteFile(path, []byte(s), 0644)
}

func appendEnvVars(path string, vars []string, appName string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		content = []byte{}
	}
	s := string(content)
	appName = strings.ReplaceAll(appName, "-", "_")
	for _, v := range vars {
		line := strings.ReplaceAll(v, "{{.AppName}}", appName)
		if strings.Contains(s, strings.TrimSpace(line)) {
			continue
		}
		if !strings.HasSuffix(s, "\n") && len(s) > 0 {
			s += "\n"
		}
		s += line + "\n"
	}
	return os.WriteFile(path, []byte(s), 0644)
}
