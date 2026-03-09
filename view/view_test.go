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
	roots := []string{"../../nimbus-starter/views", "../nimbus-starter/views", "views"}
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
	roots := []string{"../../nimbus-starter/views", "../nimbus-starter/views", "views"}
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
