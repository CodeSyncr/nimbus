package view

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// defaultFuncs returns Edge-style template helpers (raw, dump, dict, slot).
func defaultFuncs() template.FuncMap {
	return template.FuncMap{
		"raw": func(v any) template.HTML {
			if h, ok := v.(template.HTML); ok {
				return h
			}
			return template.HTML(fmt.Sprint(v))
		},
		"dump": func(v any) template.HTML {
			b, _ := json.MarshalIndent(v, "", "  ")
			return template.HTML("<pre>" + template.HTMLEscapeString(string(b)) + "</pre>")
		},
		"dict": func(kv ...any) map[string]any {
			m := make(map[string]any)
			for i := 0; i+1 < len(kv); i += 2 {
				if k, ok := kv[i].(string); ok {
					m[k] = kv[i+1]
				}
			}
			return m
		},
		"len": func(v any) int {
			if v == nil {
				return 0
			}
			switch val := v.(type) {
			case string:
				return len(val)
			case []any:
				return len(val)
			case map[string]any:
				return len(val)
			}
			// Fallback for reflection if needed, but the above covers common cases in this framework.
			return 0
		},
	}
}

// Engine renders .nimbus templates (Edge-style: {{ variable }}, {{{ raw }}}, @if, @each, @layout, @component).
type Engine struct {
	root         string
	funcs        template.FuncMap
	cache        map[string]*template.Template
	layout       map[string]string // view name -> layout name
	slotTemplate *template.Template
	slotCounter  int
}

// New creates a view engine with views loaded from root (e.g. "views"). Templates use .nimbus extension.
func New(root string, funcs template.FuncMap) *Engine {
	if funcs == nil {
		funcs = template.FuncMap{}
	}
	merged := defaultFuncs()
	for k, v := range funcs {
		merged[k] = v
	}
	e := &Engine{root: root, funcs: merged, cache: make(map[string]*template.Template), layout: make(map[string]string)}
	merged["slot"] = func(name string, data any) (template.HTML, error) {
		if e.slotTemplate == nil {
			return "", fmt.Errorf("view: slot template not ready")
		}
		var b bytes.Buffer
		if err := e.slotTemplate.ExecuteTemplate(&b, name, data); err != nil {
			return "", err
		}
		return template.HTML(b.String()), nil
	}
	merged["include"] = func(name string, data any) (template.HTML, error) {
		if data == nil {
			data = map[string]any{}
		}
		rendered, err := e.Render(name, data)
		if err != nil {
			return "", err
		}
		return template.HTML(rendered), nil
	}
	e.funcs = merged
	return e
}

// Render renders the named view (e.g. "home" -> views/home.nimbus) with data. Supports @layout('layout') like Edge.
func (e *Engine) Render(name string, data any) (string, error) {
	t, err := e.parse(name)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return "", err
	}
	body := b.String()
	if layoutName, ok := e.layout[name]; ok {
		layoutData := map[string]any{}
		if m, _ := data.(map[string]any); m != nil {
			for k, v := range m {
				layoutData[k] = v
			}
		}
		layoutData["embed"] = template.HTML(body)
		layoutData["content"] = template.HTML(body)
		return e.Render(layoutName, layoutData)
	}
	return body, nil
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
		e.slotTemplate = t
		return t, nil
	}
	body, _, err := e.readView(name)
	if err != nil {
		return nil, err
	}
	s := string(body)
	// Edge-style @layout('name') on first line
	if layoutName := parseLayoutLine(s); layoutName != "" {
		e.layout[name] = layoutName
		s = stripLayoutLine(s)
	}
	e.slotCounter = 0
	converted := e.convertNimbusToGo(s)
	t, err := template.New(name).Funcs(e.funcs).Parse(converted)
	if err != nil {
		return nil, err
	}
	// Parse components from views/components/ and add to template set
	t, err = e.parseComponents(t)
	if err != nil {
		return nil, err
	}
	e.slotTemplate = t
	e.cache[name] = t
	return t, nil
}

