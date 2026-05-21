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
	"time"
)

// Props represents HTML element attributes and helper APIs.
type Props struct {
	data map[string]any
}

// NewProps creates a new Props instance.
func NewProps(data map[string]any) *Props {
	return &Props{data: data}
}

// Has checks if a prop was provided.
func (p *Props) Has(key string) bool {
	if p == nil || p.data == nil {
		return false
	}
	_, ok := p.data[key]
	return ok
}

// Get retrieves a prop value, with optional default fallback.
func (p *Props) Get(key string, fallback ...any) any {
	if p == nil || p.data == nil {
		if len(fallback) > 0 {
			return fallback[0]
		}
		return nil
	}
	v, ok := p.data[key]
	if !ok && len(fallback) > 0 {
		return fallback[0]
	}
	return v
}

// Only returns a new Props instance with only the specified keys.
func (p *Props) Only(keys []any) *Props {
	newData := make(map[string]any)
	if p != nil && p.data != nil {
		for _, keyVal := range keys {
			if k, ok := keyVal.(string); ok {
				if v, ok := p.data[k]; ok {
					newData[k] = v
				}
			}
		}
	}
	return &Props{data: newData}
}

// Except returns a new Props instance without the specified keys.
func (p *Props) Except(keys []any) *Props {
	newData := make(map[string]any)
	if p != nil && p.data != nil {
		exclude := make(map[string]bool)
		for _, keyVal := range keys {
			if k, ok := keyVal.(string); ok {
				exclude[k] = true
			}
		}
		for k, v := range p.data {
			if !exclude[k] {
				newData[k] = v
			}
		}
	}
	return &Props{data: newData}
}

// Merge merges custom properties with props values.
func (p *Props) Merge(defaults map[string]any) *Props {
	newData := make(map[string]any)
	for k, v := range defaults {
		newData[k] = v
	}
	if p != nil && p.data != nil {
		for k, v := range p.data {
			if k == "class" {
				newData["class"] = mergeClasses(newData["class"], v)
			} else {
				newData[k] = v
			}
		}
	}
	return &Props{data: newData}
}

// MergeIf merges defaults if condition is true.
func (p *Props) MergeIf(cond bool, defaults map[string]any) *Props {
	if cond {
		return p.Merge(defaults)
	}
	newData := make(map[string]any)
	if p != nil && p.data != nil {
		for k, v := range p.data {
			newData[k] = v
		}
	}
	return &Props{data: newData}
}

// MergeUnless merges defaults if condition is false.
func (p *Props) MergeUnless(cond bool, defaults map[string]any) *Props {
	return p.MergeIf(!cond, defaults)
}

// ToAttrs serializes all props into HTML attribute string.
func (p *Props) ToAttrs() template.HTMLAttr {
	if p == nil || p.data == nil {
		return ""
	}
	var parts []string
	for k, v := range p.data {
		if k == "slots" || k == "slots.main" || k == "embed" || k == "content" {
			continue
		}
		switch val := v.(type) {
		case bool:
			if val {
				parts = append(parts, k)
			}
		case string:
			parts = append(parts, fmt.Sprintf("%s=%q", k, val))
		case []any:
			var strParts []string
			for _, item := range val {
				strParts = append(strParts, fmt.Sprint(item))
			}
			parts = append(parts, fmt.Sprintf("%s=%q", k, strings.Join(strParts, " ")))
		case []string:
			parts = append(parts, fmt.Sprintf("%s=%q", k, strings.Join(val, " ")))
		default:
			parts = append(parts, fmt.Sprintf("%s=%q", k, fmt.Sprint(v)))
		}
	}
	return template.HTMLAttr(strings.Join(parts, " "))
}

