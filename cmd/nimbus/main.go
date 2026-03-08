// Package main is the Nimbus CLI (Cobra-based, AdonisJS Ace-style).
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "nimbus",
	Short: "Nimbus - Laravel-style framework for Go",
}

var newCmd = &cobra.Command{
	Use:   "new [app-name]",
	Short: "Create a new Nimbus application",
	Args:  cobra.ExactArgs(1),
	RunE:  runNew,
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the application (AdonisJS ace serve style; run from app root)",
	RunE:  runServe,
}

var makeModelCmd = &cobra.Command{
	Use:   "make:model [name]",
	Short: "Create a new model",
	Args:  cobra.ExactArgs(1),
	RunE:  runMakeModel,
}

var makeMigrationCmd = &cobra.Command{
	Use:   "make:migration [name]",
	Short: "Create a new migration",
	Args:  cobra.ExactArgs(1),
	RunE:  runMakeMigration,
}

var makeControllerCmd = &cobra.Command{
	Use:   "make:controller [name]",
	Short: "Create a new controller",
	Args:  cobra.ExactArgs(1),
	RunE:  runMakeController,
}

var makeMiddlewareCmd = &cobra.Command{
	Use:   "make:middleware [name]",
	Short: "Create a new middleware",
	Args:  cobra.ExactArgs(1),
	RunE:  runMakeMiddleware,
}

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Run database migrations",
	RunE:  runMigrate,
}

var migrateRollbackCmd = &cobra.Command{
	Use:   "migrate:rollback",
	Short: "Rollback the last migration",
	RunE:  runMigrateRollback,
}

var makeJobCmd = &cobra.Command{
	Use:   "make:job [name]",
	Short: "Create a new queue job",
	Args:  cobra.ExactArgs(1),
	RunE:  runMakeJob,
}

var makeSeederCmd = &cobra.Command{
	Use:   "make:seeder [name]",
	Short: "Create a new database seeder",
	Args:  cobra.ExactArgs(1),
	RunE:  runMakeSeeder,
}

func main() {
	rootCmd.AddCommand(newCmd, serveCmd, makeModelCmd, makeMigrationCmd, makeControllerCmd, makeMiddlewareCmd, makeJobCmd, makeSeederCmd, migrateCmd, migrateRollbackCmd)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runNew(cmd *cobra.Command, args []string) error {
	name := args[0]
	dir := name
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	dirs := []string{
		filepath.Join(dir, "app", "controllers"),
		filepath.Join(dir, "app", "models"),
		filepath.Join(dir, "app", "middleware"),
		filepath.Join(dir, "app", "jobs"),
		filepath.Join(dir, "config"),
		filepath.Join(dir, "database", "migrations"),
		filepath.Join(dir, "database", "seeders"),
		filepath.Join(dir, "start"),
		filepath.Join(dir, "views"),
		filepath.Join(dir, "public"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return err
		}
	}
	mod := `module ` + name + `

go 1.21

require github.com/CodeSyncr/nimbus v0.0.0

replace github.com/CodeSyncr/nimbus => ../
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0644); err != nil {
		return err
	}
	t := template.Must(template.New("main").Parse(mainTmpl))
	f, _ := os.Create(filepath.Join(dir, "main.go"))
	_ = t.Execute(f, map[string]string{"AppName": name})
	_ = f.Close()
	_ = os.WriteFile(filepath.Join(dir, "start", "routes.go"), []byte(routesStub), 0644)
	_ = os.WriteFile(filepath.Join(dir, ".env.example"), []byte(envExample), 0644)
	_ = os.WriteFile(filepath.Join(dir, "config", "app.go"), []byte(configApp), 0644)
	_ = os.WriteFile(filepath.Join(dir, ".air.toml"), []byte(airConfigTmpl), 0644)
	_ = os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".env\n*.sqlite\ntmp/\n"), 0644)
	fmt.Printf("Created Nimbus app %q in ./%s\n", name, dir)
	fmt.Println("Next: cd " + dir + " && go mod tidy && go run main.go")
	return nil
}

const mainTmpl = `package main

import (
	"net/http"

	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/context"
	"github.com/CodeSyncr/nimbus/database"
	"github.com/CodeSyncr/nimbus/middleware"
)

func main() {
	app := nimbus.New()
	app.Router.Use(middleware.Logger(), middleware.Recover())

	app.Router.Get("/", homeHandler)
	app.Router.Get("/health", healthHandler)

	_, _ = database.Connect(app.Config.Database.Driver, app.Config.Database.DSN)

	_ = app.Run()
}

func homeHandler(c *context.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"message": "Welcome to Nimbus"})
}

