/*
|--------------------------------------------------------------------------
| AI SDK — Tool / Function-Calling System
|--------------------------------------------------------------------------
|
| Tools let agents call Go functions. Register tools with a name,
| description, and a typed handler function. The framework
| auto-generates JSON schemas, marshals arguments, and dispatches calls.
|
| Usage:
|
|   ai.RegisterTool(ai.Tool{
|       Name:        "weather",
|       Description: "Get current weather for a city",
|       Run:         func(ctx context.Context, in WeatherInput) (WeatherOutput, error) { ... },
|   })
|
|   // Or via the fluent builder:
|   ai.NewTool("calculator").
|       Description("Evaluate a mathematical expression").
|       Handler(func(ctx context.Context, in CalcInput) (CalcOutput, error) { ... }).
|       Register()
|
*/

package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
)

// ---------------------------------------------------------------------------
// Tool definition
// ---------------------------------------------------------------------------

// Tool represents a callable function that an AI agent can invoke.
type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description"`

	// Run is the handler function. It must have the signature:
	//   func(ctx context.Context, input T) (U, error)
	// where T is the input struct (used to generate JSON schema)
	// and U is the output (marshaled to JSON for the model).
	Run any `json:"-"`

	// schema is lazily computed from the Run function's input type.
	schema     json.RawMessage
	inputType  reflect.Type
	outputType reflect.Type
}

// Schema returns the JSON-Schema representation of the tool's input
// parameters, suitable for sending to model APIs.
func (t *Tool) Schema() json.RawMessage {
	if t.schema == nil {
		t.schema = structToJSONSchema(t.inputType)
	}
	return t.schema
}

// ToSpec converts to a ToolSpec for wire-format.
func (t *Tool) ToSpec() ToolSpec {
	return ToolSpec{
		Name:        t.Name,
		Description: t.Description,
		Parameters:  t.Schema(),
	}
}

