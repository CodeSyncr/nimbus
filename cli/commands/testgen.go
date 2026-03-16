package commands

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/CodeSyncr/nimbus/cli"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

func init() {
	cli.RegisterCommand(&TestGenCommand{})
}

// TestGenCommand generates tests from controllers and handlers.
type TestGenCommand struct {
	controller string
	output     string
	all        bool
}

func (c *TestGenCommand) Name() string        { return "test:generate" }
func (c *TestGenCommand) Description() string { return "Generate tests from controllers/handlers" }
func (c *TestGenCommand) Args() int           { return -1 }
func (c *TestGenCommand) Aliases() []string   { return []string{"test:gen", "tg"} }

func (c *TestGenCommand) Flags(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&c.controller, "controller", "c", "", "Specific controller file to generate tests for")
	cmd.Flags().StringVarP(&c.output, "output", "o", "", "Output directory (default: same directory with _test.go suffix)")
	cmd.Flags().BoolVarP(&c.all, "all", "a", false, "Generate tests for all controllers")
}

func (c *TestGenCommand) Run(ctx *cli.Context) error {
	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e"))
	fileStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#818cf8"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#64748b"))

	fmt.Fprintln(ctx.Stdout, dimStyle.Render("  Analyzing controllers..."))

	controllersDir := filepath.Join(ctx.AppRoot, "app", "controllers")

	var files []string
	if c.controller != "" {
		files = []string{filepath.Join(controllersDir, c.controller)}
	} else if c.all || len(ctx.Args) == 0 {
		entries, err := os.ReadDir(controllersDir)
		if err != nil {
			return fmt.Errorf("failed to read controllers directory: %w", err)
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") && !strings.HasSuffix(e.Name(), "_test.go") {
				files = append(files, filepath.Join(controllersDir, e.Name()))
			}
		}
	} else {
		for _, arg := range ctx.Args {
			if !strings.HasSuffix(arg, ".go") {
				arg += ".go"
			}
			files = append(files, filepath.Join(controllersDir, arg))
		}
	}

	if len(files) == 0 {
		ctx.UI.Infof("No controller files found in app/controllers/")
		return nil
	}

	totalGenerated := 0
	for _, file := range files {
		methods, err := analyzeController(file)
		if err != nil {
			ctx.UI.Errorf("Failed to analyze %s: %v", filepath.Base(file), err)
			continue
		}

		if len(methods) == 0 {
			continue
		}

		testCode := generateTestCode(file, methods)

		outPath := c.output
		if outPath == "" {
			outPath = strings.TrimSuffix(file, ".go") + "_test.go"
		} else {
			outPath = filepath.Join(outPath, strings.TrimSuffix(filepath.Base(file), ".go")+"_test.go")
		}

		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(outPath, []byte(testCode), 0644); err != nil {
			return err
		}

		relPath, _ := filepath.Rel(ctx.AppRoot, outPath)
		fmt.Fprintln(ctx.Stdout,
			successStyle.Render("  ✓ ")+
				fileStyle.Render(relPath)+
				dimStyle.Render(fmt.Sprintf(" (%d tests)", len(methods))))
		totalGenerated += len(methods)
	}

	fmt.Fprintln(ctx.Stdout)
	fmt.Fprintln(ctx.Stdout, successStyle.Render(fmt.Sprintf("  Generated %d tests from %d files", totalGenerated, len(files))))
	fmt.Fprintln(ctx.Stdout, dimStyle.Render("  Run: go test ./app/controllers/..."))

	return nil
}

// ---------------------------------------------------------------------------
// Controller Analysis
// ---------------------------------------------------------------------------

// ControllerMethod is a parsed controller method.
type ControllerMethod struct {
	ReceiverType string // e.g., "TodoController"
	Name         string // e.g., "Index", "Store"
	HTTPMethod   string // inferred: GET, POST, PUT, DELETE
	PathPattern  string // inferred: /todos, /todos/:id
	HasBody      bool   // whether it reads a body
	HasParam     bool   // whether it reads path params
}

