package commands

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/CodeSyncr/nimbus/cli"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

func init() {
	cli.RegisterCommand(&AICommand{})
}

// AICommand is the `nimbus ai` CLI copilot.
type AICommand struct {
	model string
	dry   bool
}

func (c *AICommand) Name() string        { return "ai" }
func (c *AICommand) Description() string { return "AI copilot — generate code from natural language" }
func (c *AICommand) Args() int           { return -1 }

func (c *AICommand) Flags(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&c.model, "model", "m", "", "AI model to use (default: reads from NIMBUS_AI_MODEL or uses gpt-4o)")
	cmd.Flags().BoolVar(&c.dry, "dry-run", false, "Show generated code without writing files")
}

func (c *AICommand) Run(ctx *cli.Context) error {
	prompt := strings.Join(ctx.Args, " ")
	if prompt == "" {
		return c.interactiveMode(ctx)
	}
	return c.executePrompt(ctx, prompt)
}

// ---------------------------------------------------------------------------
// Interactive mode — REPL
// ---------------------------------------------------------------------------

func (c *AICommand) interactiveMode(ctx *cli.Context) error {
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#818cf8")).
		Bold(true)
	dimStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#64748b"))

	fmt.Fprintln(ctx.Stdout, headerStyle.Render("⚡ Nimbus AI Copilot"))
	fmt.Fprintln(ctx.Stdout, dimStyle.Render("Type a request and press Enter. Type 'exit' to quit."))
	fmt.Fprintln(ctx.Stdout, dimStyle.Render("Examples: 'create a blog with posts and comments'"))
	fmt.Fprintln(ctx.Stdout, dimStyle.Render("          'add auth middleware to the API routes'"))
	fmt.Fprintln(ctx.Stdout)

	scanner := bufio.NewScanner(ctx.Stdin)
	promptStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6366f1")).
		Bold(true)

	for {
		fmt.Fprint(ctx.Stdout, promptStyle.Render("nimbus> "))
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		if input == "exit" || input == "quit" || input == "q" {
			fmt.Fprintln(ctx.Stdout, dimStyle.Render("Goodbye!"))
			return nil
		}
		if err := c.executePrompt(ctx, input); err != nil {
			ctx.UI.Errorf("%v", err)
		}
		fmt.Fprintln(ctx.Stdout)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Prompt execution — code generation engine
// ---------------------------------------------------------------------------

func (c *AICommand) executePrompt(ctx *cli.Context, prompt string) error {
	// Parse the intent from the natural language prompt.
	intent := parseIntent(prompt)

	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e"))
	fileStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#818cf8"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#64748b"))

	fmt.Fprintln(ctx.Stdout, dimStyle.Render(fmt.Sprintf("  Generating %s: %s", intent.Type, intent.Name)))

	// Execute generation based on intent.
	files, err := generateFromIntent(ctx.AppRoot, intent)
	if err != nil {
		return fmt.Errorf("generation failed: %w", err)
	}

	if len(files) == 0 {
		ctx.UI.Infof("No files to generate for: %s", prompt)
		return nil
	}

	if c.dry {
		fmt.Fprintln(ctx.Stdout, dimStyle.Render("\n  [DRY RUN] Would create:"))
		for _, f := range files {
			fmt.Fprintln(ctx.Stdout, fileStyle.Render("    → "+f.Path))
		}
		return nil
	}

	// Write files.
	for _, f := range files {
		fullPath := filepath.Join(ctx.AppRoot, f.Path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(fullPath, []byte(f.Content), 0644); err != nil {
			return err
		}
		fmt.Fprintln(ctx.Stdout, successStyle.Render("  ✓ ")+fileStyle.Render(f.Path))
	}

	// Post-generation hints.
	if len(intent.Hints) > 0 {
		fmt.Fprintln(ctx.Stdout)
		for _, hint := range intent.Hints {
			fmt.Fprintln(ctx.Stdout, dimStyle.Render("  💡 "+hint))
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// Intent Parser
// ---------------------------------------------------------------------------

// Intent represents a parsed user request.
type Intent struct {
	Type   string   // "resource", "model", "controller", "migration", "middleware", "crud", "scaffold", "auth", "api", "job", "mailer", "channel"
	Name   string   // primary entity name
	Fields []Field  // model fields
	Hints  []string // post-generation hints
	Flags  map[string]bool
}

// Field represents a model field.
type Field struct {
	Name     string
	Type     string
	Tags     string
	Nullable bool
	Unique   bool
}

// GeneratedFile is a file to be written.
type GeneratedFile struct {
	Path    string
	Content string
}

// parseIntent inspects the prompt and determines what to generate.
func parseIntent(prompt string) Intent {
	lower := strings.ToLower(prompt)
	intent := Intent{
		Flags: make(map[string]bool),
	}

	// --- CRUD / Resource / Scaffold ---
	if matchesAny(lower, "create a", "build a", "scaffold", "generate", "make a", "add a") {
		entityName := extractEntityName(prompt)
		fields := extractFields(prompt)

		if matchesAny(lower, "crud", "resource", "scaffold", "blog", "app", "with") {
			intent.Type = "scaffold"
			intent.Name = entityName
			intent.Fields = fields
			intent.Hints = []string{
				"Run migrations: nimbus migrate",
				"Register routes in start/routes.go",
				"Start the server: nimbus serve",
			}
			return intent
		}

		if matchesAny(lower, "model", "entity", "struct") {
			intent.Type = "model"
			intent.Name = entityName
			intent.Fields = fields
			intent.Hints = []string{"Run migrations: nimbus migrate"}
			return intent
		}

		if matchesAny(lower, "controller", "handler") {
			intent.Type = "controller"
			intent.Name = entityName
			return intent
		}

		if matchesAny(lower, "migration") {
			intent.Type = "migration"
			intent.Name = entityName
			intent.Fields = fields
			return intent
		}

		if matchesAny(lower, "middleware") {
			intent.Type = "middleware"
			intent.Name = entityName
			return intent
		}

		if matchesAny(lower, "job", "queue", "background") {
			intent.Type = "job"
			intent.Name = entityName
			return intent
		}

		if matchesAny(lower, "mailer", "email", "mail") {
			intent.Type = "mailer"
			intent.Name = entityName
			return intent
		}

		if matchesAny(lower, "websocket", "channel", "realtime") {
			intent.Type = "channel"
			intent.Name = entityName
			return intent
		}

		if matchesAny(lower, "api", "rest", "endpoint") {
			intent.Type = "api"
			intent.Name = entityName
			intent.Fields = fields
			intent.Hints = []string{
				"Register routes in start/routes.go",
				"Run migrations: nimbus migrate",
			}
			return intent
		}

		// Default: scaffold for named entities.
		intent.Type = "scaffold"
		intent.Name = entityName
		intent.Fields = fields
		intent.Hints = []string{
			"Run migrations: nimbus migrate",
			"Register routes in start/routes.go",
		}
		return intent
	}

	// --- Auth ---
	if matchesAny(lower, "auth", "login", "signup", "register", "authentication") {
		intent.Type = "auth"
		intent.Name = "auth"
		intent.Hints = []string{
			"Configure auth in config/auth.go",
			"Register auth routes in start/routes.go",
		}
		return intent
	}

	// --- Middleware ---
	if matchesAny(lower, "middleware", "guard", "protect") {
		intent.Type = "middleware"
		intent.Name = extractEntityName(prompt)
		return intent
	}

	// Default: scaffold.
	intent.Type = "scaffold"
	intent.Name = extractEntityName(prompt)
	intent.Fields = extractFields(prompt)
	intent.Hints = []string{
		"Run migrations: nimbus migrate",
		"Register routes in start/routes.go",
	}
	return intent
}

// ---------------------------------------------------------------------------
// Code Generation
// ---------------------------------------------------------------------------

func generateFromIntent(appRoot string, intent Intent) ([]GeneratedFile, error) {
	switch intent.Type {
	case "scaffold":
		return generateScaffold(appRoot, intent)
	case "model":
		return generateModel(intent)
	case "controller":
		return generateController(intent)
	case "migration":
		return generateMigration(intent)
	case "middleware":
		return generateMiddleware(intent)
	case "job":
		return generateJob(intent)
	case "mailer":
		return generateMailer(intent)
	case "api":
		return generateAPI(intent)
	case "auth":
		return generateAuth(intent)
	case "channel":
		return generateChannel(intent)
	default:
		return generateScaffold(appRoot, intent)
	}
}

// generateScaffold creates model + controller + migration + views for a resource.
func generateScaffold(appRoot string, intent Intent) ([]GeneratedFile, error) {
	var files []GeneratedFile

	name := sanitizeName(intent.Name)
	if name == "" {
		name = "item"
	}
	pascal := toPascalCase(name)
	snake := toSnakeCase(name)
	plural := pluralize(snake)

	fields := intent.Fields
	if len(fields) == 0 {
		fields = defaultFieldsForEntity(name)
	}

	// 1. Model
	modelFiles, _ := generateModelCode(pascal, snake, fields)
	files = append(files, modelFiles...)

	// 2. Controller
	ctrlFiles, _ := generateControllerCode(pascal, snake, plural, fields)
	files = append(files, ctrlFiles...)

	// 3. Migration
	migFiles, _ := generateMigrationCode(pascal, snake, plural, fields)
	files = append(files, migFiles...)

	// 4. Routes snippet
	routeSnippet := generateRouteSnippet(pascal, snake, plural)
	files = append(files, routeSnippet)

	return files, nil
}

func generateModel(intent Intent) ([]GeneratedFile, error) {
	name := sanitizeName(intent.Name)
	pascal := toPascalCase(name)
	snake := toSnakeCase(name)
	fields := intent.Fields
	if len(fields) == 0 {
		fields = defaultFieldsForEntity(name)
	}
	return generateModelCode(pascal, snake, fields)
}

func generateController(intent Intent) ([]GeneratedFile, error) {
	name := sanitizeName(intent.Name)
	pascal := toPascalCase(name)
	snake := toSnakeCase(name)
	plural := pluralize(snake)
	return generateControllerCode(pascal, snake, plural, nil)
}

func generateMigration(intent Intent) ([]GeneratedFile, error) {
	name := sanitizeName(intent.Name)
	pascal := toPascalCase(name)
	snake := toSnakeCase(name)
	plural := pluralize(snake)
	fields := intent.Fields
	if len(fields) == 0 {
		fields = defaultFieldsForEntity(name)
	}
	return generateMigrationCode(pascal, snake, plural, fields)
}

func generateMiddleware(intent Intent) ([]GeneratedFile, error) {
	name := sanitizeName(intent.Name)
	pascal := toPascalCase(name)
	snake := toSnakeCase(name)

	code := fmt.Sprintf(`package middleware

import (
	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

// %s is a custom middleware.
func %s() router.Middleware {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *http.Context) error {
			// TODO: Add your %s logic here.
			//
			// Examples:
			//   - Check a header: c.Request.Header.Get("X-Custom")
			//   - Read from context: c.Get("user")
			//   - Short-circuit: return c.JSON(403, map[string]string{"error": "forbidden"})

			return next(c)
		}
	}
}
`, pascal, pascal, snake)

	return []GeneratedFile{
		{Path: fmt.Sprintf("app/middleware/%s.go", snake), Content: code},
	}, nil
}

func generateJob(intent Intent) ([]GeneratedFile, error) {
	name := sanitizeName(intent.Name)
	pascal := toPascalCase(name)
	snake := toSnakeCase(name)

	code := fmt.Sprintf(`package jobs

import (
	"context"
	"log"

	"github.com/CodeSyncr/nimbus/queue"
)

// %s is a background job.
type %s struct{}

func (j *%s) Name() string { return "%s" }

func (j *%s) Handle(ctx context.Context, payload queue.Payload) error {
	log.Printf("[%s] Processing job with payload: %%v", payload)

	// TODO: Implement your job logic here.
	//
	// Examples:
	//   - Send an email
	//   - Process a file upload
	//   - Sync data with an external API
	//   - Generate a report

	return nil
}

// Dispatch%s enqueues the job.
func Dispatch%s(q *queue.Manager, data map[string]any) error {
	return q.Dispatch("%s", data)
}
`, pascal, pascal, pascal, snake, pascal, pascal, pascal, pascal, snake)

	return []GeneratedFile{
		{Path: fmt.Sprintf("app/jobs/%s.go", snake), Content: code},
	}, nil
}

func generateMailer(intent Intent) ([]GeneratedFile, error) {
	name := sanitizeName(intent.Name)
	pascal := toPascalCase(name)
	snake := toSnakeCase(name)

	code := fmt.Sprintf(`package mail

import (
	"github.com/CodeSyncr/nimbus/mail"
)

// %sMail sends a %s email.
func %sMail(to string, data map[string]any) *mail.Message {
	return &mail.Message{
		To:       []string{to},
		Subject:  "%s Notification",
		Template: "%s",
		Data:     data,
	}
}
`, pascal, name, pascal, pascal, snake)

	return []GeneratedFile{
		{Path: fmt.Sprintf("app/mail/%s.go", snake), Content: code},
	}, nil
}

func generateAPI(intent Intent) ([]GeneratedFile, error) {
	// API generates model + JSON controller (no views).
	var files []GeneratedFile
	name := sanitizeName(intent.Name)
	pascal := toPascalCase(name)
	snake := toSnakeCase(name)
	plural := pluralize(snake)

	fields := intent.Fields
	if len(fields) == 0 {
		fields = defaultFieldsForEntity(name)
	}

	modelFiles, _ := generateModelCode(pascal, snake, fields)
	files = append(files, modelFiles...)

	ctrlFiles, _ := generateControllerCode(pascal, snake, plural, fields)
	files = append(files, ctrlFiles...)

	migFiles, _ := generateMigrationCode(pascal, snake, plural, fields)
	files = append(files, migFiles...)

	routeSnippet := generateRouteSnippet(pascal, snake, plural)
	files = append(files, routeSnippet)

	return files, nil
}

func generateAuth(intent Intent) ([]GeneratedFile, error) {
	code := `package controllers

import (
	"github.com/CodeSyncr/nimbus/auth"
	"github.com/CodeSyncr/nimbus/http"
)

// AuthController handles authentication.
type AuthController struct{}

// Login authenticates a user and returns a token.
func (ac *AuthController) Login(c *http.Context) error {
	var body struct {
		Email    string ` + "`json:\"email\" validate:\"required,email\"`" + `
		Password string ` + "`json:\"password\" validate:\"required\"`" + `
	}
	if err := c.BodyParser(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
	}

	guard := auth.Guard("token")
	if guard == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Auth not configured"})
	}

	user, err := guard.Attempt(c.Request.Context(), map[string]any{
		"email":    body.Email,
		"password": body.Password,
	})
	if err != nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Invalid credentials"})
	}

	token, err := guard.Login(c.Request.Context(), user)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to create session"})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"token": token,
		"user":  user,
	})
}

// Register creates a new user account.
func (ac *AuthController) Register(c *http.Context) error {
	var body struct {
		Name     string ` + "`json:\"name\" validate:\"required\"`" + `
		Email    string ` + "`json:\"email\" validate:\"required,email\"`" + `
		Password string ` + "`json:\"password\" validate:\"required,min=8\"`" + `
	}
	if err := c.BodyParser(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
	}

	// TODO: Create user in database.
	// user := models.User{Name: body.Name, Email: body.Email}
	// user.Password, _ = hash.Make(body.Password)
	// db.Create(&user)

	return c.JSON(http.StatusCreated, map[string]string{
		"message": "Account created successfully",
	})
}

// Logout invalidates the current session/token.
func (ac *AuthController) Logout(c *http.Context) error {
	guard := auth.Guard("token")
	if guard == nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Auth not configured"})
	}

	if err := guard.Logout(c.Request.Context()); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Logout failed"})
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "Logged out"})
}

// Me returns the current authenticated user.
func (ac *AuthController) Me(c *http.Context) error {
	user := c.Get("user")
	if user == nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
	}
	return c.JSON(http.StatusOK, user)
}
`

	routeCode := `// Auth routes — add to start/routes.go:
//
//   authCtrl := &controllers.AuthController{}
//   app.Router.Post("/auth/login", authCtrl.Login)
//   app.Router.Post("/auth/register", authCtrl.Register)
//   app.Router.Post("/auth/logout", authCtrl.Logout).As("auth.logout")
//   app.Router.Get("/auth/me", authCtrl.Me).As("auth.me")
`

	return []GeneratedFile{
		{Path: "app/controllers/auth.go", Content: code},
		{Path: "app/controllers/auth_routes.txt", Content: routeCode},
	}, nil
}

func generateChannel(intent Intent) ([]GeneratedFile, error) {
	name := sanitizeName(intent.Name)
	pascal := toPascalCase(name)
	snake := toSnakeCase(name)

	code := fmt.Sprintf(`package channels

import (
	"log"

	"github.com/CodeSyncr/nimbus/websocket"
)

// %sChannel handles realtime events for %s.
type %sChannel struct{}

func (ch *%sChannel) Name() string { return "%s" }

func (ch *%sChannel) OnJoin(client *websocket.Client, data map[string]any) {
	log.Printf("[%s] Client %%s joined", client.ID)
}

func (ch *%sChannel) OnLeave(client *websocket.Client) {
	log.Printf("[%s] Client %%s left", client.ID)
}

func (ch *%sChannel) OnMessage(client *websocket.Client, event string, data map[string]any) {
	log.Printf("[%s] Event %%s from %%s: %%v", event, client.ID, data)

	// TODO: Handle channel-specific events.
	switch event {
	case "message":
		// Broadcast to all clients in channel.
		// client.Channel.Broadcast(event, data)
	default:
		log.Printf("[%s] Unknown event: %%s", event)
	}
}
`, pascal, name, pascal, pascal, snake, pascal, pascal, pascal, pascal, pascal, pascal, pascal)

	return []GeneratedFile{
		{Path: fmt.Sprintf("app/channels/%s.go", snake), Content: code},
	}, nil
}

// ---------------------------------------------------------------------------
// Code Generation Helpers
// ---------------------------------------------------------------------------

func generateModelCode(pascal, snake string, fields []Field) ([]GeneratedFile, error) {
	var b strings.Builder
	b.WriteString("package models\n\n")
	b.WriteString("import (\n\t\"time\"\n\n\t\"gorm.io/gorm\"\n)\n\n")
	b.WriteString(fmt.Sprintf("// %s represents a %s in the database.\n", pascal, snake))
	b.WriteString(fmt.Sprintf("type %s struct {\n", pascal))
	b.WriteString("\tID        uint           `json:\"id\" gorm:\"primaryKey\"`\n")

	for _, f := range fields {
		goType := fieldTypeToGo(f.Type)
		jsonTag := toSnakeCase(f.Name)
		gormTag := ""
		if f.Unique {
			gormTag = " gorm:\"uniqueIndex\""
		}
		if f.Nullable {
			goType = "*" + goType
		}
		tags := fmt.Sprintf("`json:\"%s\"%s`", jsonTag, gormTag)
		b.WriteString(fmt.Sprintf("\t%s %s %s\n", toPascalCase(f.Name), goType, tags))
	}

	b.WriteString("\tCreatedAt time.Time      `json:\"created_at\"`\n")
	b.WriteString("\tUpdatedAt time.Time      `json:\"updated_at\"`\n")
	b.WriteString("\tDeletedAt gorm.DeletedAt  `json:\"-\" gorm:\"index\"`\n")
	b.WriteString("}\n\n")
	b.WriteString(fmt.Sprintf("// TableName returns the database table name.\n"))
	b.WriteString(fmt.Sprintf("func (%s) TableName() string { return \"%s\" }\n", pascal, pluralize(snake)))

	return []GeneratedFile{
		{Path: fmt.Sprintf("app/models/%s.go", snake), Content: b.String()},
	}, nil
}

func generateControllerCode(pascal, snake, plural string, fields []Field) ([]GeneratedFile, error) {
	var b strings.Builder
	b.WriteString("package controllers\n\n")
	b.WriteString("import (\n")
	b.WriteString("\t\"strconv\"\n\n")
	b.WriteString("\t\"github.com/CodeSyncr/nimbus/http\"\n")
	b.WriteString("\t\"gorm.io/gorm\"\n")
	b.WriteString(")\n\n")

	// Generate request/response types if fields are available.
	if len(fields) > 0 {
		b.WriteString(fmt.Sprintf("// Create%sRequest is the request body for creating a %s.\n", pascal, snake))
		b.WriteString(fmt.Sprintf("type Create%sRequest struct {\n", pascal))
		for _, f := range fields {
			goType := fieldTypeToGo(f.Type)
			jsonTag := toSnakeCase(f.Name)
			validate := ""
			if !f.Nullable {
				validate = ` validate:"required"`
			}
			b.WriteString(fmt.Sprintf("\t%s %s `json:\"%s\"%s`\n", toPascalCase(f.Name), goType, jsonTag, validate))
		}
		b.WriteString("}\n\n")

		b.WriteString(fmt.Sprintf("// Update%sRequest is the request body for updating a %s.\n", pascal, snake))
		b.WriteString(fmt.Sprintf("type Update%sRequest struct {\n", pascal))
		for _, f := range fields {
			goType := "*" + fieldTypeToGo(f.Type)
			jsonTag := toSnakeCase(f.Name)
			b.WriteString(fmt.Sprintf("\t%s %s `json:\"%s,omitempty\"`\n", toPascalCase(f.Name), goType, jsonTag))
		}
		b.WriteString("}\n\n")
	}

	// Controller struct.
	b.WriteString(fmt.Sprintf("// %sController handles %s CRUD operations.\n", pascal, snake))
	b.WriteString(fmt.Sprintf("type %sController struct {\n\tDB *gorm.DB\n}\n\n", pascal))

	// Index.
	b.WriteString(fmt.Sprintf("// Index lists all %s.\n", plural))
	b.WriteString(fmt.Sprintf("func (ctrl *%sController) Index(c *http.Context) error {\n", pascal))
	b.WriteString(fmt.Sprintf("\tvar items []map[string]any\n"))
	b.WriteString(fmt.Sprintf("\tif err := ctrl.DB.Table(\"%s\").Find(&items).Error; err != nil {\n", plural))
	b.WriteString(fmt.Sprintf("\t\treturn c.JSON(http.StatusInternalServerError, map[string]string{\"error\": err.Error()})\n"))
	b.WriteString("\t}\n")
	b.WriteString(fmt.Sprintf("\treturn c.JSON(http.StatusOK, map[string]any{\"%s\": items})\n", plural))
	b.WriteString("}\n\n")

	// Show.
	b.WriteString(fmt.Sprintf("// Show returns a single %s by ID.\n", snake))
	b.WriteString(fmt.Sprintf("func (ctrl *%sController) Show(c *http.Context) error {\n", pascal))
	b.WriteString("\tid, _ := strconv.Atoi(c.Param(\"id\"))\n")
	b.WriteString(fmt.Sprintf("\tvar item map[string]any\n"))
	b.WriteString(fmt.Sprintf("\tif err := ctrl.DB.Table(\"%s\").Where(\"id = ?\", id).First(&item).Error; err != nil {\n", plural))
	b.WriteString("\t\treturn c.JSON(http.StatusNotFound, map[string]string{\"error\": \"Not found\"})\n")
	b.WriteString("\t}\n")
	b.WriteString(fmt.Sprintf("\treturn c.JSON(http.StatusOK, item)\n"))
	b.WriteString("}\n\n")

	// Store.
	b.WriteString(fmt.Sprintf("// Store creates a new %s.\n", snake))
	b.WriteString(fmt.Sprintf("func (ctrl *%sController) Store(c *http.Context) error {\n", pascal))
	if len(fields) > 0 {
		b.WriteString(fmt.Sprintf("\tvar body Create%sRequest\n", pascal))
		b.WriteString("\tif err := c.BodyParser(&body); err != nil {\n")
		b.WriteString("\t\treturn c.JSON(http.StatusBadRequest, map[string]string{\"error\": \"Invalid request\"})\n")
		b.WriteString("\t}\n")
		b.WriteString(fmt.Sprintf("\tif err := ctrl.DB.Table(\"%s\").Create(&body).Error; err != nil {\n", plural))
	} else {
		b.WriteString("\tvar body map[string]any\n")
		b.WriteString("\tif err := c.BodyParser(&body); err != nil {\n")
		b.WriteString("\t\treturn c.JSON(http.StatusBadRequest, map[string]string{\"error\": \"Invalid request\"})\n")
		b.WriteString("\t}\n")
		b.WriteString(fmt.Sprintf("\tif err := ctrl.DB.Table(\"%s\").Create(&body).Error; err != nil {\n", plural))
	}
	b.WriteString("\t\treturn c.JSON(http.StatusInternalServerError, map[string]string{\"error\": err.Error()})\n")
	b.WriteString("\t}\n")
	b.WriteString(fmt.Sprintf("\treturn c.JSON(http.StatusCreated, body)\n"))
	b.WriteString("}\n\n")

	// Update.
	b.WriteString(fmt.Sprintf("// Update modifies an existing %s.\n", snake))
	b.WriteString(fmt.Sprintf("func (ctrl *%sController) Update(c *http.Context) error {\n", pascal))
	b.WriteString("\tid, _ := strconv.Atoi(c.Param(\"id\"))\n")
	if len(fields) > 0 {
		b.WriteString(fmt.Sprintf("\tvar body Update%sRequest\n", pascal))
	} else {
		b.WriteString("\tvar body map[string]any\n")
	}
	b.WriteString("\tif err := c.BodyParser(&body); err != nil {\n")
	b.WriteString("\t\treturn c.JSON(http.StatusBadRequest, map[string]string{\"error\": \"Invalid request\"})\n")
	b.WriteString("\t}\n")
	b.WriteString(fmt.Sprintf("\tif err := ctrl.DB.Table(\"%s\").Where(\"id = ?\", id).Updates(&body).Error; err != nil {\n", plural))
	b.WriteString("\t\treturn c.JSON(http.StatusInternalServerError, map[string]string{\"error\": err.Error()})\n")
	b.WriteString("\t}\n")
	b.WriteString("\treturn c.JSON(http.StatusOK, map[string]string{\"message\": \"Updated\"})\n")
	b.WriteString("}\n\n")

	// Destroy.
	b.WriteString(fmt.Sprintf("// Destroy deletes a %s.\n", snake))
	b.WriteString(fmt.Sprintf("func (ctrl *%sController) Destroy(c *http.Context) error {\n", pascal))
	b.WriteString("\tid, _ := strconv.Atoi(c.Param(\"id\"))\n")
	b.WriteString(fmt.Sprintf("\tif err := ctrl.DB.Table(\"%s\").Where(\"id = ?\", id).Delete(nil).Error; err != nil {\n", plural))
	b.WriteString("\t\treturn c.JSON(http.StatusInternalServerError, map[string]string{\"error\": err.Error()})\n")
	b.WriteString("\t}\n")
	b.WriteString("\treturn c.JSON(http.StatusOK, map[string]string{\"message\": \"Deleted\"})\n")
	b.WriteString("}\n")

	return []GeneratedFile{
		{Path: fmt.Sprintf("app/controllers/%s.go", snake), Content: b.String()},
	}, nil
}

func generateMigrationCode(pascal, snake, plural string, fields []Field) ([]GeneratedFile, error) {
	ts := time.Now().Format("20060102150405")
	var b strings.Builder

	b.WriteString("package migrations\n\n")
	b.WriteString("import (\n\t\"gorm.io/gorm\"\n)\n\n")
	b.WriteString(fmt.Sprintf("// Migrate%s%s creates the %s table.\n", ts, pascal, plural))
	b.WriteString(fmt.Sprintf("type Migrate%s%s struct{}\n\n", ts, pascal))
	b.WriteString(fmt.Sprintf("func (m *Migrate%s%s) Up(db *gorm.DB) error {\n", ts, pascal))
	b.WriteString(fmt.Sprintf("\ttype %s struct {\n", pascal))
	b.WriteString("\t\tID        uint   `gorm:\"primaryKey\"`\n")
	for _, f := range fields {
		goType := fieldTypeToGo(f.Type)
		gormTag := ""
		if f.Unique {
			gormTag = ";uniqueIndex"
		}
		if f.Nullable {
			goType = "*" + goType
		}
		b.WriteString(fmt.Sprintf("\t\t%s %s `gorm:\"column:%s%s\"`\n",
			toPascalCase(f.Name), goType, toSnakeCase(f.Name), gormTag))
	}
	b.WriteString("\t\tCreatedAt int64\n")
	b.WriteString("\t\tUpdatedAt int64\n")
	b.WriteString("\t\tDeletedAt *int64 `gorm:\"index\"`\n")
	b.WriteString("\t}\n")
	b.WriteString(fmt.Sprintf("\treturn db.Table(\"%s\").AutoMigrate(&%s{})\n", plural, pascal))
	b.WriteString("}\n\n")
	b.WriteString(fmt.Sprintf("func (m *Migrate%s%s) Down(db *gorm.DB) error {\n", ts, pascal))
	b.WriteString(fmt.Sprintf("\treturn db.Migrator().DropTable(\"%s\")\n", plural))
	b.WriteString("}\n")

	return []GeneratedFile{
		{Path: fmt.Sprintf("database/migrations/%s_create_%s.go", ts, plural), Content: b.String()},
	}, nil
}

func generateRouteSnippet(pascal, snake, plural string) GeneratedFile {
	code := fmt.Sprintf(`// Routes for %s — add to start/routes.go:
//
//   %sCtrl := &controllers.%sController{DB: db}
//   api := app.Router.Group("/api")
//   api.Get("/%s", %sCtrl.Index).As("%s.index")
//   api.Get("/%s/:id", %sCtrl.Show).As("%s.show")
//   api.Post("/%s", %sCtrl.Store).As("%s.store")
//   api.Put("/%s/:id", %sCtrl.Update).As("%s.update")
//   api.Delete("/%s/:id", %sCtrl.Destroy).As("%s.destroy")
`,
		pascal,
		snake, pascal,
		plural, snake, plural,
		plural, snake, plural,
		plural, snake, plural,
		plural, snake, plural,
		plural, snake, plural,
	)
	return GeneratedFile{
		Path:    fmt.Sprintf("app/controllers/%s_routes.txt", snake),
		Content: code,
	}
}

// ---------------------------------------------------------------------------
// NLP Helpers
// ---------------------------------------------------------------------------

func matchesAny(text string, patterns ...string) bool {
	for _, p := range patterns {
		if strings.Contains(text, p) {
			return true
		}
	}
	return false
}

var entityNameRe = regexp.MustCompile(`(?i)(?:create|build|generate|scaffold|make|add)\s+(?:a\s+|an\s+)?(\w+)`)
var entityNameRe2 = regexp.MustCompile(`(?i)(\w+)\s+(?:model|controller|resource|crud|api|middleware|job|mailer|channel)`)

func extractEntityName(prompt string) string {
	// Try "create a <name>" pattern.
	if m := entityNameRe.FindStringSubmatch(prompt); len(m) > 1 {
		word := strings.ToLower(m[1])
		// Skip common verbs and articles.
		skip := map[string]bool{"new": true, "simple": true, "basic": true, "full": true, "complete": true, "rest": true, "crud": true, "api": true}
		if !skip[word] {
			return word
		}
	}

	// Try "<name> model/controller" pattern.
	if m := entityNameRe2.FindStringSubmatch(prompt); len(m) > 1 {
		return strings.ToLower(m[1])
	}

	// Extract first noun-like word after relevant keywords.
	words := strings.Fields(strings.ToLower(prompt))
	skipWords := map[string]bool{
		"a": true, "an": true, "the": true, "create": true, "build": true,
		"generate": true, "scaffold": true, "make": true, "add": true,
		"with": true, "for": true, "and": true, "new": true,
	}
	for _, w := range words {
		w = strings.Trim(w, ".,!?;:")
		if !skipWords[w] && len(w) > 2 {
			return w
		}
	}

	return "item"
}

var fieldPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)with\s+(?:fields?\s+)?(.+)`),
	regexp.MustCompile(`(?i)having\s+(.+)`),
	regexp.MustCompile(`(?i)fields?:\s*(.+)`),
	regexp.MustCompile(`(?i)columns?:\s*(.+)`),
}

func extractFields(prompt string) []Field {
	var fieldStr string
	for _, re := range fieldPatterns {
		if m := re.FindStringSubmatch(prompt); len(m) > 1 {
			fieldStr = m[1]
			break
		}
	}
	if fieldStr == "" {
		return nil
	}

	// Parse "title:string, body:text, published:bool" or "title, body, published_at"
	parts := strings.Split(fieldStr, ",")
	var fields []Field
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Remove trailing context words.
		for _, stopper := range []string{" and ", " with ", " for ", " in "} {
			if idx := strings.Index(part, stopper); idx > 0 {
				part = part[:idx]
			}
		}
		part = strings.TrimSpace(part)

		f := Field{}
		if strings.Contains(part, ":") {
			kv := strings.SplitN(part, ":", 2)
			f.Name = strings.TrimSpace(kv[0])
			f.Type = strings.TrimSpace(kv[1])
		} else {
			f.Name = part
			f.Type = inferType(part)
		}

		// Clean the name.
		f.Name = strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
				return r
			}
			return -1
		}, f.Name)

		if f.Name != "" {
			fields = append(fields, f)
		}
	}

	return fields
}

// inferType guesses a field type from its name.
func inferType(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, "_at") || strings.HasSuffix(lower, "date") || lower == "timestamp":
		return "time"
	case strings.HasSuffix(lower, "_id") || lower == "id" || lower == "count" || lower == "age" || lower == "quantity" || lower == "amount":
		return "int"
	case lower == "price" || lower == "total" || lower == "cost" || lower == "rate" || lower == "salary":
		return "float"
	case lower == "active" || lower == "published" || lower == "enabled" || lower == "verified" || strings.HasPrefix(lower, "is_") || strings.HasPrefix(lower, "has_"):
		return "bool"
	case lower == "body" || lower == "content" || lower == "description" || lower == "text" || lower == "bio" || lower == "notes":
		return "text"
	case lower == "data" || lower == "metadata" || lower == "config" || lower == "settings" || lower == "payload":
		return "json"
	case lower == "email":
		return "string"
	case lower == "avatar" || lower == "image" || lower == "photo" || lower == "logo":
		return "string"
	default:
		return "string"
	}
}

func fieldTypeToGo(t string) string {
	switch strings.ToLower(t) {
	case "string", "varchar", "text", "char":
		return "string"
	case "int", "integer", "bigint":
		return "int64"
	case "uint":
		return "uint64"
	case "float", "double", "decimal", "numeric":
		return "float64"
	case "bool", "boolean":
		return "bool"
	case "time", "datetime", "timestamp", "date":
		return "time.Time"
	case "json", "jsonb":
		return "json.RawMessage"
	case "uuid":
		return "string"
	case "blob", "binary", "bytes":
		return "[]byte"
	default:
		return "string"
	}
}

func defaultFieldsForEntity(name string) []Field {
	lower := strings.ToLower(name)

	// Smart defaults based on common entity names.
	defaults := map[string][]Field{
		"post": {
			{Name: "title", Type: "string"},
			{Name: "body", Type: "text"},
			{Name: "slug", Type: "string", Unique: true},
			{Name: "published", Type: "bool"},
			{Name: "author_id", Type: "int"},
		},
		"blog": {
			{Name: "title", Type: "string"},
			{Name: "content", Type: "text"},
			{Name: "slug", Type: "string", Unique: true},
			{Name: "published", Type: "bool"},
		},
		"user": {
			{Name: "name", Type: "string"},
			{Name: "email", Type: "string", Unique: true},
			{Name: "password", Type: "string"},
			{Name: "avatar", Type: "string", Nullable: true},
		},
		"product": {
			{Name: "name", Type: "string"},
			{Name: "description", Type: "text"},
			{Name: "price", Type: "float"},
			{Name: "sku", Type: "string", Unique: true},
			{Name: "quantity", Type: "int"},
			{Name: "active", Type: "bool"},
		},
		"comment": {
			{Name: "body", Type: "text"},
			{Name: "author_id", Type: "int"},
			{Name: "post_id", Type: "int"},
		},
		"category": {
			{Name: "name", Type: "string"},
			{Name: "slug", Type: "string", Unique: true},
			{Name: "description", Type: "text", Nullable: true},
			{Name: "parent_id", Type: "int", Nullable: true},
		},
		"tag": {
			{Name: "name", Type: "string", Unique: true},
			{Name: "slug", Type: "string", Unique: true},
		},
		"order": {
			{Name: "user_id", Type: "int"},
			{Name: "total", Type: "float"},
			{Name: "status", Type: "string"},
			{Name: "notes", Type: "text", Nullable: true},
		},
		"task": {
			{Name: "title", Type: "string"},
			{Name: "description", Type: "text", Nullable: true},
			{Name: "status", Type: "string"},
			{Name: "priority", Type: "int"},
			{Name: "due_date", Type: "time", Nullable: true},
			{Name: "assignee_id", Type: "int", Nullable: true},
		},
		"todo": {
			{Name: "title", Type: "string"},
			{Name: "completed", Type: "bool"},
			{Name: "due_date", Type: "time", Nullable: true},
		},
		"article": {
			{Name: "title", Type: "string"},
			{Name: "content", Type: "text"},
			{Name: "slug", Type: "string", Unique: true},
			{Name: "author_id", Type: "int"},
			{Name: "published_at", Type: "time", Nullable: true},
		},
		"event": {
			{Name: "title", Type: "string"},
			{Name: "description", Type: "text"},
			{Name: "location", Type: "string"},
			{Name: "starts_at", Type: "time"},
			{Name: "ends_at", Type: "time"},
			{Name: "capacity", Type: "int"},
		},
		"notification": {
			{Name: "type", Type: "string"},
			{Name: "title", Type: "string"},
			{Name: "body", Type: "text"},
			{Name: "read", Type: "bool"},
			{Name: "user_id", Type: "int"},
		},
	}

	if d, ok := defaults[lower]; ok {
		return d
	}

	// Generic defaults.
	return []Field{
		{Name: "name", Type: "string"},
		{Name: "description", Type: "text", Nullable: true},
		{Name: "status", Type: "string"},
	}
}

// ---------------------------------------------------------------------------
// String Utilities
// ---------------------------------------------------------------------------

func sanitizeName(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			return r
		}
		return -1
	}, s)
	return strings.TrimSpace(strings.ToLower(s))
}

func toPascalCase(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '_' || r == '-' || r == ' '
	})
	var result strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		result.WriteString(strings.ToUpper(p[:1]) + p[1:])
	}
	if result.Len() == 0 {
		return "Item"
	}
	return result.String()
}

func toSnakeCase(s string) string {
	var result strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) && i > 0 {
			result.WriteRune('_')
		}
		result.WriteRune(unicode.ToLower(r))
	}
	return result.String()
}

func pluralize(s string) string {
	if strings.HasSuffix(s, "s") {
		return s + "es"
	}
	if strings.HasSuffix(s, "y") && len(s) > 1 {
		ch := s[len(s)-2]
		if ch != 'a' && ch != 'e' && ch != 'i' && ch != 'o' && ch != 'u' {
			return s[:len(s)-1] + "ies"
		}
	}
	if strings.HasSuffix(s, "ch") || strings.HasSuffix(s, "sh") || strings.HasSuffix(s, "x") || strings.HasSuffix(s, "z") {
		return s + "es"
	}
	return s + "s"
}

// Ensure json import exists. We use it for RawMessage type reference.
var _ = json.RawMessage{}
