package ai

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanProject(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "nimbus_ai_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create mock go.mod
	goModContent := `module example.com/my-nimbus-app

go 1.24
`
	if err := os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte(goModContent), 0644); err != nil {
		t.Fatalf("failed to write go.mod: %v", err)
	}

	// Create mock models and controllers
	_ = os.MkdirAll(filepath.Join(tempDir, "app", "models"), 0755)
	_ = os.MkdirAll(filepath.Join(tempDir, "app", "controllers"), 0755)
	_ = os.WriteFile(filepath.Join(tempDir, "app", "models", "user.go"), []byte("package models\n"), 0644)
	_ = os.WriteFile(filepath.Join(tempDir, "app", "controllers", "user_controller.go"), []byte("package controllers\n"), 0644)

	ctx, err := ScanProject(tempDir)
	if err != nil {
		t.Fatalf("ScanProject failed: %v", err)
	}

	if ctx.GoModName != "example.com/my-nimbus-app" {
		t.Errorf("expected module 'example.com/my-nimbus-app', got '%s'", ctx.GoModName)
	}
	if ctx.GoVersion != "1.24" {
		t.Errorf("expected go version '1.24', got '%s'", ctx.GoVersion)
	}
	if len(ctx.Models) != 1 || ctx.Models[0] != "user.go" {
		t.Errorf("expected models ['user.go'], got %v", ctx.Models)
	}
	if len(ctx.Controllers) != 1 || ctx.Controllers[0] != "user_controller.go" {
		t.Errorf("expected controllers ['user_controller.go'], got %v", ctx.Controllers)
	}

	formatted := ctx.FormatSystemContext()
	if formatted == "" {
		t.Error("expected non-empty formatted system context")
	}
}