// analyzeController parses a Go file and extracts controller methods.
func analyzeController(filePath string) ([]ControllerMethod, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.AllErrors)
	if err != nil {
		return nil, err
	}

	var methods []ControllerMethod

	ast.Inspect(node, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
			return true
		}

		// Get receiver type.
		var recvType string
		switch t := fn.Recv.List[0].Type.(type) {
		case *ast.StarExpr:
			if ident, ok := t.X.(*ast.Ident); ok {
				recvType = ident.Name
			}
		case *ast.Ident:
			recvType = t.Name
		}

		if recvType == "" || !strings.HasSuffix(recvType, "Controller") {
			return true
		}

		// Check if it looks like a handler (takes *http.Context, returns error).
		if fn.Type.Params == nil || fn.Type.Params.NumFields() != 1 {
			return true
		}
		if fn.Type.Results == nil || fn.Type.Results.NumFields() != 1 {
			return true
		}

		method := ControllerMethod{
			ReceiverType: recvType,
			Name:         fn.Name.Name,
		}

		// Analyze the function body for clues.
		src, err := os.ReadFile(filePath)
		if err == nil {
			bodyStart := fset.Position(fn.Body.Pos()).Offset
			bodyEnd := fset.Position(fn.Body.End()).Offset
			if bodyEnd <= len(src) {
				body := string(src[bodyStart:bodyEnd])
				method.HasBody = strings.Contains(body, "BodyParser") || strings.Contains(body, "Body()")
				method.HasParam = strings.Contains(body, "Param(") || strings.Contains(body, "Params[")
			}
		}

		// Infer HTTP method and path from function name.
		method.HTTPMethod, method.PathPattern = inferHTTPMethodAndPath(method.Name, recvType)

		methods = append(methods, method)
		return true
	})

	return methods, nil
}

// inferHTTPMethodAndPath guesses the HTTP method and path from the handler name.
func inferHTTPMethodAndPath(name, controller string) (string, string) {
	resource := strings.TrimSuffix(controller, "Controller")
	resource = strings.ToLower(resource)
	plural := pluralize(resource)
	base := "/" + plural

	switch name {
	case "Index", "List":
		return "GET", base
	case "Show", "Get", "Find":
		return "GET", base + "/:id"
	case "Store", "Create", "Add":
		return "POST", base
	case "Update", "Edit", "Patch":
		return "PUT", base + "/:id"
	case "Destroy", "Delete", "Remove":
		return "DELETE", base + "/:id"
	default:
		// Check common prefixes.
		lower := strings.ToLower(name)
		if strings.HasPrefix(lower, "get") || strings.HasPrefix(lower, "list") || strings.HasPrefix(lower, "fetch") {
			return "GET", base + "/" + toSnakeCase(name)
		}
		if strings.HasPrefix(lower, "create") || strings.HasPrefix(lower, "post") || strings.HasPrefix(lower, "add") {
			return "POST", base + "/" + toSnakeCase(name)
		}
		if strings.HasPrefix(lower, "update") || strings.HasPrefix(lower, "put") || strings.HasPrefix(lower, "edit") {
			return "PUT", base + "/" + toSnakeCase(name)
		}
		if strings.HasPrefix(lower, "delete") || strings.HasPrefix(lower, "remove") {
			return "DELETE", base + "/" + toSnakeCase(name)
		}
		return "GET", base + "/" + toSnakeCase(name)
	}
}

// ---------------------------------------------------------------------------
// Test Code Generation
// ---------------------------------------------------------------------------

func generateTestCode(filePath string, methods []ControllerMethod) string {
	pkg := "controllers"
	// Detect package from filename location.
	dir := filepath.Dir(filePath)
	pkg = filepath.Base(dir)

	var b strings.Builder
	b.WriteString(fmt.Sprintf("package %s\n\n", pkg))
	b.WriteString(`import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

// testApp creates a test router with the given handler registered.
func testApp(method, path string, handler router.HandlerFunc) *router.Router {
	r := router.New()
	switch method {
	case "GET":
		r.Get(path, handler)
	case "POST":
		r.Post(path, handler)
	case "PUT":
		r.Put(path, handler)
	case "PATCH":
		r.Patch(path, handler)
	case "DELETE":
		r.Delete(path, handler)
	}
	return r
}

// doRequest performs an HTTP request against a test router.
func doRequest(t *testing.T, r *router.Router, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reqBody *bytes.Buffer
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("failed to marshal body: %v", err)
		}
		reqBody = bytes.NewBuffer(b)
	} else {
		reqBody = &bytes.Buffer{}
	}
	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// parseJSON parses a JSON response body.
func parseJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return result
}

`)

	// Group methods by controller.
	byCtrl := make(map[string][]ControllerMethod)
	for _, m := range methods {
		byCtrl[m.ReceiverType] = append(byCtrl[m.ReceiverType], m)
	}

	for ctrl, ctrlMethods := range byCtrl {
		for _, m := range ctrlMethods {
			b.WriteString(generateMethodTest(ctrl, m))
			b.WriteString("\n")
		}
	}

	return b.String()
}

