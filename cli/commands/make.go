package commands

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/CodeSyncr/nimbus/cli"
	"github.com/CodeSyncr/nimbus/cli/generators"
)

func init() {
	cli.RegisterCommand(&MakeModel{})
	cli.RegisterCommand(&MakeController{})
	cli.RegisterCommand(&MakeMigration{})
	cli.RegisterCommand(&MakeMiddleware{})
	cli.RegisterCommand(&MakeJob{})
	cli.RegisterCommand(&MakeSeeder{})
	cli.RegisterCommand(&MakeValidator{})
	cli.RegisterCommand(&MakeCommand{})
	cli.RegisterCommand(&MakePlugin{})
}

type MakeModel struct{}

func (c *MakeModel) Name() string        { return "make:model" }
func (c *MakeModel) Description() string { return "Create a new GORM model class" }
func (c *MakeModel) Args() int           { return 1 }
func (c *MakeModel) Run(ctx *cli.Context) error {
	name := ctx.Args[0]
	snake := cli.ToSnake(name)
	dir := "app/models"
	_ = os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, snake+".go")

	data := generators.Data{"ModelName": name}.WithTimestamp()
	if err := generators.RenderToFile("model.tmpl", path, data); err != nil {
		ctx.UI.Errorf("Failed to create model: %v", err)
		return err
	}
	ctx.UI.Successf("Created model %s at %s", name, path)
	return nil
}

type MakeController struct{}

func (c *MakeController) Name() string        { return "make:controller" }
func (c *MakeController) Description() string { return "Create a new HTTP controller class" }
func (c *MakeController) Args() int           { return 1 }
func (c *MakeController) Run(ctx *cli.Context) error {
	name := ctx.Args[0]
	snake := cli.ToSnake(name)
	dir := "app/controllers"
	_ = os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, snake+".go")

	data := generators.Data{"ControllerName": name}.WithTimestamp()
	if err := generators.RenderToFile("controller.tmpl", path, data); err != nil {
		ctx.UI.Errorf("Failed to create controller: %v", err)
		return err
	}
	ctx.UI.Successf("Created controller %s at %s", name, path)
	return nil
}

type MakeMigration struct{}

func (c *MakeMigration) Name() string        { return "make:migration" }
func (c *MakeMigration) Description() string { return "Create a new database migration file" }
func (c *MakeMigration) Args() int           { return 1 }
func (c *MakeMigration) Run(ctx *cli.Context) error {
	name := ctx.Args[0]
	snake := cli.ToSnake(name)
	dir := "database/migrations"
	_ = os.MkdirAll(dir, 0755)

	ts := cli.Timestamp()
	filename := ts + "_" + snake + ".go"
	path := filepath.Join(dir, filename)

	pascal := cli.ToPascal(snake)
	tableName := snake
	if len(snake) > 7 && snake[:7] == "create_" {
		tableName = snake[7:]
	} else if len(snake) > 4 && snake[:4] == "add_" {
		tableName = snake[4:] + "_table"
	}

	migName := ts + "_" + snake
	data := generators.Data{
		"SchemaName":    pascal,
		"TableName":     tableName,
		"MigrationName": migName,
	}.WithTimestamp()
	if err := generators.RenderToFile("migration.tmpl", path, data); err != nil {
		ctx.UI.Errorf("Failed to create migration: %v", err)
		return err
	}
	ctx.UI.Successf("Created migration %s at %s", name, path)
	ctx.UI.Infof("Don't forget to add &%s{} to database/migrations/registry.go", pascal)
	return nil
}

type MakeMiddleware struct{}

func (c *MakeMiddleware) Name() string        { return "make:middleware" }
func (c *MakeMiddleware) Description() string { return "Create a new HTTP middleware" }
func (c *MakeMiddleware) Args() int           { return 1 }
func (c *MakeMiddleware) Run(ctx *cli.Context) error {
	name := ctx.Args[0]
	snake := cli.ToSnake(name)
	dir := "app/middleware"
	_ = os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, snake+".go")

	data := generators.Data{"MiddlewareName": name}.WithTimestamp()
	if err := generators.RenderToFile("middleware.tmpl", path, data); err != nil {
		ctx.UI.Errorf("Failed to create middleware: %v", err)
		return err
	}
	ctx.UI.Successf("Created middleware %s at %s", name, path)
	return nil
}

type MakeJob struct{}

func (c *MakeJob) Name() string        { return "make:job" }
func (c *MakeJob) Description() string { return "Create a new queue job class" }
func (c *MakeJob) Args() int           { return 1 }
func (c *MakeJob) Run(ctx *cli.Context) error {
	name := ctx.Args[0]
	snake := cli.ToSnake(name)
	dir := "app/jobs"
	_ = os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, snake+".go")

	data := generators.Data{"JobName": name}.WithTimestamp()
	if err := generators.RenderToFile("job.tmpl", path, data); err != nil {
		ctx.UI.Errorf("Failed to create job: %v", err)
		return err
	}
	ctx.UI.Successf("Created job %s at %s", name, path)
	return nil
}

type MakeSeeder struct{}

