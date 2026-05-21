package view

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEndeachNotEndEach(t *testing.T) {
	eng := New("", nil)
	input := `@each(items)<li>{{ . }}</li>@endeach`
	got := eng.convertNimbusToGo(input)
	if strings.Contains(got, "}}each") {
		t.Errorf("convertNimbusToGo: @endeach left trailing 'each', got %q", got)
	}
}

func TestEndifNotEndIf(t *testing.T) {
	eng := New("", nil)
	// @endif must become {{ end }}, not {{ end }}if (regex was matching @end first)
	input := `@if(.x)yes@endif`
	got := eng.convertNimbusToGo(input)
	if strings.Contains(got, "}}if") {
		t.Errorf("convertNimbusToGo: @endif left trailing 'if', got %q", got)
	}
	if !strings.Contains(got, "{{ end }}") {
		t.Errorf("convertNimbusToGo: expected {{ end }}, got %q", got)
	}
}

func TestComponentRenders(t *testing.T) {
	roots := []string{"../../nimbus-starter/resources/views", "../../nimbus-starter/views", "../nimbus-starter/views", "views"}
	var root string
	for _, r := range roots {
		if _, err := os.Stat(filepath.Join(r, "components", "card.nimbus")); err == nil {
			root = r
			break
		}
	}
	if root == "" {
		t.Skip("components/card.nimbus not found")
	}
	eng := New(root, nil)
	out, err := eng.Render("test_component", map[string]any{"title": "Test"})
	if err != nil {
		t.Fatalf("Render test_component: %v", err)
	}
	if !strings.Contains(out, "Card title") || !strings.Contains(out, "Card content") {
		t.Errorf("component slot not rendered: got %q", out)
	}
	if !strings.Contains(out, "rounded-lg") {
		t.Errorf("component wrapper missing: got %q", out)
	}
}

func TestFormTemplateParses(t *testing.T) {
	// Use nimbus-starter views if present, else skip
	roots := []string{"../../nimbus-starter/resources/views", "../../nimbus-starter/views", "../nimbus-starter/views", "views"}
	var root string
	for _, r := range roots {
		if _, err := os.Stat(filepath.Join(r, "apps/todo/form.nimbus")); err == nil {
			root = r
			break
		}
	}
	if root == "" {
		t.Skip("apps/todo/form.nimbus not found")
	}
	eng := New(root, nil)
	_, err := eng.Render("apps/todo/form", map[string]any{
		"title": "Test",
		"csrf":  "x",
	})
	if err != nil {
		t.Fatalf("Render apps/todo/form: %v", err)
	}
}
func TestLenHelper(t *testing.T) {
	eng := New("", nil)

	tmpl := `{{ len .items }} {{ len .str }} {{ len .dict }}`
	converted := eng.convertNimbusToGo(tmpl)

	// Expectations:
	// {{ len .items }} should remain {{ len .items }} because it doesn't match the whole-word regex
	if !strings.Contains(converted, "{{ len .items }}") {
		t.Errorf("convertNimbusToGo: expected it to leave len call alone, got %q", converted)
	}
}

func TestBooleanInversion(t *testing.T) {
	eng := New("", nil)
	input := `@if(!.empty)yes@endif`
	got := eng.convertNimbusToGo(input)
	expected := `{{ $props := index . "$props" }}{{ $context := index . "$context" }}{{ if not .empty }}yes{{ end }}`
	if got != expected {
		t.Errorf("convertNimbusToGo: expected %q, got %q", expected, got)
	}
}