func generateMethodTest(ctrl string, m ControllerMethod) string {
	var b strings.Builder
	testName := fmt.Sprintf("Test%s_%s", ctrl, m.Name)

	b.WriteString(fmt.Sprintf("func %s(t *testing.T) {\n", testName))
	b.WriteString(fmt.Sprintf("\tctrl := &%s{}\n", ctrl))
	b.WriteString(fmt.Sprintf("\t// TODO: Initialize controller dependencies (e.g., ctrl.DB = testDB)\n\n"))

	b.WriteString("\ttests := []struct {\n")
	b.WriteString("\t\tname       string\n")
	b.WriteString("\t\tmethod     string\n")
	b.WriteString("\t\tpath       string\n")
	if m.HasBody {
		b.WriteString("\t\tbody       any\n")
	}
	b.WriteString("\t\twantStatus int\n")
	b.WriteString("\t\twantKey    string // response JSON key to check\n")
	b.WriteString("\t}{\n")

	// Generate test cases based on method type.
	switch m.Name {
	case "Index", "List":
		b.WriteString(fmt.Sprintf(`		{
			name:       "success - list all",
			method:     "%s",
			path:       "%s",
			wantStatus: http.StatusOK,
		},
`, m.HTTPMethod, m.PathPattern))

	case "Show", "Get", "Find":
		path := strings.Replace(m.PathPattern, ":id", "1", 1)
		b.WriteString(fmt.Sprintf(`		{
			name:       "success - get by id",
			method:     "%s",
			path:       "%s",
			wantStatus: http.StatusOK,
		},
		{
			name:       "not found",
			method:     "%s",
			path:       "%s",
			wantStatus: http.StatusNotFound,
			wantKey:    "error",
		},
`, m.HTTPMethod, path, m.HTTPMethod, strings.Replace(m.PathPattern, ":id", "999999", 1)))

	case "Store", "Create", "Add":
		b.WriteString(fmt.Sprintf(`		{
			name:       "success - create",
			method:     "%s",
			path:       "%s",
			body:       map[string]any{"name": "Test Item"},
			wantStatus: http.StatusCreated,
		},
		{
			name:       "invalid body",
			method:     "%s",
			path:       "%s",
			body:       nil,
			wantStatus: http.StatusBadRequest,
			wantKey:    "error",
		},
`, m.HTTPMethod, m.PathPattern, m.HTTPMethod, m.PathPattern))

	case "Update", "Edit", "Patch":
		path := strings.Replace(m.PathPattern, ":id", "1", 1)
		b.WriteString(fmt.Sprintf(`		{
			name:       "success - update",
			method:     "%s",
			path:       "%s",
			body:       map[string]any{"name": "Updated"},
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid body",
			method:     "%s",
			path:       "%s",
			body:       nil,
			wantStatus: http.StatusBadRequest,
			wantKey:    "error",
		},
`, m.HTTPMethod, path, m.HTTPMethod, path))

	case "Destroy", "Delete", "Remove":
		path := strings.Replace(m.PathPattern, ":id", "1", 1)
		b.WriteString(fmt.Sprintf(`		{
			name:       "success - delete",
			method:     "%s",
			path:       "%s",
			wantStatus: http.StatusOK,
		},
`, m.HTTPMethod, path))

	default:
		b.WriteString(fmt.Sprintf(`		{
			name:       "success",
			method:     "%s",
			path:       "%s",
			wantStatus: http.StatusOK,
		},
`, m.HTTPMethod, m.PathPattern))
	}

	b.WriteString("\t}\n\n")

	// Test loop.
	b.WriteString("\tfor _, tt := range tests {\n")
	b.WriteString("\t\tt.Run(tt.name, func(t *testing.T) {\n")
	b.WriteString(fmt.Sprintf("\t\t\tr := testApp(tt.method, \"%s\", ctrl.%s)\n", m.PathPattern, m.Name))

	if m.HasBody {
		b.WriteString("\t\t\trec := doRequest(t, r, tt.method, tt.path, tt.body)\n")
	} else {
		b.WriteString("\t\t\trec := doRequest(t, r, tt.method, tt.path, nil)\n")
	}

	b.WriteString(`
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			if tt.wantKey != "" {
				body := parseJSON(t, rec)
				if _, ok := body[tt.wantKey]; !ok {
					t.Errorf("response missing key %q", tt.wantKey)
				}
			}
`)

	b.WriteString("\t\t})\n")
	b.WriteString("\t}\n")
	b.WriteString("}\n")

	return b.String()
}
