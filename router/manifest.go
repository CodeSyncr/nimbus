package router

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

// ManifestEntry describes a single registered route for code generation
// (e.g. `nimbus gen:client` → Hive registry).
type ManifestEntry struct {
	Name     string                  `json:"name"`
	Method   string                  `json:"method"`
	Path     string                  `json:"path"`
	Params   []string                `json:"params,omitempty"`
	Schema   map[string]ManifestRule `json:"schema,omitempty"`
	Response *ManifestType           `json:"response,omitempty"`
}

// ManifestType describes the structural type representation of a Go type.
type ManifestType struct {
	Kind       string                  `json:"kind"`                 // "primitive", "struct", "array", "map", "any"
	Type       string                  `json:"type,omitempty"`       // primitive type name (string, number, boolean)
	Fields     map[string]ManifestType `json:"fields,omitempty"`     // struct fields
	Elem       *ManifestType           `json:"elem,omitempty"`       // array element type or map/slice value type
	IsNullable bool                    `json:"is_nullable,omitempty"` // nullable (pointers, slice/map, or omitempty)
}

// ManifestRule is a body-schema field's generated type info.
type ManifestRule struct {
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

// reflectTypeToManifest converts a reflect.Type to a ManifestType.
func reflectTypeToManifest(t reflect.Type, visited map[reflect.Type]bool) *ManifestType {
	if t == nil {
		return &ManifestType{Kind: "any"}
	}

	// Dereference pointers
	isNullable := false
	for t.Kind() == reflect.Ptr {
		isNullable = true
		t = t.Elem()
	}

	// Avoid circular recursion
	if visited[t] {
		return &ManifestType{Kind: "any", IsNullable: isNullable}
	}

	// Special case: time.Time
	if t.PkgPath() == "time" && t.Name() == "Time" {
		return &ManifestType{Kind: "primitive", Type: "string", IsNullable: isNullable}
	}

	switch t.Kind() {
	case reflect.Struct:
		visited[t] = true
		defer func() { visited[t] = false }()

		fields := make(map[string]ManifestType)
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			// Skip unexported fields unless anonymous
			if f.PkgPath != "" && !f.Anonymous {
				continue
			}

			tag := f.Tag.Get("json")
			if tag == "-" {
				continue
			}

			name := f.Name
			omitempty := false
			if tag != "" {
				parts := strings.Split(tag, ",")
				if parts[0] != "" {
					name = parts[0]
				}
				for _, p := range parts[1:] {
					if p == "omitempty" {
						omitempty = true
					}
				}
			}

			if f.Anonymous && tag == "" {
				elem := f.Type
				for elem.Kind() == reflect.Ptr {
					elem = elem.Elem()
				}
				if elem.Kind() == reflect.Struct {
					sub := reflectTypeToManifest(elem, visited)
					if sub != nil && sub.Kind == "struct" {
						for k, v := range sub.Fields {
							fields[k] = v
						}
					}
				}
				continue
			}

			fieldVal := reflectTypeToManifest(f.Type, visited)
			if fieldVal != nil {
				if omitempty {
					fieldVal.IsNullable = true
				}
				fields[name] = *fieldVal
			}
		}
		return &ManifestType{
			Kind:       "struct",
			Fields:     fields,
			IsNullable: isNullable,
		}

	case reflect.Slice, reflect.Array:
		elem := reflectTypeToManifest(t.Elem(), visited)
		return &ManifestType{
			Kind:       "array",
			Elem:       elem,
			IsNullable: isNullable,
		}

	case reflect.Map:
		val := reflectTypeToManifest(t.Elem(), visited)
		return &ManifestType{
			Kind:       "map",
			Elem:       val,
			IsNullable: isNullable,
		}

	case reflect.String:
		return &ManifestType{Kind: "primitive", Type: "string", IsNullable: isNullable}

	case reflect.Bool:
		return &ManifestType{Kind: "primitive", Type: "boolean", IsNullable: isNullable}

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return &ManifestType{Kind: "primitive", Type: "number", IsNullable: isNullable}

	default:
		return &ManifestType{Kind: "any", IsNullable: isNullable}
	}
}

// ManifestFileName is the manifest written into the client output directory.
const ManifestFileName = ".route-manifest.json"

// paramPattern matches both supported param syntaxes: ":id" and "{id}".
var paramPattern = regexp.MustCompile(`:([a-zA-Z_][a-zA-Z0-9_]*)|\{([a-zA-Z_][a-zA-Z0-9_]*)\}`)

