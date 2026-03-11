// Package main - plugin install command and registry.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

// PluginDef describes how to install a plugin.
type PluginDef struct {
	Name        string
	ImportPath  string
	PackageName string
	ImportAlias string // e.g. "drivegcp" for import drivegcp "path"
	Description string
	// ServerInsert is inserted after "app := nimbus.New()" in bin/server.go
	ServerInsert string
	// KernelImport adds to start/kernel.go imports
	KernelImport string
	// KernelInsert is inserted after middleware.Recover() in start/kernel.go
	KernelInsert string
	// EnvVars adds to .env.example
	EnvVars []string
}

var pluginRegistry = map[string]PluginDef{
	"telescope": {
		Name:        "telescope",
		ImportPath:  "github.com/CodeSyncr/nimbus/plugins/telescope",
		PackageName: "telescope",
		Description: "Debugging and introspection tool for requests, queries, exceptions, and more",
		ServerInsert: "\tapp.Use(telescope.New())\n",
		KernelImport: "\t\"github.com/CodeSyncr/nimbus/plugins/telescope\"\n",
		KernelInsert: `
	// Telescope request watcher (must use plugin instance for shared store)
	if te := app.Plugin("telescope"); te != nil {
		if t, ok := te.(*telescope.Plugin); ok {
			app.Router.Use(t.RequestWatcher())
		}
	}
`,
	},
	"horizon": {
		Name:        "horizon",
		ImportPath:  "github.com/CodeSyncr/nimbus/plugins/horizon",
		PackageName: "horizon",
		Description: "Queue dashboard similar to Laravel Horizon (basic stats and per-queue metrics)",
		ServerInsert: "\tapp.Use(horizon.New())\n",
	},
	"inertia": {
		Name:        "inertia",
		ImportPath:  "github.com/CodeSyncr/nimbus/plugins/inertia",
		PackageName: "inertia",
		Description: "Inertia.js for Vue/React/Svelte SPAs without building an API",
		ServerInsert: `	app.Use(inertia.New(inertia.Config{
		URL:          "http://localhost:3333",
		RootTemplate: "resources/views/inertia_layout.nimbus",
		Version:      "1",
	}))

`,
		EnvVars: []string{"VITE_APP_NAME={{.AppName}}"},
	},
	"unpoly": {
		Name:        "unpoly",
		ImportPath:  "github.com/CodeSyncr/nimbus/plugins/unpoly",
		PackageName: "unpoly",
		Description: "Unpoly.js for progressive enhancement and partial page updates",
		ServerInsert: "\tapp.Use(unpoly.New())\n",
		KernelImport: "\t\"github.com/CodeSyncr/nimbus/plugins/unpoly\"\n",
		KernelInsert: "\t\tunpoly.ServerProtocol(),\n",
	},
	"ai": {
		Name:        "ai",
		ImportPath:  "github.com/CodeSyncr/nimbus/plugins/ai",
		PackageName: "ai",
		Description: "AI integration (OpenAI, Ollama) for chat and agents",
		ServerInsert: "\tapp.Use(ai.New())\n",
		EnvVars: []string{
			"# AI provider: openai or ollama (default: auto-detect)",
			"AI_PROVIDER=",
			"OPENAI_API_KEY=",
		},
	},
	"mcp": {
		Name:         "mcp",
		ImportPath:   "github.com/CodeSyncr/nimbus/plugins/mcp",
		PackageName:  "mcp",
		Description:  "Model Context Protocol for AI clients (tools, resources, prompts)",
		ServerInsert: "\tapp.Use(mcp.New())\n",
	},
	"drive": {
		Name:         "drive",
		ImportPath:   "github.com/CodeSyncr/nimbus/plugins/drive",
		PackageName:  "drive",
		Description:  "File storage (fs, S3, GCS, R2, Spaces, Supabase) - AdonisJS Drive style",
		ServerInsert: "\tapp.Use(drive.New(nil))\n",
		EnvVars: []string{
			"# Drive: fs (local), s3, gcs, r2, spaces, supabase",
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

// defaultPlugins are auto-registered when creating a new app with nimbus new.
var defaultPlugins = []string{"drive", "redis", "transmit"}

func runPluginInstall(cmd *cobra.Command, args []string) error {
	name := strings.ToLower(strings.TrimSpace(args[0]))
	def, ok := pluginRegistry[name]
	if !ok {
		return fmt.Errorf("unknown plugin %q. Run 'nimbus plugin list' for available plugins", name)
	}

	// Find project root (directory with go.mod)
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	modPath, err := findGoMod(wd)
	if err != nil {
		return fmt.Errorf("not a Nimbus project (no go.mod found): %w", err)
	}
	appName := filepath.Base(filepath.Dir(modPath))

	// 1. go get the module
	fmt.Printf("Installing %s...\n", def.Name)
	runCmd := exec.Command("go", "get", def.ImportPath)
	runCmd.Dir = filepath.Dir(modPath)
	runCmd.Stdout = os.Stdout
	runCmd.Stderr = os.Stderr
	if err := runCmd.Run(); err != nil {
		return fmt.Errorf("go get failed: %w", err)
	}

	// 2. Patch bin/server.go
	serverPath := filepath.Join(filepath.Dir(modPath), "bin", "server.go")
	if err := patchServerGo(serverPath, def, appName); err != nil {
		return fmt.Errorf("failed to patch bin/server.go: %w", err)
	}
	fmt.Println("  ✓ bin/server.go")

	// 3. Patch start/kernel.go if needed
	if def.KernelImport != "" || def.KernelInsert != "" {
		kernelPath := filepath.Join(filepath.Dir(modPath), "start", "kernel.go")
		if err := patchKernelGo(kernelPath, def); err != nil {
			return fmt.Errorf("failed to patch start/kernel.go: %w", err)
		}
		fmt.Println("  ✓ start/kernel.go")
	}

	// 4. Add env vars if needed
	if len(def.EnvVars) > 0 {
		envPath := filepath.Join(filepath.Dir(modPath), ".env.example")
		if err := appendEnvVars(envPath, def.EnvVars, appName); err != nil {
			return fmt.Errorf("failed to update .env.example: %w", err)
		}
		fmt.Println("  ✓ .env.example")
	}

	fmt.Printf("\nPlugin %q installed successfully.\n", def.Name)
	runCmd2 := exec.Command("go", "mod", "tidy")
	runCmd2.Dir = filepath.Dir(modPath)
	runCmd2.Stdout = os.Stdout
	runCmd2.Stderr = os.Stderr
	_ = runCmd2.Run()
	return nil
}

func runPluginList(cmd *cobra.Command, args []string) error {
	fmt.Println("Available plugins:")
	fmt.Println()
	for name, def := range pluginRegistry {
		fmt.Printf("  %-12s %s\n", name, def.Description)
	}
	fmt.Println()
	fmt.Println("Install with: nimbus plugin install <name>")
	return nil
}

func findGoMod(dir string) (string, error) {
	for {
		p := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found")
		}
		dir = parent
	}
}

func patchServerGo(path string, def PluginDef, appName string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := string(content)

	// Add import
	importBlock := regexp.MustCompile(`import \(\n([\s\S]*?)\)`)
	imports := importBlock.FindStringSubmatch(s)
	if len(imports) < 2 {
		return fmt.Errorf("could not find import block in bin/server.go")
	}
	importContent := imports[1]
	// Check if already imported
	if strings.Contains(importContent, def.ImportPath) {
		return nil // already installed
	}
	// Add import - prefer adding after other nimbus imports
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

	// Add app.Use after app := nimbus.New()
	appNew := "app := nimbus.New()"
	newApp := appNew + "\n\n" + def.ServerInsert
	refName := def.PackageName
	if def.ImportAlias != "" {
		refName = def.ImportAlias
	}
	if strings.Contains(s, refName+".New()") {
		return nil // already installed
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

	// Add import
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

	// Add kernel insert
	if def.KernelInsert != "" {
		// For telescope: insert block after app.Router.Use(...)
		// For unpoly: insert inside app.Router.Use( ... )
		if strings.Contains(def.KernelInsert, "app.Plugin") {
			// Telescope-style: insert after the closing ) of app.Router.Use(
			if strings.Contains(s, "telescope.Plugin") {
				return nil // already installed
			}
			target := "middleware.Recover(),\n\t)"
			insert := "middleware.Recover(),\n\t)"
			insert += def.KernelInsert
			s = strings.Replace(s, target, insert, 1)
		} else {
			// Unpoly-style: insert inside Use( ... )
			target := "middleware.Recover(),"
			insert := target + "\n" + strings.TrimSpace(def.KernelInsert)
			if strings.Contains(s, "unpoly.ServerProtocol") {
				return nil // already installed
			}
			s = strings.Replace(s, target, insert, 1)
		}
	}

	return os.WriteFile(path, []byte(s), 0644)
}

// defaultEnvVars returns env vars from default plugins to add to .env.example
func defaultEnvVars(appName string) string {
	var lines []string
	for _, name := range defaultPlugins {
		def, ok := pluginRegistry[name]
		if !ok || len(def.EnvVars) == 0 {
			continue
		}
		for _, v := range def.EnvVars {
			line := strings.ReplaceAll(v, "{{.AppName}}", appName)
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "\n" + strings.Join(lines, "\n") + "\n"
}

// buildDefaultServerContent returns bin/server.go content with auto-registered default plugins.
func buildDefaultServerContent(appName string) string {
	var imports []string
	var useCalls []string
	for _, name := range defaultPlugins {
		def, ok := pluginRegistry[name]
		if !ok {
			continue
		}
		if def.ImportAlias != "" {
			imports = append(imports, "\t"+def.ImportAlias+" \""+def.ImportPath+"\"")
		} else {
			imports = append(imports, "\t\""+def.ImportPath+"\"")
		}
		useCalls = append(useCalls, def.ServerInsert)
	}
	importBlock := ""
	for _, imp := range imports {
		importBlock += imp + "\n"
	}
	useBlock := ""
	for _, u := range useCalls {
		useBlock += u
	}
	return `/*
|--------------------------------------------------------------------------
| HTTP Server
|--------------------------------------------------------------------------
|
| This file boots the Nimbus application: it loads environment
| variables, connects to the database, registers middleware via the
| kernel, and wires up routes.
|
| Run migrations: go run . migrate  (or: nimbus db:migrate)
|
*/

package bin

import (
	"context"
	"fmt"
	"os"

	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/database"
	"github.com/CodeSyncr/nimbus/queue"
` + importBlock + `
	"` + appName + `/config"
	"` + appName + `/database/migrations"
	"` + appName + `/start"
)

func Boot() *nimbus.App {
	config.Load()

	app := nimbus.New()

` + useBlock + `
	start.RegisterMiddleware(app)

	start.RegisterRoutes(app)

	db, err := database.Connect(config.Database.Driver, config.Database.DSN)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Database connection failed: %v\n", err)
		os.Exit(1)
	}
	_ = db

	queue.Boot(&queue.BootConfig{RegisterJobs: start.RegisterQueueJobs})

	return app
}

// RunQueueWorker runs the queue worker. Called when main is invoked with "queue:work" arg.
func RunQueueWorker() {
	app := Boot()
	if err := app.Boot(); err != nil {
		fmt.Fprintf(os.Stderr, "Boot failed: %v\n", err)
		os.Exit(1)
	}
	queue.RunWorker(context.Background(), "default")
}

// RunMigrations runs database migrations. Called when main is invoked with "migrate" arg.
func RunMigrations() {
	config.Load()
	// Ensure database exists before running migrations.
	if err := database.CreateDatabaseIfNotExists(database.CreateConfig{
		Driver:   config.Database.Driver,
		Host:     config.Database.Host,
		Port:     config.Database.Port,
		User:     config.Database.User,
		Password: config.Database.Password,
		Database: config.Database.Database,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Database create failed: %v\n", err)
		os.Exit(1)
	}
	db, err := database.Connect(config.Database.Driver, config.Database.DSN)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Database connection failed: %v\n", err)
		os.Exit(1)
	}
	migrator := database.NewMigrator(db, migrations.All())
	if err := migrator.Up(); err != nil {
		fmt.Fprintf(os.Stderr, "Migration failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Migrations completed.")
}

// RunDbCreate creates the configured database if it does not exist.
func RunDbCreate() {
	config.Load()
	if err := database.CreateDatabaseIfNotExists(database.CreateConfig{
		Driver:   config.Database.Driver,
		Host:     config.Database.Host,
		Port:     config.Database.Port,
		User:     config.Database.User,
		Password: config.Database.Password,
		Database: config.Database.Database,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Database create failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Database created (or already exists).")
}
`
}

func createStartJobsGo(path string, appName string) error {
	if _, err := os.Stat(path); err == nil {
		return nil // already exists
	}
	content := `/*
|--------------------------------------------------------------------------
| Queue Job Registration
|--------------------------------------------------------------------------
|
| Register your jobs here. They will be available for queue.Dispatch().
| Add: import "` + appName + `/app/jobs" and queue.Register(&jobs.YourJob{})
|
*/

package start

func RegisterQueueJobs() {
	// queue.Register(&jobs.SendEmail{})
}
`
	return os.WriteFile(path, []byte(content), 0644)
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