// Execute invokes the tool handler with JSON arguments.
func (t *Tool) Execute(ctx context.Context, argsJSON json.RawMessage) (json.RawMessage, error) {
	// Unmarshal args into the input type.
	inputPtr := reflect.New(t.inputType)
	if len(argsJSON) > 0 {
		if err := json.Unmarshal(argsJSON, inputPtr.Interface()); err != nil {
			return nil, fmt.Errorf("ai: tool %q: unmarshal args: %w", t.Name, err)
		}
	}

	// Call the handler.
	fn := reflect.ValueOf(t.Run)
	results := fn.Call([]reflect.Value{
		reflect.ValueOf(ctx),
		inputPtr.Elem(),
	})

	// results[1] is the error.
	if !results[1].IsNil() {
		return nil, results[1].Interface().(error)
	}

	// Marshal the output.
	out, err := json.Marshal(results[0].Interface())
	if err != nil {
		return nil, fmt.Errorf("ai: tool %q: marshal output: %w", t.Name, err)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Tool builder (fluent API)
// ---------------------------------------------------------------------------

// ToolBuilder provides a fluent API for constructing tools.
type ToolBuilder struct {
	tool Tool
}

// NewTool starts building a tool with the given name.
func NewTool(name string) *ToolBuilder {
	return &ToolBuilder{tool: Tool{Name: name}}
}

// Desc sets the tool description.
func (b *ToolBuilder) Desc(desc string) *ToolBuilder {
	b.tool.Description = desc
	return b
}

// Handler sets the typed handler function.
func (b *ToolBuilder) Handler(fn any) *ToolBuilder {
	b.tool.Run = fn
	return b
}

// Build validates and returns the tool.
func (b *ToolBuilder) Build() (*Tool, error) {
	return validateTool(&b.tool)
}

// Register validates and registers the tool globally.
func (b *ToolBuilder) Register() error {
	t, err := b.Build()
	if err != nil {
		return err
	}
	RegisterTool(*t)
	return nil
}

// ---------------------------------------------------------------------------
// Global tool registry
// ---------------------------------------------------------------------------

var (
	toolsMu  sync.RWMutex
	toolsMap = map[string]*Tool{}
)

// RegisterTool adds a tool to the global registry. It validates the
// handler signature and pre-computes the input JSON schema.
func RegisterTool(t Tool) {
	validated, err := validateTool(&t)
	if err != nil {
		panic(err)
	}
	toolsMu.Lock()
	defer toolsMu.Unlock()
	toolsMap[validated.Name] = validated
}

// GetTool retrieves a registered tool by name.
func GetTool(name string) (*Tool, bool) {
	toolsMu.RLock()
	defer toolsMu.RUnlock()
	t, ok := toolsMap[name]
	return t, ok
}

// AllTools returns all registered tools.
func AllTools() []*Tool {
	toolsMu.RLock()
	defer toolsMu.RUnlock()
	out := make([]*Tool, 0, len(toolsMap))
	for _, t := range toolsMap {
		out = append(out, t)
	}
	return out
}

// ToolSpecs returns ToolSpec for all registered tools (for provider APIs).
func ToolSpecs() []ToolSpec {
	tools := AllTools()
	specs := make([]ToolSpec, len(tools))
	for i, t := range tools {
		specs[i] = t.ToSpec()
	}
	return specs
}

// ExecuteTool finds and runs a tool by name with raw JSON arguments.
func ExecuteTool(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
	t, ok := GetTool(name)
	if !ok {
		return nil, fmt.Errorf("ai: tool %q not registered", name)
	}
	return t.Execute(ctx, args)
}

// ---------------------------------------------------------------------------
// Validation helper
// ---------------------------------------------------------------------------

// validateTool ensures the handler has the right signature and
// pre-computes reflection metadata.
func validateTool(t *Tool) (*Tool, error) {
	if t.Name == "" {
		return nil, fmt.Errorf("ai: tool name is required")
	}
	if t.Run == nil {
		return nil, fmt.Errorf("ai: tool %q: handler (Run) is required", t.Name)
	}

	fn := reflect.TypeOf(t.Run)
	if fn.Kind() != reflect.Func {
		return nil, fmt.Errorf("ai: tool %q: Run must be a function", t.Name)
	}
	if fn.NumIn() != 2 {
		return nil, fmt.Errorf("ai: tool %q: Run must have exactly 2 args (context.Context, InputStruct)", t.Name)
	}
	if fn.NumOut() != 2 {
		return nil, fmt.Errorf("ai: tool %q: Run must return exactly 2 values (Output, error)", t.Name)
	}

	// First arg must be context.Context
	ctxType := reflect.TypeOf((*context.Context)(nil)).Elem()
	if !fn.In(0).Implements(ctxType) {
		return nil, fmt.Errorf("ai: tool %q: first arg must be context.Context", t.Name)
	}

	// Second out must be error
	errType := reflect.TypeOf((*error)(nil)).Elem()
	if !fn.Out(1).Implements(errType) {
		return nil, fmt.Errorf("ai: tool %q: second return must be error", t.Name)
	}

	t.inputType = fn.In(1)
	t.outputType = fn.Out(0)
	t.schema = structToJSONSchema(t.inputType)

	return t, nil
}

// ---------------------------------------------------------------------------
// Schema generation
// ---------------------------------------------------------------------------

// structToJSONSchema generates a minimal JSON Schema from a Go struct
// using reflection. Supports string, int, float64, bool, and nested
// structs. Reads `json` tags for property names and `description` tags
// for field descriptions.
func structToJSONSchema(t reflect.Type) json.RawMessage {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	schema := map[string]any{
		"type": "object",
	}

	if t.Kind() != reflect.Struct {
		// Fallback: any non-struct becomes an empty object schema.
		b, _ := json.Marshal(schema)
		return b
	}

	properties := map[string]any{}
	required := []string{}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}

		name := field.Tag.Get("json")
		if name == "" || name == "-" {
			name = field.Name
		}
		// Strip ",omitempty"
		if idx := len(name); idx > 0 {
			for j := 0; j < len(name); j++ {
				if name[j] == ',' {
					name = name[:j]
					break
				}
			}
		}

		prop := map[string]any{}
		switch field.Type.Kind() {
		case reflect.String:
			prop["type"] = "string"
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			prop["type"] = "integer"
		case reflect.Float32, reflect.Float64:
			prop["type"] = "number"
		case reflect.Bool:
			prop["type"] = "boolean"
		case reflect.Slice:
			prop["type"] = "array"
		case reflect.Struct:
			// Nested struct — recurse.
			nested := structToJSONSchema(field.Type)
			var m map[string]any
			json.Unmarshal(nested, &m)
			prop = m
		default:
			prop["type"] = "string"
		}

		if desc := field.Tag.Get("description"); desc != "" {
			prop["description"] = desc
		}

		properties[name] = prop
		// All exported fields are required by default.
		required = append(required, name)
	}

	schema["properties"] = properties
	if len(required) > 0 {
		schema["required"] = required
	}

	b, _ := json.Marshal(schema)
	return b
}