func TestTemplateExecutionWithLenAndNot(t *testing.T) {
	// Create a temp directory for views
	tmpDir, err := os.MkdirTemp("", "nimbus_views")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a test template
	tmplContent := `Count: {{ len .items }}. Empty check: @if(!.items)empty@else{{ len .items }}@endif`
	err = os.WriteFile(filepath.Join(tmpDir, "test.nimbus"), []byte(tmplContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write template: %v", err)
	}

	eng := New(tmpDir, nil)

	// Test case 1: Items present
	data := map[string]any{
		"items": []any{1, 2, 3},
	}
	out, err := eng.Render("test", data)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	expected := "Count: 3. Empty check: 3"
	if strings.TrimSpace(out) != expected {
		t.Errorf("Render: expected %q, got %q", expected, out)
	}

	// Test case 2: Items empty
	dataEmpty := map[string]any{
		"items": []any{},
	}
	outEmpty, err := eng.Render("test", dataEmpty)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	// Note: len of empty slice is 0, which is "falsy" in Go template context?
	// Actually no, empty slice isn't falsy in Go templates, we usually use len.
	// But `not .items` will return true if .items is empty slice?
	// In Go templates: "The condition is the empty value of its type"
	// For slices, empty is len 0.
	expectedEmpty := "Count: 0. Empty check: empty"
	if strings.TrimSpace(outEmpty) != expectedEmpty {
		t.Errorf("Render (empty items): expected %q, got %q", expectedEmpty, outEmpty)
	}
}

func TestPropsAPI(t *testing.T) {
	// Create a temp directory for views
	tmpDir, err := os.MkdirTemp("", "nimbus_props_views")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create component directory and a component using props
	compDir := filepath.Join(tmpDir, "components")
	if err := os.MkdirAll(compDir, 0755); err != nil {
		t.Fatalf("Failed to create components dir: %v", err)
	}

	compContent := `<input {{ $props.merge(dict "class" "form-input" "type" "text").except(slice "label").toAttrs() }} />`
	if err := os.WriteFile(filepath.Join(compDir, "input.nimbus"), []byte(compContent), 0644); err != nil {
		t.Fatalf("Failed to write component: %v", err)
	}

	// Use component in a test page
	pageContent := `@!input({
		name: "email",
		label: "Email Address",
		class: ["w-full", "border-red-500"],
		placeholder: "you@example.com"
	})`
	if err := os.WriteFile(filepath.Join(tmpDir, "test_page.nimbus"), []byte(pageContent), 0644); err != nil {
		t.Fatalf("Failed to write test page: %v", err)
	}

	eng := New(tmpDir, nil)
	out, err := eng.Render("test_page", nil)
	if err != nil {
		t.Fatalf("Render page failed: %v", err)
	}

	// Check output attributes:
	// - class should merge defaults: "form-input w-full border-red-500"
	// - name and placeholder should be serialized
	// - label should be excluded (via except)
	expectedAttrs := []string{
		`name="email"`,
		`placeholder="you@example.com"`,
		`type="text"`,
		`class="form-input w-full border-red-500"`,
	}
	for _, attr := range expectedAttrs {
		if !strings.Contains(out, attr) {
			t.Errorf("Expected output to contain attribute %s, got: %q", attr, out)
		}
	}
	if strings.Contains(out, "label=") {
		t.Errorf("Expected 'label' attribute to be excluded, got: %q", out)
	}
}

func TestProvideInject(t *testing.T) {
	// Create a temp directory for views
	tmpDir, err := os.MkdirTemp("", "nimbus_context_views")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create component directory
	compDir := filepath.Join(tmpDir, "components", "field")
	if err := os.MkdirAll(compDir, 0755); err != nil {
		t.Fatalf("Failed to create components field dir: %v", err)
	}

	// Parent component providing value
	rootContent := `{{ $context.Provide("name_key", name) }}<div>{{{ .slots.main }}}</div>`
	if err := os.WriteFile(filepath.Join(compDir, "root.nimbus"), []byte(rootContent), 0644); err != nil {
		t.Fatalf("Failed to write root component: %v", err)
	}

	// Child component injecting value
	labelContent := `{{ const n = $context.Inject("name_key") }}<label>Name: {{ $n }}</label>`
	if err := os.WriteFile(filepath.Join(compDir, "label.nimbus"), []byte(labelContent), 0644); err != nil {
		t.Fatalf("Failed to write label component: %v", err)
	}

	// Page content nesting them
	pageContent := `@field.root({ name: "john_doe" })
		@!field.label()
	@end`
	if err := os.WriteFile(filepath.Join(tmpDir, "test_page.nimbus"), []byte(pageContent), 0644); err != nil {
		t.Fatalf("Failed to write test page: %v", err)
	}

	eng := New(tmpDir, nil)
	out, err := eng.Render("test_page", nil)
	if err != nil {
		t.Fatalf("Render page failed: %v", err)
	}

	expected := "<div><label>Name: john_doe</label></div>"
	clean := func(s string) string {
		s = strings.ReplaceAll(s, "\n", "")
		s = strings.ReplaceAll(s, "\r", "")
		s = strings.ReplaceAll(s, "\t", "")
		s = strings.ReplaceAll(s, " ", "")
		return s
	}
	if clean(out) != clean(expected) {
		t.Errorf("Provide/Inject output mismatch. Expected %q, got %q", expected, out)
	}
}
