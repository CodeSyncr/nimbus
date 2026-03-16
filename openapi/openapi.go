// Package openapi provides full OpenAPI 3.0 specification generation from
// Nimbus router routes. It introspects route metadata, reflects on Go types
// to build JSON Schema, and produces a complete OpenAPI document.
package openapi

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/CodeSyncr/nimbus/router"
)

// ---------------------------------------------------------------------------
// OpenAPI 3.0 Spec Types
// ---------------------------------------------------------------------------

// Spec is the root OpenAPI 3.0 document.
type Spec struct {
	OpenAPI    string                `json:"openapi"`
	Info       Info                  `json:"info"`
	Servers    []Server              `json:"servers,omitempty"`
	Paths      map[string]*PathItem  `json:"paths"`
	Components *Components           `json:"components,omitempty"`
	Tags       []Tag                 `json:"tags,omitempty"`
	Security   []map[string][]string `json:"security,omitempty"`
}

// Info provides metadata about the API.
type Info struct {
	Title          string   `json:"title"`
	Description    string   `json:"description,omitempty"`
	Version        string   `json:"version"`
	TermsOfService string   `json:"termsOfService,omitempty"`
	Contact        *Contact `json:"contact,omitempty"`
	License        *License `json:"license,omitempty"`
}

// Contact information for the API.
type Contact struct {
	Name  string `json:"name,omitempty"`
	URL   string `json:"url,omitempty"`
	Email string `json:"email,omitempty"`
}

// License information for the API.
type License struct {
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}

// Server represents a server.
type Server struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

// Tag describes a tag for API documentation.
type Tag struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// PathItem holds operations on a single path.
type PathItem struct {
	Get     *Operation `json:"get,omitempty"`
	Post    *Operation `json:"post,omitempty"`
	Put     *Operation `json:"put,omitempty"`
	Patch   *Operation `json:"patch,omitempty"`
	Delete  *Operation `json:"delete,omitempty"`
	Head    *Operation `json:"head,omitempty"`
	Options *Operation `json:"options,omitempty"`
}

// Operation describes a single API operation on a path.
type Operation struct {
	OperationID string                `json:"operationId,omitempty"`
	Summary     string                `json:"summary,omitempty"`
	Description string                `json:"description,omitempty"`
	Tags        []string              `json:"tags,omitempty"`
	Parameters  []Parameter           `json:"parameters,omitempty"`
	RequestBody *RequestBody          `json:"requestBody,omitempty"`
	Responses   map[string]*Response  `json:"responses"`
	Security    []map[string][]string `json:"security,omitempty"`
	Deprecated  bool                  `json:"deprecated,omitempty"`
}

// Parameter describes a single operation parameter.
type Parameter struct {
	Name        string  `json:"name"`
	In          string  `json:"in"` // query, header, path, cookie
	Description string  `json:"description,omitempty"`
	Required    bool    `json:"required,omitempty"`
	Schema      *Schema `json:"schema,omitempty"`
}

// RequestBody describes a request body.
type RequestBody struct {
	Description string               `json:"description,omitempty"`
	Required    bool                 `json:"required,omitempty"`
	Content     map[string]MediaType `json:"content"`
}

// Response describes a single response from an API Operation.
type Response struct {
	Description string               `json:"description"`
	Content     map[string]MediaType `json:"content,omitempty"`
}

// MediaType provides schema and examples for the media type.
type MediaType struct {
	Schema *Schema `json:"schema,omitempty"`
}

// Schema represents a JSON Schema object.
type Schema struct {
	Type        string             `json:"type,omitempty"`
	Format      string             `json:"format,omitempty"`
	Description string             `json:"description,omitempty"`
	Properties  map[string]*Schema `json:"properties,omitempty"`
	Items       *Schema            `json:"items,omitempty"`
	Required    []string           `json:"required,omitempty"`
	Ref         string             `json:"$ref,omitempty"`
	Enum        []any              `json:"enum,omitempty"`
	Example     any                `json:"example,omitempty"`
	Nullable    bool               `json:"nullable,omitempty"`
	OneOf       []*Schema          `json:"oneOf,omitempty"`
	AllOf       []*Schema          `json:"allOf,omitempty"`
	AnyOf       []*Schema          `json:"anyOf,omitempty"`
	Default     any                `json:"default,omitempty"`
	Minimum     *float64           `json:"minimum,omitempty"`
	Maximum     *float64           `json:"maximum,omitempty"`
	MinLength   *int               `json:"minLength,omitempty"`
	MaxLength   *int               `json:"maxLength,omitempty"`
	Pattern     string             `json:"pattern,omitempty"`
}