func mergeClasses(c1, c2 any) any {
	toStrings := func(v any) []string {
		if v == nil {
			return nil
		}
		switch val := v.(type) {
		case string:
			return strings.Fields(val)
		case []string:
			return val
		case []any:
			var res []string
			for _, item := range val {
				if s, ok := item.(string); ok {
					res = append(res, strings.Fields(s)...)
				} else {
					res = append(res, fmt.Sprint(item))
				}
			}
			return res
		}
		return []string{fmt.Sprint(v)}
	}
	s1 := toStrings(c1)
	s2 := toStrings(c2)
	seen := make(map[string]bool)
	var res []string
	for _, s := range append(s1, s2...) {
		if !seen[s] {
			seen[s] = true
			res = append(res, s)
		}
	}
	return strings.Join(res, " ")
}

// Context represents the provide/inject context store.
type Context struct {
	mu    sync.RWMutex
	store map[string]any
}

// NewContext creates a new Provide/Inject Context store.
func NewContext() *Context {
	return &Context{store: make(map[string]any)}
}

// Provide shares state with child component trees.
func (c *Context) Provide(key string, val any) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store[key] = val
	return ""
}

// Inject accesses the state shared by parent components.
func (c *Context) Inject(key string) any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.store[key]
}

// defaultFuncs returns Edge-style template helpers (raw, dump, dict, slot, injectContext, newProps, slice).
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
			return 0
		},
		"slice": func(args ...any) []any {
			return args
		},
		"newProps": func(data any) *Props {
			if m, ok := data.(map[string]any); ok {
				return NewProps(m)
			}
			return NewProps(nil)
		},
		"injectContext": func(data any) *Context {
			if m, ok := data.(map[string]any); ok {
				if ctx, ok := m["$context"].(*Context); ok {
					return ctx
				}
			}
			return NewContext()
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
	merged["renderSlot"] = func(slots any, name string, data any) (template.HTML, error) {
		if slots == nil {
			return "", nil
		}
		var slotTmplName string
		switch sMap := slots.(type) {
		case map[string]any:
			if val, ok := sMap[name].(string); ok {
				slotTmplName = val
			}
		}
		if slotTmplName == "" {
			return "", nil
		}
		if e.slotTemplate == nil {
			return "", fmt.Errorf("view: slot template not ready")
		}
		var b bytes.Buffer
		if err := e.slotTemplate.ExecuteTemplate(&b, slotTmplName, data); err != nil {
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
	return e.renderWithLayouts(name, data, nil)
}

func (e *Engine) renderWithLayouts(name string, data any, seen map[string]bool) (string, error) {
	start := time.Now()

	// Initialize $context store on top-level data context
	var contextObj *Context
	if m, ok := data.(map[string]any); ok {
		if ctxVal, exists := m["$context"]; exists {
			if ctx, ok := ctxVal.(*Context); ok {
				contextObj = ctx
			}
		}
		if contextObj == nil {
			contextObj = NewContext()
			m["$context"] = contextObj
		}
	} else if data == nil {
		contextObj = NewContext()
		data = map[string]any{"$context": contextObj}
	}

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
		if seen == nil {
			seen = make(map[string]bool)
		}
		if seen[layoutName] {
			return "", fmt.Errorf("view: layout cycle detected: %s -> %s", name, layoutName)
		}
		seen[name] = true
		layoutData := map[string]any{}
		if m, _ := data.(map[string]any); m != nil {
			for k, v := range m {
				layoutData[k] = v
			}
		}
		layoutData["embed"] = template.HTML(body)
		layoutData["content"] = template.HTML(body)
		out, err := e.renderWithLayouts(layoutName, layoutData, seen)
		if err == nil && OnRendered != nil {
			OnRendered(name, time.Since(start), data)
		}
		return out, err
	}
	if OnRendered != nil {
		OnRendered(name, time.Since(start), data)
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
	if layoutName := parseLayoutLine(s); layoutName != "" {
		e.layout[name] = layoutName
		s = stripLayoutLine(s)
	}
	e.slotCounter = 0
	converted := e.convertNimbusToGo(s)
	t, err := template.New(name).Funcs(e.funcs).Parse(converted)
	if err != nil {
		return nil, fmt.Errorf("view %q parse: %w", name, err)
	}
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
	err := filepath.WalkDir(componentsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) && path == componentsDir {
				return nil
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".nimbus") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(componentsDir, path)
		if err != nil {
			return err
		}
		compName := strings.TrimSuffix(rel, ".nimbus")
		compName = filepath.ToSlash(compName)
		tmplName := "components/" + compName
		converted := e.convertNimbusToGo(string(body))
		_, err = t.New(tmplName).Parse(converted)
		if err != nil {
			return fmt.Errorf("component %s: %w", compName, err)
		}
		return nil
	})
	return t, err
}