func (e *Engine) parseComponents(t *template.Template) (*template.Template, error) {
	componentsDir := filepath.Join(e.root, "components")
	entries, err := os.ReadDir(componentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return t, nil
		}
		return nil, err
	}
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".nimbus") {
			continue
		}
		compPath := filepath.Join(componentsDir, ent.Name())
		body, err := os.ReadFile(compPath)
		if err != nil {
			return nil, err
		}
		compName := strings.TrimSuffix(ent.Name(), ".nimbus")
		tmplName := "components/" + compName
		converted := e.convertNimbusToGo(string(body))
		_, err = t.New(tmplName).Parse(converted)
		if err != nil {
			return nil, fmt.Errorf("component %s: %w", compName, err)
		}
	}
	return t, nil
}

func parseLayoutLine(s string) string {
	s = strings.TrimSpace(s)
	prefix := "@layout("
	if !strings.HasPrefix(s, prefix) {
		return ""
	}
	start := len(prefix) // first char inside the parens
	end := strings.Index(s[start:], ")")
	if end == -1 {
		return ""
	}
	inner := strings.TrimSpace(s[start : start+end])
	return strings.Trim(inner, "'\"")
}

func stripLayoutLine(s string) string {
	idx := strings.Index(s, "\n")
	if idx == -1 {
		return ""
	}
	return strings.TrimLeft(s[idx+1:], "\n\r")
}

