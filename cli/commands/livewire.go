package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/CodeSyncr/nimbus/cli"
)

func init() {
	cli.RegisterCommand(&LivewireLayout{})
	cli.RegisterCommand(&MakeLivewire{})
}

// ── livewire:layout ─────────────────────────────────────────────

type LivewireLayout struct{}

func (c *LivewireLayout) Name() string        { return "livewire:layout" }
func (c *LivewireLayout) Description() string { return "Create the default Livewire layout view" }
func (c *LivewireLayout) Args() int           { return 0 }

func (c *LivewireLayout) Run(ctx *cli.Context) error {
	_ = os.MkdirAll(filepath.Join("resources", "views", "layouts"), 0755)
	path := filepath.Join("resources", "views", "layouts", "app.nimbus")
	if _, err := os.Stat(path); err == nil {
		ctx.UI.Warnf("%s already exists", path)
		return nil
	}

	const content = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width,initial-scale=1"/>
  <title>{{ if .title }}{{ .title }}{{ else }}{{ if .appName }}{{ .appName }}{{ else }}Nimbus{{ end }}{{ end }}</title>
  {{ livewireStyles }}
</head>
<body>
  {{ .embed }}
  {{ livewireScripts }}
</body>
</html>
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return err
	}
	ctx.UI.Successf("Created %s", path)
	return nil
}

// ── make:livewire ───────────────────────────────────────────────

type MakeLivewire struct{}

func (c *MakeLivewire) Name() string        { return "make:livewire" }
func (c *MakeLivewire) Description() string { return "Create a new Livewire-style component (view + Go component)" }
func (c *MakeLivewire) Args() int           { return 1 }

func (c *MakeLivewire) Run(ctx *cli.Context) error {
	raw := strings.TrimSpace(ctx.Args[0])
	if raw == "" {
		return fmt.Errorf("component name required")
	}

	viewName, viewPath := livewireViewTarget(raw)
	goName := livewireGoName(raw)
	goFile := filepath.Join("app", "livewire", strings.ToLower(goName)+".go")

	_ = os.MkdirAll(filepath.Dir(viewPath), 0755)
	_ = os.MkdirAll(filepath.Dir(goFile), 0755)

	if _, err := os.Stat(viewPath); err != nil {
		viewStub := `<div>
  <h2>{{ .title }}</h2>
  <p>Component: ` + raw + `</p>
</div>
`
		_ = os.WriteFile(viewPath, []byte(viewStub), 0644)
		ctx.UI.Successf("Created %s", viewPath)
	}

	if _, err := os.Stat(goFile); err == nil {
		ctx.UI.Warnf("%s already exists", goFile)
		return nil
	}

	goStub := `package livewire

import (
	"html/template"

	lw "github.com/CodeSyncr/nimbus-livewire/livewire"
)

type ` + goName + ` struct {
	// Add state fields (public fields, e.g. Count int ` + "`json:\"count\"`" + `)
}

func (c *` + goName + `) Render() (template.HTML, error) {
	return lw.RenderView("` + viewName + `", map[string]any{"title": "` + raw + `"})
}

// Optional: Add action methods or lifecycle hooks
//
// func (c *` + goName + `) Increment() {
// 	c.Count++
// }

func init() {
	lw.Register("` + raw + `", func() *` + goName + ` { return &` + goName + `{} })
}
`
	if err := os.WriteFile(goFile, []byte(goStub), 0644); err != nil {
		return err
	}
	ctx.UI.Successf("Created %s", goFile)
	return nil
}

func livewireViewTarget(name string) (viewName string, viewPath string) {
	// Support pages::foo.bar (Livewire convention)
	if strings.HasPrefix(name, "pages::") {
		rest := strings.TrimPrefix(name, "pages::")
		rest = strings.ReplaceAll(rest, ".", "/")
		viewName = "pages/" + rest
		viewPath = filepath.Join("resources", "views", "pages", rest) + ".nimbus"
		return
	}
	// Default: components/<path>
	rest := strings.ReplaceAll(name, ".", "/")
	viewName = "components/" + rest
	viewPath = filepath.Join("resources", "views", "components", rest) + ".nimbus"
	return
}

func livewireGoName(name string) string {
	// Build a stable Go identifier (very simple).
	name = strings.ReplaceAll(name, "pages::", "pages_")
	name = strings.ReplaceAll(name, "::", "_")
	name = strings.ReplaceAll(name, ".", "_")
	name = strings.ReplaceAll(name, "-", "_")
	name = strings.Trim(name, "_")
	if name == "" {
		return "Component"
	}
	parts := strings.Split(name, "_")
	for i := range parts {
		if parts[i] == "" {
			continue
		}
		parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
	}
	return strings.Join(parts, "")
}

