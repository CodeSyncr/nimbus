package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/CodeSyncr/nimbus/cli"
	"github.com/CodeSyncr/nimbus/internal/version"
	"github.com/spf13/cobra"
)

func init() {
	cli.RegisterCommand(&NewCommand{})
}

type NewCommand struct {
	noDefaults bool
	kit        string
}

func (c *NewCommand) Name() string        { return "new" }
func (c *NewCommand) Description() string { return "Create a new Nimbus application" }
func (c *NewCommand) Aliases() []string   { return []string{"create"} }
func (c *NewCommand) Args() int           { return -1 }

func (c *NewCommand) Flags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&c.kit, "kit", "", "Frontend kit: react, vue, svelte")
	cmd.Flags().BoolVar(&c.noDefaults, "no-default-plugins", false, "Skip auto-registering default plugins")
}

func (c *NewCommand) Run(ctx *cli.Context) error {
	ctx.UI.Panel("Nimbus Setup", "Welcome to Nimbus! Let's scaffold your new project.")

	var name string
	if len(ctx.Args) > 0 {
		name = strings.TrimSpace(ctx.Args[0])
	} else {
		ans, err := ctx.UI.AskInput("Project name", "example-app")
		if err != nil {
			return err
		}
		name = strings.TrimSpace(ans)
	}

	interactive := c.kit == ""

	dbDriver := "sqlite"
	var selectedPlugins []string
	authGuard := "none"

	if interactive {
		pt, err := ctx.UI.AskSelect("Select project type", []string{"Basic (server-rendered views)", "Inertia (Inertia.js SPA)"}, "Basic (server-rendered views)")
		if err != nil {
			return err
		}
		if pt == "Inertia (Inertia.js SPA)" {
			c.kit = "react"
			ctx.UI.Infof("Using React Inertia kit (Vue and Svelte coming soon).")
		} else {
			c.kit = ""
		}

		dbAns, err := ctx.UI.AskSelect("Select database", []string{"SQLite (default)", "Postgres", "MySQL"}, "SQLite (default)")
		if err != nil {
			return err
		}
		switch dbAns {
		case "Postgres":
			dbDriver = "postgres"
		case "MySQL":
			dbDriver = "mysql"
		default:
			dbDriver = "sqlite"
		}

		// ── Authentication Guard ────────────────────────────────
		wantAuth, err := ctx.UI.AskConfirm("Would you like to add authentication?", true)
		if err != nil {
			return err
		}
		if wantAuth {
			guardAns, err := ctx.UI.AskSelect("Select auth guard", []string{
				"Session (cookie-based, ideal for web apps & SPAs on same domain)",
				"Access Token (opaque tokens, ideal for APIs, mobile & 3rd-party)",
				"Basic Auth (HTTP basic, ideal for internal tools & development)",
			}, "Session (cookie-based, ideal for web apps & SPAs on same domain)")
			if err != nil {
				return err
			}
			switch {
			case strings.Contains(guardAns, "Session"):
				authGuard = "session"
			case strings.Contains(guardAns, "Access Token"):
				authGuard = "access_token"
			case strings.Contains(guardAns, "Basic Auth"):
				authGuard = "basic"
			}
		}

		options := []string{
			"AI (OpenAI/Ollama)",
			"MCP (Model Context Protocol)",
			"Telescope (debug dashboard)",
			"Pulse (app monitoring & metrics)",
			"Scout (full-text search)",
			"Socialite (OAuth social login)",
		}
		if c.kit == "" {
			options = append(options, "Unpoly (progressive enhancement)")
		}

		plAns, err := ctx.UI.AskMultiSelect("Select plugins", options, nil)
		if err != nil {
			return err
		}
		for _, o := range plAns {
			if strings.Contains(o, "AI") {
				selectedPlugins = append(selectedPlugins, "ai")
			} else if strings.Contains(o, "MCP") {
				selectedPlugins = append(selectedPlugins, "mcp")
			} else if strings.Contains(o, "Telescope") {
				selectedPlugins = append(selectedPlugins, "telescope")
			} else if strings.Contains(o, "Pulse") {
				selectedPlugins = append(selectedPlugins, "pulse")
			} else if strings.Contains(o, "Scout") {
				selectedPlugins = append(selectedPlugins, "scout")
			} else if strings.Contains(o, "Socialite") {
				selectedPlugins = append(selectedPlugins, "socialite")
			} else if strings.Contains(o, "Unpoly") {
				selectedPlugins = append(selectedPlugins, "unpoly")
			}
		}
	} else {
		if c.kit != "" && c.kit != "react" && c.kit != "vue" && c.kit != "svelte" {
			return fmt.Errorf("invalid --kit=%q: must be react, vue, svelte, or empty", c.kit)
		}
	}

	dir := name
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	baseDirs := []string{
		filepath.Join(dir, "app", "controllers"),
		filepath.Join(dir, "app", "models"),
		filepath.Join(dir, "app", "middleware"),
		filepath.Join(dir, "app", "jobs"),
		filepath.Join(dir, "bin"),
		filepath.Join(dir, "config"),
		filepath.Join(dir, "database", "migrations"),
		filepath.Join(dir, "database", "seeders"),
		filepath.Join(dir, "start"),
		filepath.Join(dir, "public"),
	}
	if c.kit == "" {
		baseDirs = append(baseDirs,
			filepath.Join(dir, "resources", "views"),
			filepath.Join(dir, "resources", "css"),
			filepath.Join(dir, "resources", "js"),
		)
	} else {
		baseDirs = append(baseDirs,
			filepath.Join(dir, "resources", "views"),
			filepath.Join(dir, "resources", "css"),
			filepath.Join(dir, "resources", "js"),
			filepath.Join(dir, "inertia", "pages", "home"),
			filepath.Join(dir, "inertia", "layouts"),
			filepath.Join(dir, "inertia", "Pages", "Home"),
		)
	}
	for _, d := range baseDirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return err
		}
	}

	mod := goModContent(name, c.kit)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0644); err != nil {
		return err
	}

	t := template.Must(template.New("main").Parse(mainTmpl))
	f, _ := os.Create(filepath.Join(dir, "main.go"))
	_ = t.Execute(f, map[string]string{"AppName": name})
	_ = f.Close()

	if c.kit == "" {
		var serverContent string
		if c.noDefaults {
			ts := template.Must(template.New("server").Parse(binServerTmpl))
			var buf strings.Builder
			_ = ts.Execute(&buf, map[string]string{"AppName": name})
			serverContent = buf.String()
		} else {
			serverContent = buildDefaultServerContent(name)
		}
		_ = os.WriteFile(filepath.Join(dir, "bin", "server.go"), []byte(serverContent), 0644)

		tr := template.Must(template.New("routes").Parse(routesStub))
		rf, _ := os.Create(filepath.Join(dir, "start", "routes.go"))
		_ = tr.Execute(rf, map[string]string{"AppName": name})
		_ = rf.Close()

		// we simulate view template copying from old system
		_ = os.WriteFile(filepath.Join(dir, "resources", "views", "home.nimbus"), []byte(homeViewTmpl), 0644)
		_ = os.WriteFile(filepath.Join(dir, "resources", "views", "layout.nimbus"), []byte(layoutViewTmpl), 0644)

	} else {
		ts := template.Must(template.New("server").Parse(binServerInertiaTmpl))
		sf, _ := os.Create(filepath.Join(dir, "bin", "server.go"))
		_ = ts.Execute(sf, map[string]string{"AppName": name})
		_ = sf.Close()

		tr := template.Must(template.New("routes").Parse(routesInertiaStub))
		rf, _ := os.Create(filepath.Join(dir, "start", "routes.go"))
		_ = tr.Execute(rf, map[string]string{"AppName": name})
		_ = rf.Close()

		if err := createInertiaKit(dir, name, c.kit); err != nil {
			return err
		}
	}

	_ = os.WriteFile(filepath.Join(dir, "start", "kernel.go"), []byte(kernelStub), 0644)

	startJobsPath := filepath.Join(dir, "start", "jobs.go")
	startJobsContent := "package start\n\nfunc RegisterQueueJobs() {}\n"
	_ = os.WriteFile(startJobsPath, []byte(startJobsContent), 0644)

	_ = os.WriteFile(filepath.Join(dir, "config", "config.go"), []byte(configLoader), 0644)
	_ = os.WriteFile(filepath.Join(dir, "config", "app.go"), []byte(configApp), 0644)
	_ = os.WriteFile(filepath.Join(dir, "config", "database.go"), []byte(configDatabase), 0644)
	_ = os.WriteFile(filepath.Join(dir, "config", "env.go"), []byte(configEnv), 0644)
	_ = os.WriteFile(filepath.Join(dir, "config", "cors.go"), []byte(configCORS), 0644)
	_ = os.WriteFile(filepath.Join(dir, "config", "session.go"), []byte(configSession), 0644)
	_ = os.WriteFile(filepath.Join(dir, "config", "hash.go"), []byte(configHash), 0644)
	_ = os.WriteFile(filepath.Join(dir, "config", "logger.go"), []byte(configLogger), 0644)
	_ = os.WriteFile(filepath.Join(dir, "config", "mail.go"), []byte(configMail), 0644)
	_ = os.WriteFile(filepath.Join(dir, "config", "queue.go"), []byte(configQueue), 0644)
	_ = os.WriteFile(filepath.Join(dir, "config", "storage.go"), []byte(configStorage), 0644)
	_ = os.WriteFile(filepath.Join(dir, "config", "static.go"), []byte(configStatic), 0644)
	_ = os.WriteFile(filepath.Join(dir, "config", "bodyparser.go"), []byte(configBodyParser), 0644)
	_ = os.WriteFile(filepath.Join(dir, "config", "limiter.go"), []byte(configLimiter), 0644)
	_ = os.WriteFile(filepath.Join(dir, "config", "cache.go"), []byte(configCache), 0644)
	_ = os.WriteFile(filepath.Join(dir, "config", "shield.go"), []byte(configShield), 0644)

	te := template.Must(template.New("env").Parse(envExample))
	ef, _ := os.Create(filepath.Join(dir, ".env.example"))
	_ = te.Execute(ef, map[string]string{"AppName": name})
	_ = ef.Close()
	if c.kit != "" {
		envEx, _ := os.ReadFile(filepath.Join(dir, ".env.example"))
		_ = os.WriteFile(filepath.Join(dir, ".env.example"), append(envEx, []byte("\nVITE_APP_NAME="+name+"\n")...), 0644)
	} else if !c.noDefaults {
		if defEnv := defaultEnvVars(name); defEnv != "" {
			envEx, _ := os.ReadFile(filepath.Join(dir, ".env.example"))
			_ = os.WriteFile(filepath.Join(dir, ".env.example"), append(envEx, []byte(defEnv)...), 0644)
		}
	}

	envContent := "PORT=3333\nAPP_ENV=development\nAPP_NAME=" + name + "\nDB_DRIVER=sqlite\nDB_DSN=database.sqlite\nQUEUE_DRIVER=sync\n"
	if c.kit != "" {
		envContent += "VITE_APP_NAME=" + name + "\n"
	} else if !c.noDefaults {
		envContent += "REDIS_URL=redis://localhost:6379\n"
	}
	_ = os.WriteFile(filepath.Join(dir, ".env"), []byte(envContent), 0644)
	_ = os.WriteFile(filepath.Join(dir, ".air.toml"), []byte(airConfigTmpl), 0644)

	gitignore := ".env\n*.sqlite\ntmp/\n"
	if c.kit != "" {
		gitignore += "node_modules/\npublic/build/\n"
	}
	_ = os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(gitignore), 0644)

	_ = os.WriteFile(filepath.Join(dir, "database", "migrations", "registry.go"), []byte(migrationsRegistryStub), 0644)

	// ── Scaffold auth based on selected guard ───────────────
	if authGuard != "none" {
		if err := scaffoldAuth(dir, name, authGuard); err != nil {
			ctx.UI.Warnf("failed to scaffold auth: %v", err)
		} else {
			ctx.UI.Successf("Auth guard scaffolded: %s", authGuard)
		}
	}

	if interactive {
		if err := updateEnvDB(dir, name, dbDriver); err != nil {
			ctx.UI.Warnf("failed to update DB settings in env files: %v", err)
		}

		if len(selectedPlugins) > 0 {
			ctx.UI.Infof("Installing selected plugins...")
			wd, _ := os.Getwd()
			_ = os.Chdir(dir)

			pluginCmd := &PluginInstallCommand{}
			for _, p := range selectedPlugins {
				pluginCtx := &cli.Context{
					UI:   ctx.UI,
					Args: []string{p},
				}
				_ = pluginCmd.Run(pluginCtx)
			}

			_ = os.Chdir(wd)
		}
	}

	ctx.UI.Successf("Project %s successfully created!", name)

	msg := fmt.Sprintf("Selected DB: %s", dbDriver)
	if authGuard != "none" {
		msg += fmt.Sprintf("\nAuth guard: %s", authGuard)
	}
	if len(selectedPlugins) > 0 {
		msg += fmt.Sprintf("\nPlugins selected: %s (run nimbus plugin install for each in your app directory)", strings.Join(selectedPlugins, ", "))
	}
	ctx.UI.Panel("Configuration Summary", msg)

	ctx.UI.Infof("Next steps:")
	ctx.UI.Infof("  cd %s", name)
	ctx.UI.Infof("  go mod tidy")
	if c.kit != "" {
		ctx.UI.Infof("  npm install")
	}
	ctx.UI.Infof("  nimbus serve")

	return nil
}