// convertNimbusToGo turns Edge-style syntax into Go template syntax.
// Content inside <code>...</code> and <pre>...</pre> is left unchanged (for docs).
func (e *Engine) convertNimbusToGo(s string) string {
	// Protect code/pre blocks from conversion (so docs can show literal syntax)
	var blocks []string
	placeholder := " __NIMBUS_RAW_%d__ "
	replaceBlock := func(m string) string {
		blocks = append(blocks, m)
		return fmt.Sprintf(placeholder, len(blocks)-1)
	}
	s = regexp.MustCompile(`(?s)<code[^>]*>.*?</code>`).ReplaceAllStringFunc(s, replaceBlock)
	s = regexp.MustCompile(`(?s)<pre[^>]*>.*?</pre>`).ReplaceAllStringFunc(s, replaceBlock)

	// @componentName() ... @end -> slot + template invoke (must run before @end conversion)
	s = e.convertComponents(s)

	// {{-- comment --}} -> strip entirely
	s = regexp.MustCompile(`\{\{--.*?--\}\}`).ReplaceAllString(s, "")

	// {{{ expr }}} -> {{ raw expr }} (unescaped HTML, Edge-style)
	s = regexp.MustCompile(`\{\{\{\s*(.*?)\s*\}\}\}`).ReplaceAllStringFunc(s, func(m string) string {
		sub := regexp.MustCompile(`\{\{\{\s*(.*?)\s*\}\}\}`).FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		expr := strings.TrimSpace(sub[1])
		if expr != "" && !strings.HasPrefix(expr, ".") && !strings.HasPrefix(expr, "$") {
			expr = "." + expr
		}
		return "{{ raw " + expr + " }}"
	})

	// {{ variable }} -> {{ .variable }} (escaped output)
	goKeywords := map[string]bool{"end": true, "else": true, "if": true, "range": true, "with": true, "template": true, "define": true, "block": true}
	s = regexp.MustCompile(`\{\{\s*([a-zA-Z_][a-zA-Z0-9_.]*)\s*\}\}`).ReplaceAllStringFunc(s, func(m string) string {
		sub := regexp.MustCompile(`\{\{\s*([a-zA-Z_][a-zA-Z0-9_.]*)\s*\}\}`).FindStringSubmatch(m)
		if len(sub) < 2 || sub[1] == "." || goKeywords[sub[1]] {
			return m
		}
		return "{{ ." + sub[1] + " }}"
	})

	// @dump(expr) -> {{ dump expr }} (debug, Edge-style)
	s = regexp.MustCompile(`@dump\s*\((.*?)\)`).ReplaceAllStringFunc(s, func(m string) string {
		sub := regexp.MustCompile(`@dump\s*\((.*?)\)`).FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		expr := strings.TrimSpace(sub[1])
		if expr == "state" {
			return "{{ dump . }}"
		}
		if expr != "" && !strings.HasPrefix(expr, ".") && expr[0] != '$' && !strings.Contains(expr, " ") {
			expr = "." + expr
		}
		return "{{ dump " + expr + " }}"
	})

	// @include('viewname') or @include("viewname") -> {{ include "viewname" . }}
	s = regexp.MustCompile(`@include\s*\(\s*['"]([^'"]+)['"]\s*\)`).ReplaceAllStringFunc(s, func(m string) string {
		sub := regexp.MustCompile(`@include\s*\(\s*['"]([^'"]+)['"]\s*\)`).FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		viewName := sub[1]
		return `{{ include "` + viewName + `" . }}`
	})

	// @if(condition) -> {{ if condition }}
	s = regexp.MustCompile(`@if\s*\((.*?)\)`).ReplaceAllStringFunc(s, func(m string) string {
		sub := regexp.MustCompile(`@if\s*\((.*?)\)`).FindStringSubmatch(m)
		condition := strings.TrimSpace(sub[1])
		// Convert ! to not for Go templates
		condition = strings.ReplaceAll(condition, "!", "not ")
		return "{{ if " + condition + " }}"
	})
	// @else if(condition) or @elseif(condition) -> {{ else if condition }} (must run before @else)
	s = regexp.MustCompile(`@else\s+if\s*\((.*?)\)`).ReplaceAllStringFunc(s, func(m string) string {
		sub := regexp.MustCompile(`@else\s+if\s*\((.*?)\)`).FindStringSubmatch(m)
		condition := strings.TrimSpace(sub[1])
		condition = strings.ReplaceAll(condition, "!", "not ")
		return "{{ else if " + condition + " }}"
	})
	s = regexp.MustCompile(`@elseif\s*\((.*?)\)`).ReplaceAllStringFunc(s, func(m string) string {
		sub := regexp.MustCompile(`@elseif\s*\((.*?)\)`).FindStringSubmatch(m)
		condition := strings.TrimSpace(sub[1])
		condition = strings.ReplaceAll(condition, "!", "not ")
		return "{{ else if " + condition + " }}"
	})
	// @else -> {{ else }}
	s = strings.ReplaceAll(s, "@else", "{{ else }}")
	// @endeach, @endif, @endrange, or @end -> {{ end }}
	s = regexp.MustCompile(`@(endeach|endif|endrange|end)`).ReplaceAllString(s, "{{ end }}")

	// @each(item in collection) or @each(collection) -> {{ range }}
	s = regexp.MustCompile(`@each\s*\((.*?)\)`).ReplaceAllStringFunc(s, func(m string) string {
		sub := regexp.MustCompile(`@each\s*\((.*?)\)`).FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		inner := strings.TrimSpace(sub[1])
		if strings.Contains(inner, " in ") {
			parts := strings.SplitN(inner, " in ", 2)
			if len(parts) == 2 {
				varname := strings.TrimSpace(parts[0])
				collection := strings.TrimSpace(parts[1])
				if !strings.HasPrefix(collection, ".") {
					collection = "." + collection
				}
				return "{{ range $" + varname + " := " + collection + " }}"
			}
		}
		// @each(items) -> {{ range .items }}
		collection := inner
		if !strings.HasPrefix(collection, ".") {
			collection = "." + collection
		}
		return "{{ range " + collection + " }}"
	})

	// @range $k, $v := .expr -> {{ range $k, $v := .expr }}
	s = regexp.MustCompile(`@range\s+(.*)`).ReplaceAllStringFunc(s, func(m string) string {
		sub := regexp.MustCompile(`@range\s+(.*)`).FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		expr := strings.TrimSpace(sub[1])
		return "{{ range " + expr + " }}"
	})

	// Restore in reverse order so nested blocks (e.g. <pre><code>...</code></pre>) get inner placeholders replaced
	for i := len(blocks) - 1; i >= 0; i-- {
		escaped := escapeTemplateLiterals(blocks[i])
		s = strings.Replace(s, fmt.Sprintf(placeholder, i), escaped, 1)
	}
	return s
}