// Components holds reusable objects.
type Components struct {
	Schemas         map[string]*Schema         `json:"schemas,omitempty"`
	SecuritySchemes map[string]*SecurityScheme `json:"securitySchemes,omitempty"`
}

// SecurityScheme defines a security scheme.
type SecurityScheme struct {
	Type         string     `json:"type"` // apiKey, http, oauth2, openIdConnect
	Description  string     `json:"description,omitempty"`
	Name         string     `json:"name,omitempty"` // for apiKey
	In           string     `json:"in,omitempty"`   // header, query, cookie
	Scheme       string     `json:"scheme,omitempty"`
	BearerFormat string     `json:"bearerFormat,omitempty"`
	Flows        *OAuthFlow `json:"flows,omitempty"`
}

// OAuthFlow describes OAuth2 flow configuration.
type OAuthFlow struct {
	Implicit          *FlowConfig `json:"implicit,omitempty"`
	Password          *FlowConfig `json:"password,omitempty"`
	ClientCredentials *FlowConfig `json:"clientCredentials,omitempty"`
	AuthorizationCode *FlowConfig `json:"authorizationCode,omitempty"`
}

// FlowConfig holds OAuth2 flow details.
type FlowConfig struct {
	AuthorizationURL string            `json:"authorizationUrl,omitempty"`
	TokenURL         string            `json:"tokenUrl,omitempty"`
	RefreshURL       string            `json:"refreshUrl,omitempty"`
	Scopes           map[string]string `json:"scopes"`
}

// ---------------------------------------------------------------------------
// Generator
// ---------------------------------------------------------------------------

// GeneratorConfig configures the OpenAPI spec generator.
type GeneratorConfig struct {
	Title       string
	Description string
	Version     string
	Servers     []Server
	Contact     *Contact
	License     *License

	// SecuritySchemes to include in components.
	SecuritySchemes map[string]*SecurityScheme

	// GlobalSecurity applied to all operations by default.
	GlobalSecurity []map[string][]string

	// ExcludePatterns defines path prefixes to skip (e.g. "/_").
	ExcludePatterns []string

	// TagDescriptions allows adding descriptions to tags.
	TagDescriptions map[string]string

	// BasePath prefix stripped from paths.
	BasePath string
}

// Generator builds an OpenAPI 3.0 spec from registered routes.
type Generator struct {
	config  GeneratorConfig
	schemas map[string]*Schema
	seen    map[reflect.Type]string
}

// NewGenerator creates a new OpenAPI spec generator.
func NewGenerator(cfg GeneratorConfig) *Generator {
	if cfg.Version == "" {
		cfg.Version = "1.0.0"
	}
	if cfg.Title == "" {
		cfg.Title = "Nimbus API"
	}
	return &Generator{
		config:  cfg,
		schemas: make(map[string]*Schema),
		seen:    make(map[reflect.Type]string),
	}
}

