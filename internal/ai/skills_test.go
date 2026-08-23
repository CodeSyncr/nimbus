package ai

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureDefaultSkillsAndLoading(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	// 1. Test provisioning default skills to ~/.nimbus/skills/
	if err := EnsureDefaultSkills(); err != nil {
		t.Fatalf("EnsureDefaultSkills failed: %v", err)
	}

	skillsDir := filepath.Join(tmpHome, ".nimbus", "skills")
	frontendSkillPath := filepath.Join(skillsDir, "frontend-design", "SKILL.md")
	if _, err := os.Stat(frontendSkillPath); err != nil {
		t.Errorf("expected frontend-design skill to be provisioned at %s: %v", frontendSkillPath, err)
	}

	nimbusSkillPath := filepath.Join(skillsDir, "nimbus-expert", "SKILL.md")
	if _, err := os.Stat(nimbusSkillPath); err != nil {
		t.Errorf("expected nimbus-expert skill to be provisioned at %s: %v", nimbusSkillPath, err)
	}

	// 2. Test LoadSkills from global directory (Tier 1: index only)
	loaded, err := LoadSkills("")
	if err != nil {
		t.Fatalf("LoadSkills failed: %v", err)
	}

	var foundFrontend, foundNimbus, foundGo bool
	for _, s := range loaded {
		if s.Name == "frontend-design" {
			foundFrontend = true
			if !strings.Contains(s.Description, "distinctive") {
				t.Errorf("unexpected description for frontend-design: %s", s.Description)
			}
		}
		if s.Name == "nimbus-expert" || s.Name == "nimbus_expert" {
			foundNimbus = true
		}
		if s.Name == "go-architect" {
			foundGo = true
		}
	}

	if !foundFrontend {
		t.Errorf("frontend-design skill not found in loaded skills: %+v", loaded)
	}
	if !foundNimbus {
		t.Errorf("nimbus-expert skill not found in loaded skills: %+v", loaded)
	}
	if !foundGo {
		t.Errorf("go-architect skill not found in loaded skills: %+v", loaded)
	}

	// 3. Test Project Level Override
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

	// 4. Test Tier 2: ReadSkillContent on-demand
	body, err := ReadSkillContent(tmpProject, "frontend-design")
	if err != nil {
		t.Fatalf("ReadSkillContent failed: %v", err)
	}
	if !strings.Contains(body, "# Custom Design Instructions") {
		t.Errorf("expected custom body content, got: %s", body)
	}

	// 5. Test ToolExecutor load_skill tool call
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

	// 6. Test FormatSkillsSummary
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