// convertComponents replaces @componentName() ... @end with slot define + template invoke.
func (e *Engine) convertComponents(s string) string {
	compRe := regexp.MustCompile(`@([a-zA-Z][a-zA-Z0-9_]*)\s*\(\s*\)`)
	for {
		loc := compRe.FindStringIndex(s)
		if loc == nil {
			break
		}
		start := loc[0]
		compName := compRe.FindStringSubmatch(s[start:])[1]
		contentStart := loc[1]
		// Find matching @end (count nested @comp() and @end)
		depth := 1
		pos := contentStart
		endPos := -1
		for pos < len(s) {
			rest := s[pos:]
			if strings.HasPrefix(rest, "@end") && (len(rest) == 4 || !isIdentChar(rest[4])) {
				depth--
				if depth == 0 {
					endPos = pos
					break
				}
				pos += 4
				continue
			}
			if idx := compRe.FindStringIndex(rest); idx != nil && idx[0] == 0 {
				depth++
				pos += idx[1]
				continue
			}
			pos++
		}
		if endPos < 0 {
			break
		}
		inner := strings.TrimSpace(s[contentStart:endPos])
		slotName := fmt.Sprintf("slot_%d", e.slotCounter)
		e.slotCounter++
		convertedInner := e.convertNimbusToGo(inner)
		replacement := fmt.Sprintf(`{{ define %q }}%s{{ end }}{{ template "components/%s" (dict "slots" (dict "main" (slot %q .))) }}`, slotName, convertedInner, compName, slotName)
		s = s[:start] + replacement + s[endPos+4:]
	}
	return s
}

func isIdentChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}

// escapeTemplateLiterals escapes {{ and }} so they are output literally by the Go template engine.
func escapeTemplateLiterals(s string) string {
	const rightPlaceholder = "\x00__NIMBUS_RBRACE__\x00"
	s = strings.ReplaceAll(s, "}}", rightPlaceholder)
	s = strings.ReplaceAll(s, "{{", `{{"{{"}}`)
	return strings.ReplaceAll(s, rightPlaceholder, `{{"}}"}}`)
}

// Default engine. By convention new apps use "resources/views" as the root
// (mirroring AdonisJS), but older apps may still use a top-level "views"
// directory. The init logic below prefers "resources/views" when it exists,
// otherwise falls back to "views".
var Default *Engine

var (
	pluginViews   = make(map[string]fs.FS)
	pluginViewsMu sync.RWMutex
)

func init() {
	// Prefer the modern resources/views convention when present.
	if _, err := os.Stat("resources/views"); err == nil {
		Default = New("resources/views", nil)
		return
	}
	// Backwards-compatible fallback for apps that still use views/ at root.
	if _, err := os.Stat("views"); err == nil {
		Default = New("views", nil)
		return
	}
	// If neither exists yet (e.g. during early tooling), default to resources/views.
	Default = New("resources/views", nil)
}

// SetRoot sets the default engine root and clears cache.
func SetRoot(root string) {
	Default = New(root, Default.funcs)
}

// RegisterPluginViews registers an embedded FS for views under the given prefix.
// When rendering "prefix/name", the engine reads "name.nimbus" from the FS.
// Plugins should call this in Register or Boot.
func RegisterPluginViews(prefix string, filesystem fs.FS) {
	pluginViewsMu.Lock()
	defer pluginViewsMu.Unlock()
	pluginViews[prefix] = filesystem
}

func (e *Engine) readView(name string) ([]byte, string, error) {
	// Check plugin views first (e.g. telescope/dashboard -> prefix telescope, file dashboard.nimbus)
	if i := strings.Index(name, "/"); i > 0 {
		prefix := name[:i]
		pluginViewsMu.RLock()
		pfs, ok := pluginViews[prefix]
		pluginViewsMu.RUnlock()
		if ok {
			suffix := name[i+1:]
			path := suffix + ".nimbus"
			body, err := fs.ReadFile(pfs, path)
			if err == nil {
				return body, name, nil
			}
		}
	}
	// Fall back to main root
	path := filepath.Join(e.root, name+".nimbus")
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("view: read %s: %w", path, err)
	}
	return body, name, nil
}

// Render is a shortcut for Default.Render.
func Render(name string, data any) (string, error) {
	if Default == nil {
		return "", fmt.Errorf("view: default engine not set")
	}
	return Default.Render(name, data)
}
