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
	// ConfigFiles maps relative file path → content to scaffold on install.
	ConfigFiles map[string]string
	// ConfigLoaderInsert is the function call (e.g. "loadTelescope()") to
	// append to config/config.go's Load() function when installing the plugin.
	ConfigLoaderInsert string

	// ServerBootInsert is a line (e.g. "bootNoSQL(app)") inserted after the
	// "bootDatabase(app)" call in bin/server.go for boot-level integrations
	// that are not standard app.Use() plugins.
	ServerBootInsert string
	// ServerFuncInsert is a full Go function body appended to the end of
	// bin/server.go (e.g. the bootNoSQL function).
	ServerFuncInsert string
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
		ConfigFiles: map[string]string{
			"config/telescope.go": telescopeConfigFile,
		},
		ConfigLoaderInsert: "loadTelescope()",
		EnvVars: []string{
			"TELESCOPE_ENABLED=true",
		},
	},
	"horizon": {
		Name:         "horizon",
		ImportPath:   "github.com/CodeSyncr/nimbus/plugins/horizon",
		PackageName:  "horizon",
		Description:  "Queue dashboard",
		ServerInsert: "\tapp.Use(horizon.New())\n",
		ConfigFiles: map[string]string{
			"config/horizon.go": horizonConfigFile,
		},
		ConfigLoaderInsert: "loadHorizon()",
		EnvVars: []string{
			"HORIZON_PATH=/horizon",
		},
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
		ConfigFiles: map[string]string{
			"config/transmit.go": transmitConfigFile,
		},
		ConfigLoaderInsert: "loadTransmit()",
		EnvVars: []string{
			"TRANSMIT_TRANSPORT=",
		},
	},
	"scout": {
		Name:         "scout",
		ImportPath:   "github.com/CodeSyncr/nimbus/search",
		PackageName:  "search",
		ImportAlias:  "search",
		Description:  "Full-text search (Postgres, Meilisearch, Typesense)",
		ServerInsert: "\tapp.Use(search.NewPlugin(nil))\n",
		EnvVars: []string{
			"SEARCH_DRIVER=postgres",
		},
	},
	"pulse": {
		Name:         "pulse",
		ImportPath:   "github.com/CodeSyncr/nimbus/plugins/pulse",
		PackageName:  "pulse",
		Description:  "Lightweight app monitoring & metrics",
		ServerInsert: "\tapp.Use(pulse.NewPlugin())\n",
		KernelImport: "\t\"github.com/CodeSyncr/nimbus/plugins/pulse\"\n",
		KernelInsert: `
	if pu := app.Plugin("pulse"); pu != nil {
		if p, ok := pu.(*pulse.PulsePlugin); ok {
			app.Router.Use(p.Pulse.Middleware())
		}
	}
`,
	},
	"nosql": {
		Name:             "nosql",
		ImportPath:       "github.com/CodeSyncr/nimbus/database/nosql",
		PackageName:      "nosql",
		Description:      "NoSQL / MongoDB support with query builder",
		ServerBootInsert: "\tbootNoSQL(app)\n",
		ServerFuncInsert: nosqlBootFunc,
		ConfigFiles: map[string]string{
			"config/nosql.go": nosqlConfigFile,
		},
		ConfigLoaderInsert: "loadNoSQL()",
		EnvVars: []string{
			"MONGO_URI=mongodb://localhost:27017",
			"MONGO_DATABASE=nimbus",
		},
	},
	"socialite": {
		Name:         "socialite",
		ImportPath:   "github.com/CodeSyncr/nimbus/auth/socialite",
		PackageName:  "socialite",
		Description:  "OAuth social authentication (GitHub, Google, Discord, Apple)",
		ServerInsert: socialiteServerInsert,
		EnvVars: []string{
			"GITHUB_CLIENT_ID=",
			"GITHUB_CLIENT_SECRET=",
			"GITHUB_REDIRECT_URL=http://localhost:3333/auth/github/callback",
			"GOOGLE_CLIENT_ID=",
			"GOOGLE_CLIENT_SECRET=",
			"GOOGLE_REDIRECT_URL=http://localhost:3333/auth/google/callback",
			"DISCORD_CLIENT_ID=",
			"DISCORD_CLIENT_SECRET=",
			"DISCORD_REDIRECT_URL=http://localhost:3333/auth/discord/callback",
		},
		ConfigFiles: map[string]string{
			"config/socialite.go": socialiteConfigFile,
		},
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

	// Scaffold config files
	if len(def.ConfigFiles) > 0 {
		for relPath, content := range def.ConfigFiles {
			fullPath := filepath.Join(ctx.AppRoot, relPath)
			_ = os.MkdirAll(filepath.Dir(fullPath), 0755)
			if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
				ctx.UI.Warnf("failed to create %s: %v", relPath, err)
			} else {
				ctx.UI.Successf("%s created", relPath)
			}
		}
	}

	// Add loadXxx() call to config/config.go
	if def.ConfigLoaderInsert != "" {
		configPath := filepath.Join(ctx.AppRoot, "config", "config.go")
		if err := patchConfigGo(configPath, def.ConfigLoaderInsert); err != nil {
			ctx.UI.Warnf("failed to patch config/config.go: %v", err)
		} else {
			ctx.UI.Successf("config/config.go updated")
		}
	}

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

	// Standard plugin: insert after app := nimbus.New()
	if def.ServerInsert != "" {
		appNew := "app := nimbus.New()"
		newApp := appNew + "\n\n" + def.ServerInsert
		refName := def.PackageName
		if def.ImportAlias != "" {
			refName = def.ImportAlias
		}
		if strings.Contains(s, refName+".") {
			// Plugin already referenced in server.go
			return os.WriteFile(path, []byte(s), 0644)
		}
		s = strings.Replace(s, appNew, newApp, 1)
	}

	// Boot-level integration: insert call after bootDatabase(app)
	if def.ServerBootInsert != "" && !strings.Contains(s, strings.TrimSpace(def.ServerBootInsert)) {
		anchor := "bootDatabase(app)"
		if idx := strings.Index(s, anchor); idx >= 0 {
			// Find end of the line containing bootDatabase(app)
			endOfLine := idx + len(anchor)
			for endOfLine < len(s) && s[endOfLine] != '\n' {
				endOfLine++
			}
			if endOfLine < len(s) {
				endOfLine++ // include the newline
			}
			s = s[:endOfLine] + def.ServerBootInsert + s[endOfLine:]
		}
	}

	// Append a full function to end of file
	if def.ServerFuncInsert != "" && !strings.Contains(s, strings.TrimSpace(def.ServerFuncInsert[:min(80, len(def.ServerFuncInsert))])) {
		if !strings.HasSuffix(s, "\n") {
			s += "\n"
		}
		s += "\n" + def.ServerFuncInsert
	}

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
			// Check if already present
			refPkg := def.PackageName
			if def.ImportAlias != "" {
				refPkg = def.ImportAlias
			}
			if strings.Contains(s, refPkg+".Plugin") || strings.Contains(s, refPkg+".PulsePlugin") {
				return os.WriteFile(path, []byte(s), 0644)
			}
			// Find the closing of the app.Router.Use(...) block.
			// We search for "app.Router.Use(" then find its matching ")".
			useIdx := strings.Index(s, "app.Router.Use(")
			if useIdx >= 0 {
				depth := 0
				insertAt := -1
				for i := useIdx; i < len(s); i++ {
					if s[i] == '(' {
						depth++
					} else if s[i] == ')' {
						depth--
						if depth == 0 {
							insertAt = i + 1
							break
						}
					}
				}
				if insertAt > 0 {
					s = s[:insertAt] + "\n" + def.KernelInsert + s[insertAt:]
				}
			}
		} else {
			target := "middleware.Recover(),"
			insert := target + "\n" + strings.TrimSpace(def.KernelInsert)
			if strings.Contains(s, strings.TrimSpace(def.KernelInsert)) {
				return os.WriteFile(path, []byte(s), 0644)
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

// patchConfigGo adds a loader function call (e.g. "loadTelescope()") to
// the Load() function inside config/config.go, right before the closing
// brace. It is idempotent — a duplicate call is never inserted.
func patchConfigGo(path, loaderCall string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := string(content)

	// Already present?
	if strings.Contains(s, loaderCall) {
		return nil
	}

	// Find the last "}" which closes func Load().
	// We insert "\tloaderCall\n" just before it.
	idx := strings.LastIndex(s, "}")
	if idx < 0 {
		return fmt.Errorf("could not find closing brace in config/config.go")
	}
	insert := "\t" + loaderCall + "\n"
	s = s[:idx] + insert + s[idx:]
	return os.WriteFile(path, []byte(s), 0644)
}