func healthHandler(c *context.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}
`

const envExample = `PORT=3333
APP_ENV=development
APP_NAME=nimbus
DB_DRIVER=sqlite
DB_DSN=database.sqlite
`

const routesStub = `package start

// RegisterRoutes can be called from main to register all routes (AdonisJS start/routes).
// func RegisterRoutes(app *nimbus.App) { ... }
`

const configApp = `package config

// App-specific config can live here; base config is in nimbus/config.
`

const airConfigTmpl = `# Nimbus hot reload (air)
root = "."
tmp_dir = "tmp"

[build]
  cmd = "go build -o ./tmp/main ."
  bin = "./tmp/main"
  delay = 1000
  exclude_dir = ["tmp", "vendor"]
  exclude_regex = ["_test.go"]
  include_ext = ["go", "nimbus"]
  send_interrupt = true

[log]
  time = false
`

func runServe(cmd *cobra.Command, args []string) error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	if !isNimbusApp(dir) {
		fmt.Println("Not a Nimbus app. Run 'nimbus serve' from your app root (where go.mod and main.go are).")
		fmt.Println("Create an app with: nimbus new myapp")
		return nil
	}
	// Hot reload: run air via go run (no separate install; go fetches it when needed)
	ensureAirConfig(dir)
	fmt.Println("Starting Nimbus app (hot reload)...")
	c := exec.Command("go", "run", "github.com/air-verse/air@v1.52.3")
	c.Dir = dir
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

// ensureAirConfig writes a default .air.toml if missing (watches .go and .nimbus).
func ensureAirConfig(dir string) {
	path := filepath.Join(dir, ".air.toml")
	if _, err := os.Stat(path); err == nil {
		return
	}
	const config = `# Nimbus hot reload (air)
root = "."
tmp_dir = "tmp"

[build]
  cmd = "go build -o ./tmp/main ."
  bin = "./tmp/main"
  delay = 1000
  exclude_dir = ["tmp", "vendor"]
  exclude_regex = ["_test.go"]
  include_ext = ["go", "nimbus"]
  send_interrupt = true

[log]
  time = false
`
	_ = os.WriteFile(path, []byte(config), 0644)
}

// isNimbusApp reports whether dir contains a Nimbus app (go.mod requires nimbus + main.go).
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

func runMakeModel(cmd *cobra.Command, args []string) error {
	name := args[0]
	snake := toSnake(name)
	dir := "app/models"
	_ = os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, snake+".go")
	content := fmt.Sprintf(modelTmpl, name, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return err
	}
	fmt.Printf("Created model %s at %s\n", name, path)
	return nil
}

const modelTmpl = `package models

import "github.com/CodeSyncr/nimbus/database"

// %s embeds the base model (ID, timestamps).
type %s struct {
	database.Model
	// Add fields here
}
`

func runMakeMigration(cmd *cobra.Command, args []string) error {
	name := args[0]
	dir := "database/migrations"
	_ = os.MkdirAll(dir, 0755)
	ts := time.Now().Format("20060102150405")
	filename := ts + "_" + toSnake(name) + ".go"
	path := filepath.Join(dir, filename)
	pascal := toPascal(toSnake(name))
	content := fmt.Sprintf(migrationTmpl, name, pascal, pascal, pascal, pascal, pascal)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return err
	}
	fmt.Printf("Created migration %s at %s\n", name, path)
	return nil
}

const migrationTmpl = `package migrations

import "gorm.io/gorm"

