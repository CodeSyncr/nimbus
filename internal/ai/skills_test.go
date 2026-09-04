package ai

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CodeSyncr/nimbus/cli/auth"
)

// TestUserSkillPipeline covers the local skill pathway that remains in the
// CLI: a skill the user writes in their project is discovered, readable, and
// loadable through the tool.
//
// Provisioning Nimbus's own library to ~/.nimbus/skills was removed — that
// library is Nimbus Cloud property and is applied server-side now. See
// TestNoProprietarySkillsShipInTheClient.
func TestUserSkillPipeline(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv(auth.ConfigDirEnv, filepath.Join(tmpHome, ".nimbus"))

	// A project-level skill authored by the user.
	tmpProject := t.TempDir()
	projSkillDir := filepath.Join(tmpProject, ".nimbus", "skills", "frontend-design")
	_ = os.MkdirAll(projSkillDir, 0755)
	customContent := `---
name: frontend-design
description: Custom project-level design guidelines for this app.
---
# Custom Design Instructions
`
	_ = os.WriteFile(filepath.Join(projSkillDir, "SKILL.md"), []byte(customContent), 0644)

	projLoaded, err := LoadSkills(tmpProject)
	if err != nil {
		t.Fatalf("LoadSkills with project failed: %v", err)
	}

	var overridden bool
	for _, s := range projLoaded {
		if s.Name == "frontend-design" {
			if s.Source == "project" && strings.Contains(s.Description, "Custom project-level") {
				overridden = true
			}
		}
	}
	if !overridden {
		t.Errorf("expected project-level skill override for frontend-design")
	}

	// Its body is readable on demand.
	body, err := ReadSkillContent(tmpProject, "frontend-design")
	if err != nil {
		t.Fatalf("ReadSkillContent failed: %v", err)
	}
	if !strings.Contains(body, "# Custom Design Instructions") {
		t.Errorf("expected custom body content, got: %s", body)
	}

	// And the load_skill tool serves it.
	executor := NewToolExecutor(tmpProject)
	toolOut, diff, err := executor.ExecuteTool(context.Background(), "load_skill", map[string]any{
		"skill_name": "frontend-design",
	})
	if err != nil {
		t.Fatalf("ExecuteTool load_skill failed: %v", err)
	}
	if diff != "" {
		t.Errorf("expected no diff for load_skill")
	}
	if !strings.Contains(toolOut, "# Custom Design Instructions") {
		t.Errorf("expected skill instructions in tool output, got: %s", toolOut)
	}

	// The summary lists the user's own skills.
	summary := FormatSkillsSummary(projLoaded)
	if !strings.Contains(summary, "Available Agent Skills") {
		t.Errorf("expected Available Agent Skills header, got: %s", summary)
	}
	if !strings.Contains(summary, "load_skill") {
		t.Errorf("expected load_skill instruction in summary, got: %s", summary)
	}
	if !strings.Contains(summary, "frontend-design") {
		t.Errorf("expected frontend-design in summary, got: %s", summary)
	}
}