func parseLayoutLine(s string) string {
	s = strings.TrimSpace(s)
	prefix := "@layout("
	if !strings.HasPrefix(s, prefix) {
		return ""
	}
	start := len(prefix)
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
func (e *Engine) convertNimbusToGo(s string) string {
	var slotDefs []string
	converted := e.convertNimbusToGoInternal(s, &slotDefs)

	// Prepend scoped variables so $props and $context are always defined at top scope
	prefix := `{{ $props := index . "$props" }}{{ $context := index . "$context" }}`
	converted = prefix + converted

	if len(slotDefs) > 0 {
		converted = converted + "\n" + strings.Join(slotDefs, "\n")
	}
	return converted
}

func (e *Engine) convertNimbusToGoInternal(s string, slotDefs *[]string) string {
	var blocks []string
	placeholder := " __NIMBUS_RAW_%d__ "
	replaceBlock := func(m string) string {
		blocks = append(blocks, m)
		return fmt.Sprintf(placeholder, len(blocks)-1)
	}
	s = regexp.MustCompile(`(?s)<code[^>]*>.*?</code>`).ReplaceAllStringFunc(s, replaceBlock)
	s = regexp.MustCompile(`(?s)<pre[^>]*>.*?</pre>`).ReplaceAllStringFunc(s, replaceBlock)

	s = e.convertComponents(s, slotDefs)

	s = regexp.MustCompile(`\{\{--.*?--\}\}`).ReplaceAllString(s, "")

	// Convert slots rendering: {{{ slots.main() }}}, {{{ $slots.main }}}, {{ slots.main }} -> {{ renderSlot .slots "main" . }}
	s = regexp.MustCompile(`\{\{\{\s*(\.slots|\$slots|slots)\.([a-zA-Z0-9_]+)(\(\))?\s*\}\}\}`).ReplaceAllStringFunc(s, func(m string) string {
		sub := regexp.MustCompile(`\{\{\{\s*(\.slots|\$slots|slots)\.([a-zA-Z0-9_]+)(\(\))?\s*\}\}\}`).FindStringSubmatch(m)
		slotKey := sub[2]
		return fmt.Sprintf(`{{ renderSlot .slots %q . }}`, slotKey)
	})
	s = regexp.MustCompile(`\{\{\s*(\.slots|\$slots|slots)\.([a-zA-Z0-9_]+)(\(\))?\s*\}\}`).ReplaceAllStringFunc(s, func(m string) string {
		sub := regexp.MustCompile(`\{\{\s*(\.slots|\$slots|slots)\.([a-zA-Z0-9_]+)(\(\))?\s*\}\}`).FindStringSubmatch(m)
		slotKey := sub[2]
		return fmt.Sprintf(`{{ renderSlot .slots %q . }}`, slotKey)
	})

	// Convert const/let declarations: const x = y -> {{ $x := y }}
	s = regexp.MustCompile(`\{\{\s*(const|let)\s+([a-zA-Z_][a-zA-Z0-9_]*)\s*=\s*(.*?)\s*\}\}`).ReplaceAllStringFunc(s, func(m string) string {
		sub := regexp.MustCompile(`\{\{\s*(const|let)\s+([a-zA-Z_][a-zA-Z0-9_]*)\s*=\s*(.*?)\s*\}\}`).FindStringSubmatch(m)
		if len(sub) < 4 {
			return m
		}
		varName := sub[2]
		valExpr := strings.TrimSpace(sub[3])
		valExpr = translateJSMethodCalls(valExpr)
		return "{{ $" + varName + " := " + valExpr + " }}"
	})

	s = regexp.MustCompile(`\{\{\{\s*(.*?)\s*\}\}\}`).ReplaceAllStringFunc(s, func(m string) string {
		sub := regexp.MustCompile(`\{\{\{\s*(.*?)\s*\}\}\}`).FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		expr := strings.TrimSpace(sub[1])
		expr = translateJSMethodCalls(expr)
		if expr != "" && !strings.HasPrefix(expr, ".") && !strings.HasPrefix(expr, "$") && expr[0] != '(' {
			expr = "." + expr
		}
		return "{{ raw " + expr + " }}"
	})

	goKeywords := map[string]bool{"end": true, "else": true, "if": true, "range": true, "with": true, "template": true, "define": true, "block": true}

	s = regexp.MustCompile(`\{\{\s*(.*?)\s*\}\}`).ReplaceAllStringFunc(s, func(m string) string {
		sub := regexp.MustCompile(`\{\{\s*(.*?)\s*\}\}`).FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		expr := strings.TrimSpace(sub[1])
		if expr == "" || expr == "." || strings.HasPrefix(expr, "if ") || strings.HasPrefix(expr, "else") || strings.HasPrefix(expr, "end") || strings.HasPrefix(expr, "range ") || strings.HasPrefix(expr, "with ") || strings.HasPrefix(expr, "template ") || strings.HasPrefix(expr, "define ") || strings.HasPrefix(expr, "$props :=") || strings.HasPrefix(expr, "$context :=") {
			return m
		}
		if strings.HasPrefix(expr, "$") && strings.Contains(expr, ":=") {
			return m
		}
		expr = translateJSMethodCalls(expr)
		if regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`).MatchString(expr) && !goKeywords[expr] {
			expr = "." + expr
		}
		return "{{ " + expr + " }}"
	})

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

	s = regexp.MustCompile(`@include\s*\(\s*['"]([^'"]+)['"]\s*\)`).ReplaceAllStringFunc(s, func(m string) string {
		sub := regexp.MustCompile(`@include\s*\(\s*['"]([^'"]+)['"]\s*\)`).FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		viewName := sub[1]
		return `{{ include "` + viewName + `" . }}`
	})

	s = regexp.MustCompile(`@if\s*\((.*?)\)`).ReplaceAllStringFunc(s, func(m string) string {
		sub := regexp.MustCompile(`@if\s*\((.*?)\)`).FindStringSubmatch(m)
		condition := strings.TrimSpace(sub[1])
		condition = translateJSMethodCalls(condition)
		condition = strings.ReplaceAll(condition, "!", "not ")
		return "{{ if " + condition + " }}"
	})

	s = regexp.MustCompile(`@else\s+if\s*\((.*?)\)`).ReplaceAllStringFunc(s, func(m string) string {
		sub := regexp.MustCompile(`@else\s+if\s*\((.*?)\)`).FindStringSubmatch(m)
		condition := strings.TrimSpace(sub[1])
		condition = translateJSMethodCalls(condition)
		condition = strings.ReplaceAll(condition, "!", "not ")
		return "{{ else if " + condition + " }}"
	})
	s = regexp.MustCompile(`@elseif\s*\((.*?)\)`).ReplaceAllStringFunc(s, func(m string) string {
		sub := regexp.MustCompile(`@elseif\s*\((.*?)\)`).FindStringSubmatch(m)
		condition := strings.TrimSpace(sub[1])
		condition = translateJSMethodCalls(condition)
		condition = strings.ReplaceAll(condition, "!", "not ")
		return "{{ else if " + condition + " }}"
	})

	s = strings.ReplaceAll(s, "@else", "{{ else }}")
	s = regexp.MustCompile(`@(endeach|endif|endrange|end)`).ReplaceAllString(s, "{{ end }}")

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
		collection := inner
		if !strings.HasPrefix(collection, ".") {
			collection = "." + collection
		}
		return "{{ range " + collection + " }}"
	})

	s = regexp.MustCompile(`@range\s+(.*)`).ReplaceAllStringFunc(s, func(m string) string {
		sub := regexp.MustCompile(`@range\s+(.*)`).FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		expr := strings.TrimSpace(sub[1])
		return "{{ range " + expr + " }}"
	})

	for i := len(blocks) - 1; i >= 0; i-- {
		escaped := escapeTemplateLiterals(blocks[i])
		s = strings.Replace(s, fmt.Sprintf(placeholder, i), escaped, 1)
	}
	return s
}

// convertComponents replaces component calls with slot definition and template execution.
func (e *Engine) convertComponents(s string, slotDefs *[]string) string {
	compRe := regexp.MustCompile(`@(!?)([a-zA-Z][a-zA-Z0-9_.]*)`)
	keywords := map[string]bool{
		"if": true, "else": true, "elseif": true, "endif": true,
		"each": true, "endeach": true, "layout": true, "dump": true,
		"include": true, "range": true, "endrange": true, "slot": true,
		"end": true,
	}

	for {
		loc := compRe.FindStringIndex(s)
		if loc == nil {
			break
		}
		start := loc[0]
		matchStr := s[start:loc[1]]
		sub := compRe.FindStringSubmatch(matchStr)
		isSelfClosing := sub[1] == "!"
		compName := sub[2]

		if keywords[compName] {
			s = s[:start] + "__NIMBUS_KEYWORD_" + compName + s[start+len(matchStr):]
			continue
		}

		argsStr := ""
		argsEnd := start + len(matchStr)
		if argsEnd < len(s) && s[argsEnd] == '(' {
			var err error
			argsStr, argsEnd, err = parseComponentArgs(s, argsEnd)
			if err != nil {
				s = s[:start] + "__NIMBUS_BAD_COMP_" + compName + s[start+len(matchStr):]
				continue
			}
		}

		// Convert dots to slashes in component names to support nested component folders
		compPath := strings.ReplaceAll(compName, ".", "/")

		dictParts := []string{`"slots" (dict "main" "%s")`, `"$context" (injectContext .)`}
		argsStr = strings.TrimSpace(argsStr)
		if strings.HasPrefix(argsStr, "{") && strings.HasSuffix(argsStr, "}") {
			argsStr = strings.TrimSpace(argsStr[1 : len(argsStr)-1])
		}
		var rawProps []string
		if argsStr != "" {
			pairs := splitPairs(argsStr)
			for _, pair := range pairs {
				if k, v, ok := splitKeyPair(pair); ok {
					k = strings.Trim(k, `'"`)
					valExpr := convertValExpr(v)
					dictParts = append(dictParts, fmt.Sprintf("%q %s", k, valExpr))
					rawProps = append(rawProps, fmt.Sprintf("%q %s", k, valExpr))
				}
			}
		}

		slotName := fmt.Sprintf("slot_%d", e.slotCounter)
		e.slotCounter++

		var dictCall string
		baseParts := fmt.Sprintf(`%s "$context" (injectContext .)`, fmt.Sprintf(dictParts[0], slotName))
		if len(rawProps) > 0 {
			dictCall = fmt.Sprintf(`(dict %s "$props" (newProps (dict %s)) %s)`, baseParts, strings.Join(rawProps, " "), strings.Join(rawProps, " "))
		} else {
			dictCall = fmt.Sprintf(`(dict %s "$props" (newProps nil))`, baseParts)
		}

		if isSelfClosing {
			*slotDefs = append(*slotDefs, fmt.Sprintf(`{{ define %q }}{{ $props := index . "$props" }}{{ $context := index . "$context" }}{{ end }}`, slotName))
			replacement := fmt.Sprintf(`{{ template "components/%s" %s }}`, compPath, dictCall)
			s = s[:start] + replacement + s[argsEnd:]
		} else {
			depth := 1
			pos := argsEnd
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
				if mLoc := compRe.FindStringIndex(rest); mLoc != nil && mLoc[0] == 0 {
					mMatch := rest[mLoc[0]:mLoc[1]]
					mSub := compRe.FindStringSubmatch(mMatch)
					mName := mSub[2]
					if !keywords[mName] {
						mIsSelf := mSub[1] == "!"
						if !mIsSelf {
							depth++
						}
					}
					pos += mLoc[1]
					continue
				}
				pos++
			}
			if endPos < 0 {
				s = s[:start] + "__NIMBUS_UNCLOSED_COMP_" + compName + s[start+len(matchStr):]
				continue
			}
			inner := s[argsEnd:endPos]
			convertedInner := e.convertNimbusToGoInternal(inner, slotDefs)
			*slotDefs = append(*slotDefs, fmt.Sprintf(`{{ define %q }}{{ $props := index . "$props" }}{{ $context := index . "$context" }}%s{{ end }}`, slotName, convertedInner))
			replacement := fmt.Sprintf(`{{ template "components/%s" %s }}`, compPath, dictCall)
			s = s[:start] + replacement + s[endPos+4:]
		}
	}

	s = strings.ReplaceAll(s, "__NIMBUS_KEYWORD_", "@")
	s = strings.ReplaceAll(s, "__NIMBUS_BAD_COMP_", "@")
	s = strings.ReplaceAll(s, "__NIMBUS_UNCLOSED_COMP_", "@")
	return s
}