// %s migration.
// Register: database.Migration{Name: "%s", Up: %sUp, Down: %sDown}
func %sUp(db *gorm.DB) error {
	// db.Exec("CREATE TABLE ...")
	return nil
}

func %sDown(db *gorm.DB) error {
	// db.Exec("DROP TABLE ...")
	return nil
}
`

func runMakeController(cmd *cobra.Command, args []string) error {
	name := args[0]
	snake := toSnake(name)
	dir := "app/controllers"
	_ = os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, snake+".go")
	content := fmt.Sprintf(controllerTmpl, name, name, name, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return err
	}
	fmt.Printf("Created controller %s at %s\n", name, path)
	return nil
}

const controllerTmpl = `package controllers

import (
	"net/http"

	"github.com/CodeSyncr/nimbus/context"
)

// %s controller.
type %s struct{}

// Index lists resources.
func (c *%s) Index(ctx *context.Context) error {
	return ctx.JSON(http.StatusOK, map[string]string{"message": "index"})
}

// Show returns one resource.
func (c *%s) Show(ctx *context.Context) error {
	id := ctx.Param("id")
	return ctx.JSON(http.StatusOK, map[string]string{"id": id})
}
`

func runMakeMiddleware(cmd *cobra.Command, args []string) error {
	name := args[0]
	snake := toSnake(name)
	dir := "app/middleware"
	_ = os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, snake+".go")
	content := fmt.Sprintf(middlewareTmpl, name, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return err
	}
	fmt.Printf("Created middleware %s at %s\n", name, path)
	return nil
}

const middlewareTmpl = `package middleware

import (
	"github.com/CodeSyncr/nimbus/context"
	"github.com/CodeSyncr/nimbus/router"
)

// %s returns a middleware that runs before the handler.
func %s() router.Middleware {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *context.Context) error {
			// Before handler
			err := next(c)
			// After handler
			return err
		}
	}
}
`

func runMakeJob(cmd *cobra.Command, args []string) error {
	name := args[0]
	snake := toSnake(name)
	dir := "app/jobs"
	_ = os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, snake+".go")
	content := fmt.Sprintf(jobTmpl, name, name, name, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return err
	}
	fmt.Printf("Created job %s at %s\n", name, path)
	return nil
}

const jobTmpl = `package jobs

import "context"

// %s implements queue.Job. Push to queue with queue.Push(%s{}).
type %s struct {
	// Add payload fields
}

func (j *%s) Handle(ctx context.Context) error {
	// Do work
	return nil
}
`

func runMakeSeeder(cmd *cobra.Command, args []string) error {
	name := args[0]
	snake := toSnake(name)
	dir := "database/seeders"
	_ = os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, snake+".go")
	content := fmt.Sprintf(seederTmpl, name, name, name, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return err
	}
	fmt.Printf("Created seeder %s at %s\n", name, path)
	return nil
}

const seederTmpl = `package seeders

import "gorm.io/gorm"

// %s seeds data. Add to database.NewSeedRunner(db, []database.Seeder{&%s{}}).Run()
type %s struct{}

func (s *%s) Run(db *gorm.DB) error {
	// db.Create(&Model{...})
	return nil
}
`

func runMigrate(cmd *cobra.Command, args []string) error {
	fmt.Println("Run migrations from your app: add migrations to a slice and call database.NewMigrator(db, migrations).Up()")
	fmt.Println("Or create cmd/migrate/main.go that connects to DB and runs your migration list.")
	return nil
}

func runMigrateRollback(cmd *cobra.Command, args []string) error {
	fmt.Println("Rollback: call database.NewMigrator(db, migrations).Down() from your app.")
	return nil
}

func toSnake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		if r >= 'A' && r <= 'Z' {
			r = r + ('a' - 'A')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func toPascal(snake string) string {
	var b strings.Builder
	up := true
	for _, r := range snake {
		if r == '_' {
			up = true
			continue
		}
		if up && r >= 'a' && r <= 'z' {
			r = r - ('a' - 'A')
			up = false
		}
		b.WriteRune(r)
	}
	return b.String()
}