func goModContent(name, kit string) string {
	mod := `module ` + name + `

go 1.21

require (
	github.com/CodeSyncr/nimbus v` + version.Nimbus + `
	github.com/joho/godotenv v1.5.1
	github.com/air-verse/air v1.52.3
)

replace github.com/CodeSyncr/nimbus => ../nimbus
`
	if kit != "" {
		mod = `module ` + name + `

go 1.21

require (
	github.com/CodeSyncr/nimbus v` + version.Nimbus + `
	github.com/joho/godotenv v1.5.1
	github.com/air-verse/air v1.52.3
	github.com/petaki/inertia-go v1.6.0
)

replace github.com/CodeSyncr/nimbus => ../nimbus
`
	}
	return mod
}

func createInertiaKit(dir, name, kit string) error {
	scriptPath := "/build/assets/app.js"
	layoutContent := strings.Replace(inertiaLayoutNimbus, "{{SCRIPT_SRC}}", scriptPath, 1)
	_ = os.WriteFile(filepath.Join(dir, "resources", "views", "inertia_layout.nimbus"), []byte(layoutContent), 0644)

	switch kit {
	case "react":
		_ = os.WriteFile(filepath.Join(dir, "inertia", "app.tsx"), []byte(inertiaAppReact), 0644)
		_ = os.WriteFile(filepath.Join(dir, "inertia", "layouts", "default.tsx"), []byte(inertiaLayoutDefault), 0644)
		_ = os.WriteFile(filepath.Join(dir, "inertia", "pages", "home", "index.tsx"), []byte(inertiaPageHomeReact), 0644)
	case "vue":
		_ = os.WriteFile(filepath.Join(dir, "inertia", "app.js"), []byte(inertiaAppVue), 0644)
		_ = os.WriteFile(filepath.Join(dir, "inertia", "pages", "home", "index.vue"), []byte(inertiaPageHomeVue), 0644)
	case "svelte":
		_ = os.WriteFile(filepath.Join(dir, "inertia", "app.js"), []byte(inertiaAppSvelte), 0644)
		_ = os.WriteFile(filepath.Join(dir, "inertia", "pages", "home", "index.svelte"), []byte(inertiaPageHomeSvelte), 0644)
	}

	_ = os.WriteFile(filepath.Join(dir, "package.json"), []byte(inertiaPackageJSON(kit)), 0644)
	_ = os.WriteFile(filepath.Join(dir, "vite.config.js"), []byte(inertiaViteConfig(kit)), 0644)
	_ = os.WriteFile(filepath.Join(dir, "index.html"), []byte(inertiaIndexHTML(kit)), 0644)
	if kit == "react" {
		_ = os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(inertiaTsconfig), 0644)
		_ = os.WriteFile(filepath.Join(dir, "tsconfig.node.json"), []byte(inertiaTsconfigNode), 0644)
		_ = os.WriteFile(filepath.Join(dir, "inertia", "tsconfig.json"), []byte(inertiaTsconfigInertia), 0644)
		_ = os.WriteFile(filepath.Join(dir, "inertia", "types.ts"), []byte(inertiaTypesTS), 0644)
	}
	return nil
}