func (c *MakeSeeder) Name() string        { return "make:seeder" }
func (c *MakeSeeder) Description() string { return "Create a new database seeder" }
func (c *MakeSeeder) Args() int           { return 1 }
func (c *MakeSeeder) Run(ctx *cli.Context) error {
	name := ctx.Args[0]
	snake := cli.ToSnake(name)
	dir := "database/seeders"
	_ = os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, snake+".go")

	data := generators.Data{"SeederName": name}.WithTimestamp()
	if err := generators.RenderToFile("seeder.tmpl", path, data); err != nil {
		ctx.UI.Errorf("Failed to create seeder: %v", err)
		return err
	}
	ctx.UI.Successf("Created seeder %s at %s", name, path)
	return nil
}

type MakeValidator struct{}

func (c *MakeValidator) Name() string        { return "make:validator" }
func (c *MakeValidator) Description() string { return "Create a new validation schema" }
func (c *MakeValidator) Args() int           { return 1 }
func (c *MakeValidator) Run(ctx *cli.Context) error {
	name := ctx.Args[0]
	snake := cli.ToSnake(name)
	pascal := cli.ToPascal(snake)
	dir := "app/validators"
	_ = os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, snake+".go")

	data := generators.Data{"ValidatorName": pascal}.WithTimestamp()
	if err := generators.RenderToFile("validator.tmpl", path, data); err != nil {
		ctx.UI.Errorf("Failed to create validator: %v", err)
		return err
	}
	ctx.UI.Successf("Created validator %s at %s", pascal, path)
	return nil
}

type MakeCommand struct{}

func (c *MakeCommand) Name() string        { return "make:command" }
func (c *MakeCommand) Description() string { return "Create a new CLI command" }
func (c *MakeCommand) Args() int           { return 1 }
func (c *MakeCommand) Run(ctx *cli.Context) error {
	name := ctx.Args[0]
	fileSnake := cli.ToSnake(strings.ReplaceAll(name, ":", "_"))
	pascal := cli.ToPascal(fileSnake)
	dir := "commands"
	_ = os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, fileSnake+".go")

	data := generators.Data{"CommandName": pascal, "FullName": name}.WithTimestamp()
	if err := generators.RenderToFile("command.tmpl", path, data); err != nil {
		ctx.UI.Errorf("Failed to create command: %v", err)
		return err
	}
	ctx.UI.Successf("Created command %s at %s", name, path)
	ctx.UI.Infof("Ensure your command is registered in main.go!")
	return nil
}

type MakePlugin struct{}

func (c *MakePlugin) Name() string        { return "make:plugin" }
func (c *MakePlugin) Description() string { return "Create a new plugin skeleton" }
func (c *MakePlugin) Args() int           { return 1 }
func (c *MakePlugin) Run(ctx *cli.Context) error {
	name := ctx.Args[0]
	snake := cli.ToSnake(name)
	pascal := cli.ToPascal(snake)
	upper := strings.ToUpper(snake)
	dir := filepath.Join("app", "plugins", snake)
	_ = os.MkdirAll(dir, 0755)

	data := generators.Data{
		"PluginName": snake,
		"PascalName": pascal,
		"UpperName":  upper,
	}.WithTimestamp()

	files := []struct {
		tmpl string
		dest string
	}{
		{"plugin_main.tmpl", "plugin.go"},
		{"plugin_config.tmpl", "config.go"},
		{"plugin_service.tmpl", "service.go"},
		{"plugin_routes.tmpl", "routes.go"},
		{"plugin_handlers.tmpl", "handlers.go"},
		{"plugin_middleware.tmpl", "middleware.go"},
		{"plugin_events.tmpl", "events.go"},
		{"plugin_commands.tmpl", "commands.go"},
		{"plugin_readme.tmpl", "README.md"},
	}

	for _, f := range files {
		path := filepath.Join(dir, f.dest)
		if err := generators.RenderToFile(f.tmpl, path, data); err != nil {
			ctx.UI.Errorf("Failed to write %s: %v", f.dest, err)
			return err
		}
	}

	ctx.UI.Successf("Created plugin %s at %s/", pascal, dir)
	ctx.UI.Infof("")
	ctx.UI.Infof("  %s/", dir)
	ctx.UI.Infof("  ├── plugin.go       Core plugin, Register/Boot")
	ctx.UI.Infof("  ├── config.go       Configuration & defaults")
	ctx.UI.Infof("  ├── service.go      Business logic & SDK wrapper")
	ctx.UI.Infof("  ├── routes.go       HTTP route registration")
	ctx.UI.Infof("  ├── handlers.go     HTTP handlers")
	ctx.UI.Infof("  ├── middleware.go   Named middleware")
	ctx.UI.Infof("  ├── events.go       Event listeners")
	ctx.UI.Infof("  ├── commands.go     CLI commands")
	ctx.UI.Infof("  └── README.md       Documentation")
	ctx.UI.Infof("")
	ctx.UI.Infof("Register it in bin/server.go:")
	ctx.UI.Infof("  app.Use(%s.New())", snake)
	return nil
}