// PathParams returns the parameter names in a route path, supporting both
// ":id" and "{id}" syntaxes.
func PathParams(path string) []string {
	matches := paramPattern.FindAllStringSubmatch(path, -1)
	params := make([]string, 0, len(matches))
	for _, m := range matches {
		if m[1] != "" {
			params = append(params, m[1])
		} else if m[2] != "" {
			params = append(params, m[2])
		}
	}
	return params
}

// isParamSegment reports whether a single path segment is a parameter.
func isParamSegment(seg string) bool {
	return strings.HasPrefix(seg, ":") || (strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}"))
}

// sanitizeSegment reduces a static path segment to a safe name part.
func sanitizeSegment(seg string) string {
	var b strings.Builder
	for _, r := range seg {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		case r == '-':
			b.WriteRune('_')
		}
	}
	return b.String()
}

// DeriveName builds a stable, readable route name from a method and path for
// routes that were not explicitly named with .As(). It follows REST
// conventions:
//
//	GET    /api/posts        → api.posts.index
//	POST   /api/posts        → api.posts.store
//	GET    /api/posts/{id}   → api.posts.show
//	PUT    /api/posts/{id}   → api.posts.update
//	DELETE /api/posts/{id}   → api.posts.destroy
//	POST   /api/posts/{id}/publish → api.posts.publish.store
//
// Explicit .As("...") names always take precedence over this.
func DeriveName(method, path string) string {
	rawSegs := strings.Split(strings.Trim(path, "/"), "/")

	var static []string
	endsWithParam := false
	for i, seg := range rawSegs {
		if seg == "" || seg == "*" {
			continue
		}
		if isParamSegment(seg) {
			if i == len(rawSegs)-1 {
				endsWithParam = true
			}
			continue
		}
		if s := sanitizeSegment(seg); s != "" {
			static = append(static, s)
		}
	}

	var suffix string
	switch strings.ToUpper(method) {
	case "GET", "HEAD":
		if endsWithParam {
			suffix = "show"
		} else {
			suffix = "index"
		}
	case "POST":
		suffix = "store"
	case "PUT", "PATCH":
		suffix = "update"
	case "DELETE":
		suffix = "destroy"
	default:
		suffix = strings.ToLower(method)
	}

	if len(static) == 0 {
		return suffix
	}
	return strings.Join(static, ".") + "." + suffix
}

// Manifest builds the route manifest for a router. Explicitly named routes keep
// their name; unnamed routes get a derived one. Names are guaranteed unique —
// on a collision the HTTP method is appended, then a numeric suffix.
func Manifest(r *Router) []ManifestEntry {
	routes := r.Routes()
	entries := make([]ManifestEntry, 0, len(routes))
	used := make(map[string]bool, len(routes))

	// Reserve explicit names first so derived names never steal them.
	for _, rt := range routes {
		if n := rt.Name(); n != "" {
			used[n] = true
		}
	}

	for _, rt := range routes {
		name := rt.Name()
		if name == "" {
			name = uniqueName(DeriveName(rt.Method(), rt.Path()), rt.Method(), used)
			used[name] = true
		}

		var schema map[string]ManifestRule
		if rt.Meta.BodySchema != nil {
			type tsTyper interface {
				TypeScriptType() string
				IsRequired() bool
			}
			schema = make(map[string]ManifestRule, len(rt.Meta.BodySchema))
			for k, rule := range rt.Meta.BodySchema {
				info := ManifestRule{Type: "any"}
				if t, ok := rule.(tsTyper); ok {
					info = ManifestRule{Type: t.TypeScriptType(), Required: t.IsRequired()}
				}
				schema[k] = info
			}
		}

		var respType *ManifestType
		if rt.Meta.Response != nil {
			respType = reflectTypeToManifest(reflect.TypeOf(rt.Meta.Response), make(map[reflect.Type]bool))
		}

		entries = append(entries, ManifestEntry{
			Name:     name,
			Method:   rt.Method(),
			Path:     rt.Path(),
			Params:   PathParams(rt.Path()),
			Schema:   schema,
			Response: respType,
		})
	}
	return entries
}

// uniqueName resolves collisions deterministically.
func uniqueName(base, method string, used map[string]bool) string {
	if !used[base] {
		return base
	}
	withMethod := base + "." + strings.ToLower(method)
	if !used[withMethod] {
		return withMethod
	}
	for i := 2; ; i++ {
		candidate := withMethod + strconv.Itoa(i)
		if !used[candidate] {
			return candidate
		}
	}
}

// WriteManifest writes the route manifest JSON into outDir. It is called by the
// app at startup when NIMBUS_DUMP_ROUTES=1, and consumed by `nimbus gen:client`.
func WriteManifest(r *Router, outDir string) error {
	data, err := json.MarshalIndent(Manifest(r), "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, ManifestFileName), data, 0o644)
}
