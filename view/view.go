package view

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Engine renders .nimbus templates (plan: {{ variable }}, @if, @each, ctx.View()).
type Engine struct {
	root   string
	funcs  template.FuncMap
	cache  map[string]*template.Template
}

// New creates a view engine with views loaded from root (e.g. "views").
func New(root string, funcs template.FuncMap) *Engine {
	if funcs == nil {
		funcs = template.FuncMap{}
	}
	return &Engine{root: root, funcs: funcs, cache: make(map[string]*template.Template)}
}

// Render renders the named view (e.g. "home" -> views/home.nimbus or views/pages/home.nimbus) with data.
func (e *Engine) Render(name string, data any) (string, error) {
	t, err := e.parse(name)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return "", err
	}
	return b.String(), nil
}

// RenderWriter writes the rendered view to w.
func (e *Engine) RenderWriter(name string, data any, w io.Writer) error {
	t, err := e.parse(name)
	if err != nil {
		return err
	}
	return t.Execute(w, data)
}

func (e *Engine) parse(name string) (*template.Template, error) {
	if t, ok := e.cache[name]; ok {
		return t, nil
	}
	path := filepath.Join(e.root, name+".nimbus")
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	converted := e.convertNimbusToGo(string(body))
	t, err := template.New(name).Funcs(e.funcs).Parse(converted)
	if err != nil {
		return nil, err
	}
	e.cache[name] = t
	return t, nil
}

// convertNimbusToGo turns @if/@each/{{ }} into Go template syntax.
func (e *Engine) convertNimbusToGo(s string) string {
	// {{ variable }} -> {{ .variable }} if single word, else leave for Go template
	s = regexp.MustCompile(`\{\{\s*([a-zA-Z_][a-zA-Z0-9_.]*)\s*\}\}`).ReplaceAllString(s, "{{ .$1 }}")
	// @if(condition) -> {{ if condition }}
	s = regexp.MustCompile(`@if\s*\((.*?)\)`).ReplaceAllString(s, "{{ if $1 }}")
	// @else -> {{ else }}
	s = strings.ReplaceAll(s, "@else", "{{ else }}")
	// @endif or @end -> {{ end }}
	s = regexp.MustCompile(`@(end|endif)`).ReplaceAllString(s, "{{ end }}")
	// @each(list) -> {{ range .list }}
	s = regexp.MustCompile(`@each\s*\((.*?)\)`).ReplaceAllString(s, "{{ range .$1 }}")
	// @endeach -> {{ end }}
	s = strings.ReplaceAll(s, "@endeach", "{{ end }}")
	return s
}

// Default engine (root "views"). Set via SetRoot or use New in app.
var Default *Engine

func init() {
	Default = New("views", nil)
}

// SetRoot sets the default engine root and clears cache.
func SetRoot(root string) {
	Default = New(root, Default.funcs)
}

// Render is a shortcut for Default.Render.
func Render(name string, data any) (string, error) {
	if Default == nil {
		return "", fmt.Errorf("view: default engine not set")
	}
	return Default.Render(name, data)
}