// Generate produces a complete OpenAPI 3.0 Spec from the given routes.
func (g *Generator) Generate(routes []*router.Route) *Spec {
	spec := &Spec{
		OpenAPI: "3.0.3",
		Info: Info{
			Title:       g.config.Title,
			Description: g.config.Description,
			Version:     g.config.Version,
			Contact:     g.config.Contact,
			License:     g.config.License,
		},
		Servers:  g.config.Servers,
		Paths:    make(map[string]*PathItem),
		Security: g.config.GlobalSecurity,
	}

	tagSet := make(map[string]bool)

	for _, rt := range routes {
		path := rt.Path()
		method := strings.ToLower(rt.Method())

		// Check exclusion patterns.
		excluded := false
		for _, p := range g.config.ExcludePatterns {
			if strings.HasPrefix(path, p) {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}

		// Strip base path.
		if g.config.BasePath != "" {
			path = strings.TrimPrefix(path, g.config.BasePath)
		}

		// Convert :param to {param} for OpenAPI.
		path = convertPathParams(path)

		op := g.buildOperation(rt, method)

		// Track tags.
		for _, t := range op.Tags {
			tagSet[t] = true
		}

		// Get or create path item.
		pi, ok := spec.Paths[path]
		if !ok {
			pi = &PathItem{}
			spec.Paths[path] = pi
		}

		switch method {
		case "get":
			pi.Get = op
		case "post":
			pi.Post = op
		case "put":
			pi.Put = op
		case "patch":
			pi.Patch = op
		case "delete":
			pi.Delete = op
		case "head":
			pi.Head = op
		case "options":
			pi.Options = op
		}
	}

	// Build tags.
	for tag := range tagSet {
		t := Tag{Name: tag}
		if desc, ok := g.config.TagDescriptions[tag]; ok {
			t.Description = desc
		}
		spec.Tags = append(spec.Tags, t)
	}

	// Build components.
	if len(g.schemas) > 0 || len(g.config.SecuritySchemes) > 0 {
		spec.Components = &Components{}
		if len(g.schemas) > 0 {
			spec.Components.Schemas = g.schemas
		}
		if len(g.config.SecuritySchemes) > 0 {
			spec.Components.SecuritySchemes = g.config.SecuritySchemes
		}
	}

	return spec
}

// JSON returns the spec as indented JSON bytes.
func (g *Generator) JSON(routes []*router.Route) ([]byte, error) {
	spec := g.Generate(routes)
	return json.MarshalIndent(spec, "", "  ")
}

// buildOperation constructs an Operation from route metadata.
func (g *Generator) buildOperation(rt *router.Route, method string) *Operation {
	meta := rt.Meta

	op := &Operation{
		Summary:     meta.Summary,
		Description: meta.Description,
		Tags:        meta.Tags,
		Deprecated:  meta.Deprecated,
		Responses:   make(map[string]*Response),
	}

	// Operation ID from route name or path+method.
	if rt.Name() != "" {
		op.OperationID = rt.Name()
	} else {
		op.OperationID = g.generateOperationID(method, rt.Path())
	}

	// Parameters from metadata and path.
	op.Parameters = g.buildParameters(rt)

	// Request body.
	if meta.RequestBody != nil {
		op.RequestBody = g.buildRequestBody(meta.RequestBody)
	}

	// Responses.
	if meta.Responses != nil {
		for status, v := range meta.Responses {
			op.Responses[fmt.Sprintf("%d", status)] = g.buildResponse(status, v)
		}
	} else if meta.Response != nil {
		op.Responses["200"] = g.buildResponse(200, meta.Response)
	}

	// Always add a default response if none specified.
	if len(op.Responses) == 0 {
		op.Responses["200"] = &Response{Description: "Successful response"}
	}

	// Security.
	if len(meta.Security) > 0 {
		sec := make(map[string][]string)
		for _, s := range meta.Security {
			sec[s] = []string{}
		}
		op.Security = []map[string][]string{sec}
	}

	return op
}

// buildParameters extracts parameters from route metadata and path.
func (g *Generator) buildParameters(rt *router.Route) []Parameter {
	var params []Parameter

	// Extract path params from :param patterns.
	path := rt.Path()
	pathParams := extractPathParams(path)
	for _, pp := range pathParams {
		params = append(params, Parameter{
			Name:     pp,
			In:       "path",
			Required: true,
			Schema:   &Schema{Type: "string"},
		})
	}

	// Merge with metadata params (metadata takes precedence).
	for _, pm := range rt.Meta.Params {
		found := false
		for i, p := range params {
			if p.Name == pm.Name && p.In == pm.In {
				params[i].Description = pm.Description
				if pm.Type != "" {
					params[i].Schema = &Schema{Type: pm.Type}
				}
				params[i].Required = pm.Required
				found = true
				break
			}
		}
		if !found {
			p := Parameter{
				Name:        pm.Name,
				In:          pm.In,
				Description: pm.Description,
				Required:    pm.Required,
			}
			if pm.Type != "" {
				p.Schema = &Schema{Type: pm.Type}
			} else {
				p.Schema = &Schema{Type: "string"}
			}
			params = append(params, p)
		}
	}

	return params
}

// buildRequestBody creates a RequestBody from a Go type.
func (g *Generator) buildRequestBody(v any) *RequestBody {
	schema := g.typeToSchema(reflect.TypeOf(v))
	return &RequestBody{
		Required: true,
		Content: map[string]MediaType{
			"application/json": {Schema: schema},
		},
	}
}

// buildResponse creates a Response from a status code and Go type.
func (g *Generator) buildResponse(status int, v any) *Response {
	desc := statusDescription(status)
	if v == nil {
		return &Response{Description: desc}
	}
	schema := g.typeToSchema(reflect.TypeOf(v))
	return &Response{
		Description: desc,
		Content: map[string]MediaType{
			"application/json": {Schema: schema},
		},
	}
}

// ---------------------------------------------------------------------------
// Type Reflection → JSON Schema
// ---------------------------------------------------------------------------

// typeToSchema converts a Go type to a JSON Schema, creating $ref schemas
// for named struct types.
func (g *Generator) typeToSchema(t reflect.Type) *Schema {
	if t == nil {
		return &Schema{Type: "object"}
	}

	// Dereference pointers.
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	// Check if we've already seen this type (breaks cycles).
	if ref, ok := g.seen[t]; ok {
		return &Schema{Ref: "#/components/schemas/" + ref}
	}

	switch t.Kind() {
	case reflect.String:
		return g.handleSpecialStringTypes(t)
	case reflect.Bool:
		return &Schema{Type: "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32:
		return &Schema{Type: "integer", Format: "int32"}
	case reflect.Int64:
		return &Schema{Type: "integer", Format: "int64"}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return &Schema{Type: "integer", Format: "int32"}
	case reflect.Uint64:
		return &Schema{Type: "integer", Format: "int64"}
	case reflect.Float32:
		return &Schema{Type: "number", Format: "float"}
	case reflect.Float64:
		return &Schema{Type: "number", Format: "double"}
	case reflect.Slice, reflect.Array:
		items := g.typeToSchema(t.Elem())
		if t.Elem().Kind() == reflect.Uint8 {
			return &Schema{Type: "string", Format: "byte"}
		}
		return &Schema{Type: "array", Items: items}
	case reflect.Map:
		if t.Key().Kind() == reflect.String {
			valSchema := g.typeToSchema(t.Elem())
			return &Schema{
				Type: "object",
				Properties: map[string]*Schema{
					"additionalProperties": valSchema,
				},
			}
		}
		return &Schema{Type: "object"}
	case reflect.Struct:
		return g.structToSchema(t)
	case reflect.Interface:
		return &Schema{Type: "object"}
	default:
		return &Schema{Type: "string"}
	}
}

// structToSchema converts a struct type to a JSON Schema, handling
// embedded structs, json tags, and common validation tags.
func (g *Generator) structToSchema(t reflect.Type) *Schema {
	// Handle special types.
	if t == reflect.TypeOf(time.Time{}) {
		return &Schema{Type: "string", Format: "date-time"}
	}

	// For named types, create a component schema reference.
	name := t.Name()
	if name != "" && t.PkgPath() != "" {
		// Use short name; if collision, qualify with package.
		schemaName := name
		if _, exists := g.schemas[schemaName]; exists && g.seen[t] != schemaName {
			schemaName = strings.ReplaceAll(t.PkgPath(), "/", "_") + "_" + name
		}
		g.seen[t] = schemaName

		schema := g.buildStructSchema(t)
		g.schemas[schemaName] = schema
		return &Schema{Ref: "#/components/schemas/" + schemaName}
	}

	return g.buildStructSchema(t)
}

// buildStructSchema creates a Schema from struct fields.
func (g *Generator) buildStructSchema(t reflect.Type) *Schema {
	schema := &Schema{
		Type:       "object",
		Properties: make(map[string]*Schema),
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// Skip unexported fields.
		if !field.IsExported() {
			continue
		}

		// Handle embedded structs.
		if field.Anonymous {
			embedded := g.typeToSchema(field.Type)
			// If it's a ref, add it to allOf.
			if embedded.Ref != "" {
				schema.AllOf = append(schema.AllOf, embedded)
			} else if embedded.Properties != nil {
				for k, v := range embedded.Properties {
					schema.Properties[k] = v
				}
				schema.Required = append(schema.Required, embedded.Required...)
			}
			continue
		}

		// Get JSON field name.
		jsonTag := field.Tag.Get("json")
		fieldName := field.Name
		omitEmpty := false
		if jsonTag != "" {
			parts := strings.Split(jsonTag, ",")
			if parts[0] == "-" {
				continue // skip json:"-"
			}
			if parts[0] != "" {
				fieldName = parts[0]
			}
			for _, p := range parts[1:] {
				if p == "omitempty" {
					omitEmpty = true
				}
			}
		}

		propSchema := g.typeToSchema(field.Type)

		// Apply description from doc tag.
		if doc := field.Tag.Get("doc"); doc != "" {
			propSchema.Description = doc
		}

		// Apply example from example tag.
		if ex := field.Tag.Get("example"); ex != "" {
			propSchema.Example = ex
		}

		// Apply enum from enum tag.
		if enum := field.Tag.Get("enum"); enum != "" {
			parts := strings.Split(enum, ",")
			for _, p := range parts {
				propSchema.Enum = append(propSchema.Enum, strings.TrimSpace(p))
			}
		}

		// Apply validation constraints from validate tag.
		if validate := field.Tag.Get("validate"); validate != "" {
			g.applyValidationConstraints(propSchema, validate)
		}

		schema.Properties[fieldName] = propSchema

		// Determine required fields.
		if validate := field.Tag.Get("validate"); strings.Contains(validate, "required") {
			schema.Required = append(schema.Required, fieldName)
		} else if !omitEmpty && jsonTag == "" {
			// Fields without json tags and without omitempty are required.
			schema.Required = append(schema.Required, fieldName)
		}
	}

	return schema
}

// applyValidationConstraints reads common validation tags and maps them to
// JSON Schema constraints.
func (g *Generator) applyValidationConstraints(s *Schema, validate string) {
	parts := strings.Split(validate, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "min=") {
			v := parseFloat(part[4:])
			if s.Type == "string" {
				iv := int(v)
				s.MinLength = &iv
			} else {
				s.Minimum = &v
			}
		}
		if strings.HasPrefix(part, "max=") {
			v := parseFloat(part[4:])
			if s.Type == "string" {
				iv := int(v)
				s.MaxLength = &iv
			} else {
				s.Maximum = &v
			}
		}
		if part == "email" {
			s.Format = "email"
		}
		if part == "url" || part == "uri" {
			s.Format = "uri"
		}
		if part == "uuid" {
			s.Format = "uuid"
		}
		if part == "ip" || part == "ipv4" {
			s.Format = "ipv4"
		}
		if part == "ipv6" {
			s.Format = "ipv6"
		}
	}
}

// handleSpecialStringTypes checks for types that are strings but have
// special formatting.
func (g *Generator) handleSpecialStringTypes(t reflect.Type) *Schema {
	name := t.Name()
	switch name {
	case "UUID":
		return &Schema{Type: "string", Format: "uuid"}
	case "Email":
		return &Schema{Type: "string", Format: "email"}
	case "URL":
		return &Schema{Type: "string", Format: "uri"}
	default:
		return &Schema{Type: "string"}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// convertPathParams converts :param to {param} for OpenAPI paths.
func convertPathParams(path string) string {
	var result strings.Builder
	i := 0
	for i < len(path) {
		if path[i] == ':' {
			result.WriteByte('{')
			i++
			for i < len(path) && path[i] != '/' {
				result.WriteByte(path[i])
				i++
			}
			result.WriteByte('}')
		} else if path[i] == '*' {
			// Wildcard: *name → {name}
			result.WriteByte('{')
			i++
			for i < len(path) && path[i] != '/' {
				result.WriteByte(path[i])
				i++
			}
			result.WriteByte('}')
		} else {
			result.WriteByte(path[i])
			i++
		}
	}
	return result.String()
}

// extractPathParams extracts parameter names from :param patterns.
func extractPathParams(path string) []string {
	var params []string
	parts := strings.Split(path, "/")
	for _, part := range parts {
		if strings.HasPrefix(part, ":") {
			params = append(params, part[1:])
		} else if strings.HasPrefix(part, "*") {
			params = append(params, part[1:])
		}
	}
	return params
}

// generateOperationID creates an operation ID from method and path.
func (g *Generator) generateOperationID(method, path string) string {
	// /users/:id → users_id
	path = strings.ReplaceAll(path, "/", "_")
	path = strings.ReplaceAll(path, ":", "")
	path = strings.ReplaceAll(path, "*", "")
	path = strings.ReplaceAll(path, "{", "")
	path = strings.ReplaceAll(path, "}", "")
	path = strings.Trim(path, "_")
	if path == "" {
		path = "root"
	}
	return method + "_" + path
}

// statusDescription returns a human-readable description for HTTP status codes.
func statusDescription(status int) string {
	switch status {
	case 200:
		return "Successful response"
	case 201:
		return "Created"
	case 204:
		return "No content"
	case 301:
		return "Moved permanently"
	case 302:
		return "Found"
	case 304:
		return "Not modified"
	case 400:
		return "Bad request"
	case 401:
		return "Unauthorized"
	case 403:
		return "Forbidden"
	case 404:
		return "Not found"
	case 405:
		return "Method not allowed"
	case 409:
		return "Conflict"
	case 422:
		return "Unprocessable entity"
	case 429:
		return "Too many requests"
	case 500:
		return "Internal server error"
	case 502:
		return "Bad gateway"
	case 503:
		return "Service unavailable"
	default:
		return fmt.Sprintf("Response %d", status)
	}
}

// parseFloat parses a float from a string, returning 0 on error.
func parseFloat(s string) float64 {
	var v float64
	fmt.Sscanf(s, "%f", &v)
	return v
}
