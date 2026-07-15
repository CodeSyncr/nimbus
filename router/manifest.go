package router

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ManifestEntry describes a single registered route for code generation
// (e.g. `nimbus gen:client` → Hive registry).
type ManifestEntry struct {
	Name   string                  `json:"name"`
	Method string                  `json:"method"`
	Path   string                  `json:"path"`
	Params []string                `json:"params,omitempty"`
	Schema map[string]ManifestRule `json:"schema,omitempty"`
}

// ManifestRule is a body-schema field's generated type info.
type ManifestRule struct {
	Type     string `json:"type"`
	Required bool   `json:"required"`
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

		entries = append(entries, ManifestEntry{
			Name:   name,
			Method: rt.Method(),
			Path:   rt.Path(),
			Params: PathParams(rt.Path()),
			Schema: schema,
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
