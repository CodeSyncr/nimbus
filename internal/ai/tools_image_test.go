package ai

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The tool is only advertised when a generator is wired up: offering a tool
// that always fails is worse than not offering it.
func TestImageToolOnlyOfferedWhenAvailable(t *testing.T) {
	plain := &ToolExecutor{AppRoot: t.TempDir()}
	for _, def := range plain.GetToolDefinitions() {
		if def.Name == "generate_image" {
			t.Fatal("generate_image offered with no generator wired up")
		}
	}

	wired := &ToolExecutor{
		AppRoot:       t.TempDir(),
		GenerateImage: func(ctx context.Context, p, s, m string) ([]byte, string, error) { return []byte("x"), "imagen", nil },
	}
	var found bool
	for _, def := range wired.GetToolDefinitions() {
		if def.Name == "generate_image" {
			found = true
		}
	}
	if !found {
		t.Error("generate_image missing when a generator is available")
	}
}

func TestGenerateImageWritesIntoTheWorkspace(t *testing.T) {
	dir := t.TempDir()
	var gotPrompt, gotSize string
	exec := &ToolExecutor{
		AppRoot: dir,
		GenerateImage: func(ctx context.Context, prompt, size, model string) ([]byte, string, error) {
			gotPrompt, gotSize = prompt, size
			return []byte("PNGDATA"), "imagen-3.0-generate-002", nil
		},
	}

	out, _, err := exec.ExecuteTool(context.Background(), "generate_image", map[string]any{
		"prompt": "a red bicycle on a wet street at night",
		"path":   "public/images/hero.png",
		"size":   "1792x1024",
	})
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}

	data, readErr := os.ReadFile(filepath.Join(dir, "public/images/hero.png"))
	if readErr != nil {
		t.Fatalf("image not written: %v", readErr)
	}
	if string(data) != "PNGDATA" {
		t.Errorf("wrong bytes written: %q", data)
	}
	if gotPrompt != "a red bicycle on a wet street at night" || gotSize != "1792x1024" {
		t.Errorf("prompt/size not forwarded: %q / %q", gotPrompt, gotSize)
	}
	// The model needs the path back to reference it.
	if !strings.Contains(out, "public/images/hero.png") {
		t.Errorf("result should name the path: %q", out)
	}
	if !strings.Contains(out, "imagen") {
		t.Errorf("result should name the model: %q", out)
	}
}

// A generated image must not escape the workspace, exactly like any write.
func TestGenerateImageCannotEscapeTheWorkspace(t *testing.T) {
	dir := t.TempDir()
	exec := &ToolExecutor{
		AppRoot:       dir,
		GenerateImage: func(ctx context.Context, p, s, m string) ([]byte, string, error) { return []byte("x"), "m", nil },
	}

	if _, err := exec.CreateImage(context.Background(), "x", "../escaped.png", ""); err == nil {
		t.Error("a path outside the project should be refused")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escaped.png")); err == nil {
		t.Error("a file was written outside the workspace")
	}
}

func TestGenerateImageValidatesInputAndReportsFailures(t *testing.T) {
	exec := &ToolExecutor{
		AppRoot: t.TempDir(),
		GenerateImage: func(ctx context.Context, p, s, m string) ([]byte, string, error) {
			return nil, "", errors.New("quota exceeded")
		},
	}

	if _, err := exec.CreateImage(context.Background(), "", "a.png", ""); err == nil {
		t.Error("an empty prompt should be refused")
	}
	if _, err := exec.CreateImage(context.Background(), "a cat", "", ""); err == nil {
		t.Error("a missing path should be refused")
	}
	// A provider failure reaches the model verbatim so it can react.
	_, err := exec.CreateImage(context.Background(), "a cat", "cat.png", "")
	if err == nil || !strings.Contains(err.Error(), "quota exceeded") {
		t.Errorf("provider error not surfaced: %v", err)
	}
}