func parseComponentArgs(s string, startPos int) (argsStr string, endPos int, err error) {
	if startPos >= len(s) || s[startPos] != '(' {
		return "", startPos, fmt.Errorf("expected '('")
	}
	parenDepth := 0
	braceDepth := 0
	inSingleQuote := false
	inDoubleQuote := false
	inBacktick := false
	var i int
	for i = startPos; i < len(s); i++ {
		char := s[i]
		if inSingleQuote {
			if char == '\'' && s[i-1] != '\\' {
				inSingleQuote = false
			}
			continue
		}
		if inDoubleQuote {
			if char == '"' && s[i-1] != '\\' {
				inDoubleQuote = false
			}
			continue
		}
		if inBacktick {
			if char == '`' && s[i-1] != '\\' {
				inBacktick = false
			}
			continue
		}

		switch char {
		case '\'':
			inSingleQuote = true
		case '"':
			inDoubleQuote = true
		case '`':
			inBacktick = true
		case '(':
			parenDepth++
		case ')':
			parenDepth--
			if parenDepth == 0 {
				return s[startPos+1 : i], i + 1, nil
			}
		case '{':
			braceDepth++
		case '}':
			braceDepth--
		}
	}
	return "", startPos, fmt.Errorf("unmatched '('")
}

func splitPairs(s string) []string {
	var parts []string
	parenDepth := 0
	braceDepth := 0
	bracketDepth := 0
	inSingleQuote := false
	inDoubleQuote := false
	inBacktick := false
	start := 0
	for i := 0; i < len(s); i++ {
		char := s[i]
		if inSingleQuote {
			if char == '\'' && s[i-1] != '\\' {
				inSingleQuote = false
			}
			continue
		}
		if inDoubleQuote {
			if char == '"' && s[i-1] != '\\' {
				inDoubleQuote = false
			}
			continue
		}
		if inBacktick {
			if char == '`' && s[i-1] != '\\' {
				inBacktick = false
			}
			continue
		}

		switch char {
		case '\'':
			inSingleQuote = true
		case '"':
			inDoubleQuote = true
		case '`':
			inBacktick = true
		case '(':
			parenDepth++
		case ')':
			parenDepth--
		case '{':
			braceDepth++
		case '}':
			braceDepth--
		case '[':
			bracketDepth++
		case ']':
			bracketDepth--
		case ',':
			if parenDepth == 0 && braceDepth == 0 && bracketDepth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func splitKeyPair(s string) (key, val string, ok bool) {
	parenDepth := 0
	braceDepth := 0
	bracketDepth := 0
	inSingleQuote := false
	inDoubleQuote := false
	inBacktick := false
	for i := 0; i < len(s); i++ {
		char := s[i]
		if inSingleQuote {
			if char == '\'' && s[i-1] != '\\' {
				inSingleQuote = false
			}
			continue
		}
		if inDoubleQuote {
			if char == '"' && s[i-1] != '\\' {
				inDoubleQuote = false
			}
			continue
		}
		if inBacktick {
			if char == '`' && s[i-1] != '\\' {
				inBacktick = false
			}
			continue
		}

		switch char {
		case '\'':
			inSingleQuote = true
		case '"':
			inDoubleQuote = true
		case '`':
			inBacktick = true
		case '(':
			parenDepth++
		case ')':
			parenDepth--
		case '{':
			braceDepth++
		case '}':
			braceDepth--
		case '[':
			bracketDepth++
		case ']':
			bracketDepth--
		case ':', '=':
			if parenDepth == 0 && braceDepth == 0 && bracketDepth == 0 {
				return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:]), true
			}
		}
	}
	return "", "", false
}

func translateJSMethodCalls(expr string) string {
	// Translate context calls:
	// $context.provide(k, v) -> ($context.Provide k v)
	expr = regexp.MustCompile(`\$context\.(provide|Provide)\s*\(\s*(.*?)\s*,\s*(.*?)\s*\)`).
		ReplaceAllStringFunc(expr, func(m string) string {
			sub := regexp.MustCompile(`\$context\.(provide|Provide)\s*\(\s*(.*?)\s*,\s*(.*?)\s*\)`).FindStringSubmatch(m)
			k := convertValExpr(sub[2])
			v := convertValExpr(sub[3])
			return fmt.Sprintf(`($context.Provide %s %s)`, k, v)
		})
	// $context.inject(k) -> ($context.Inject k)
	expr = regexp.MustCompile(`\$context\.(inject|Inject)\s*\(\s*(.*?)\s*\)`).
		ReplaceAllStringFunc(expr, func(m string) string {
			sub := regexp.MustCompile(`\$context\.(inject|Inject)\s*\(\s*(.*?)\s*\)`).FindStringSubmatch(m)
			k := convertValExpr(sub[2])
			return fmt.Sprintf(`($context.Inject %s)`, k)
		})

	// $props.toAttrs() -> ($props.ToAttrs)
	expr = regexp.MustCompile(`\$props\.(toAttrs|ToAttrs)\s*\(\s*\)`).ReplaceAllString(expr, `($props.ToAttrs)`)

	// Translate props API method chaining
	methods := map[string]string{
		"mergeUnless": "MergeUnless", "MergeUnless": "MergeUnless",
		"mergeIf": "MergeIf", "MergeIf": "MergeIf",
		"merge": "Merge", "Merge": "Merge",
		"except": "Except", "Except": "Except",
		"only": "Only", "Only": "Only",
		"has": "Has", "Has": "Has",
		"get": "Get", "Get": "Get",
		"toAttrs": "ToAttrs", "ToAttrs": "ToAttrs",
	}

	for {
		found := false
		for method, goMethod := range methods {
			dotMethod := "." + method + "("
			idx := strings.Index(expr, dotMethod)
			if idx < 0 {
				continue
			}

			argsStart := idx + len(method) + 1
			argsStr, argsEnd, err := parseComponentArgs(expr, argsStart)
			if err != nil {
				continue
			}

			objStart := 0
			parenCount := 0
			for i := idx - 1; i >= 0; i-- {
				char := expr[i]
				if char == ')' {
					parenCount++
				} else if char == '(' {
					parenCount--
					if parenCount < 0 {
						objStart = i + 1
						break
					}
				} else if parenCount == 0 && (char == ' ' || char == ',' || char == '{' || char == '}') {
					objStart = i + 1
					break
				}
			}
			obj := strings.TrimSpace(expr[objStart:idx])

			var cleanedArgs []string
			if strings.TrimSpace(argsStr) != "" {
				argsList := splitPairs(argsStr)
				for _, arg := range argsList {
					cleanedArgs = append(cleanedArgs, convertValExpr(arg))
				}
			}

			var replacement string
			if len(cleanedArgs) > 0 {
				replacement = fmt.Sprintf(`((%s).%s %s)`, obj, goMethod, strings.Join(cleanedArgs, " "))
			} else {
				replacement = fmt.Sprintf(`((%s).%s)`, obj, goMethod)
			}

			expr = expr[:objStart] + replacement + expr[argsEnd:]
			found = true
			break
		}
		if !found {
			break
		}
	}
	return expr
}

func convertValExpr(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return `""`
	}
	if strings.HasPrefix(v, "'") && strings.HasSuffix(v, "'") {
		return `"` + strings.ReplaceAll(v[1:len(v)-1], `"`, `\"`) + `"`
	}
	if (strings.HasPrefix(v, `"`) && strings.HasSuffix(v, `"`)) || v == "true" || v == "false" {
		return v
	}
	if regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`).MatchString(v) {
		return v
	}
	if strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]") {
		elements := splitPairs(v[1 : len(v)-1])
		var convertedElems []string
		for _, el := range elements {
			convertedElems = append(convertedElems, convertValExpr(el))
		}
		return "(slice " + strings.Join(convertedElems, " ") + ")"
	}
	if strings.HasPrefix(v, ".") || strings.HasPrefix(v, "$") {
		return v
	}
	if strings.Contains(v, " ") {
		return "(" + v + ")"
	}
	return "." + v
}

func isIdentChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}

func escapeTemplateLiterals(s string) string {
	var b strings.Builder
	b.Grow(len(s) * 2)
	for i := 0; i < len(s); i++ {
		if i+1 < len(s) && s[i] == '{' && s[i+1] == '{' {
			b.WriteString(`{{"{{"}}`)
			i++
		} else if i+1 < len(s) && s[i] == '}' && s[i+1] == '}' {
			b.WriteString(`{{"}}"}}`)
			i++
		} else if s[i] == '{' {
			b.WriteString(`{{ "{" }}`)
		} else if s[i] == '}' {
			b.WriteString(`{{ "}" }}`)
		} else {
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// Default engine.
var Default *Engine

var (
	pluginViews   = make(map[string]fs.FS)
	pluginViewsMu sync.RWMutex
)

var (
	defaultFuncsMu sync.RWMutex
)

// OnRendered is called after a successful template render.
var OnRendered func(name string, duration time.Duration, data any)

func init() {
	if _, err := os.Stat("resources/views"); err == nil {
		Default = New("resources/views", nil)
		return
	}
	if _, err := os.Stat("views"); err == nil {
		Default = New("views", nil)
		return
	}
	Default = New("resources/views", nil)
}

// SetRoot sets the default engine root and clears cache.
func SetRoot(root string) {
	Default = New(root, Default.funcs)
}

// RegisterFunc registers a global template function.
func RegisterFunc(name string, fn any) {
	if name == "" || fn == nil {
		return
	}
	defaultFuncsMu.Lock()
	defer defaultFuncsMu.Unlock()
	if Default == nil {
		Default = New("resources/views", nil)
	}
	funcs := template.FuncMap{}
	for k, v := range Default.funcs {
		funcs[k] = v
	}
	funcs[name] = fn
	Default = New(Default.root, funcs)
}

// RegisterFuncs registers multiple global template functions.
func RegisterFuncs(funcs template.FuncMap) {
	if len(funcs) == 0 {
		return
	}
	defaultFuncsMu.Lock()
	defer defaultFuncsMu.Unlock()
	if Default == nil {
		Default = New("resources/views", nil)
	}
	merged := template.FuncMap{}
	for k, v := range Default.funcs {
		merged[k] = v
	}
	for k, v := range funcs {
		if k != "" && v != nil {
			merged[k] = v
		}
	}
	Default = New(Default.root, merged)
}

// RegisterPluginViews registers an embedded FS for views.
func RegisterPluginViews(prefix string, filesystem fs.FS) {
	pluginViewsMu.Lock()
	pluginViews[prefix] = filesystem
	pluginViewsMu.Unlock()
}

func (e *Engine) readView(name string) ([]byte, string, error) {
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