func updateEnvDB(dir, appName, driver string) error {
	update := func(path string) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		lines := strings.Split(string(data), "\n")
		host := "localhost"
		port := ""
		user := ""
		password := ""
		dbName := appName
		switch driver {
		case "postgres":
			port = "5432"
			user = "postgres"
			password = "postgres"
		case "mysql":
			port = "3306"
			user = "root"
			password = "password"
		default:
			dbName = appName
		}

		seenDriver := false
		seenHost := false
		seenPort := false
		seenUser := false
		seenPassword := false
		seenDatabase := false
		seenDSN := false

		for i, line := range lines {
			if strings.HasPrefix(line, "DB_DRIVER=") {
				lines[i] = "DB_DRIVER=" + driver
				seenDriver = true
			}
			if strings.HasPrefix(line, "DB_DSN=") {
				lines[i] = "DB_DSN="
				seenDSN = true
			}
			if strings.HasPrefix(line, "DB_HOST=") {
				lines[i] = "DB_HOST=" + host
				seenHost = true
			}
			if strings.HasPrefix(line, "DB_PORT=") {
				lines[i] = "DB_PORT=" + port
				seenPort = true
			}
			if strings.HasPrefix(line, "DB_USER=") {
				lines[i] = "DB_USER=" + user
				seenUser = true
			}
			if strings.HasPrefix(line, "DB_PASSWORD=") {
				lines[i] = "DB_PASSWORD=" + password
				seenPassword = true
			}
			if strings.HasPrefix(line, "DB_DATABASE=") {
				lines[i] = "DB_DATABASE=" + dbName
				seenDatabase = true
			}
		}
		if !seenDriver {
			lines = append(lines, "DB_DRIVER="+driver)
		}
		if !seenHost {
			lines = append(lines, "DB_HOST="+host)
		}
		if !seenPort {
			lines = append(lines, "DB_PORT="+port)
		}
		if !seenUser {
			lines = append(lines, "DB_USER="+user)
		}
		if !seenPassword {
			lines = append(lines, "DB_PASSWORD="+password)
		}
		if !seenDatabase {
			lines = append(lines, "DB_DATABASE="+dbName)
		}
		if !seenDSN {
			lines = append(lines, "DB_DSN=")
		}

		return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
	}
	if err := update(filepath.Join(dir, ".env.example")); err != nil {
		return err
	}
	if err := update(filepath.Join(dir, ".env")); err != nil {
		return err
	}
	return nil
}

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

func RunQueueWorker() {
	app := Boot()
	if err := app.Boot(); err != nil {
		fmt.Fprintf(os.Stderr, "Boot failed: %v\n", err)
		os.Exit(1)
	}
	queue.RunWorker(context.Background(), "default")
}

func RunMigrations() {
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
