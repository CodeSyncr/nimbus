package ai

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The Nimbus skill library is Nimbus Cloud property and must not ship in the
// CLI: nothing may be embedded, and nothing may be written to the user's
// machine.
func TestNoProprietarySkillsShipInTheClient(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("NIMBUS_CONFIG_DIR", filepath.Join(home, ".nimbus"))

	if err := EnsureDefaultSkills(); err != nil {
		t.Fatalf("EnsureDefaultSkills: %v", err)
	}

	// Nothing may have been written into the user's skills directory.
	skillsDir := filepath.Join(home, ".nimbus", "skills")
	if entries, err := os.ReadDir(skillsDir); err == nil && len(entries) > 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("the CLI wrote skills to the user's disk: %v", names)
	}

	// And discovery must find nothing when the user has authored nothing.
	skills, err := LoadSkills(t.TempDir())
	if err != nil {
		t.Fatalf("LoadSkills: %v", err)
	}
	for _, s := range skills {
		if s.Source == "embedded" {
			t.Errorf("embedded skill %q is still bundled in the CLI", s.Name)
		}
	}
}

// A skill the user writes themselves stays local and is still discovered.
func TestUserAuthoredSkillsStillWork(t *testing.T) {
	project := t.TempDir()
	dir := filepath.Join(project, ".nimbus", "skills", "house-style")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: house-style\ndescription: Our internal conventions.\n---\n\nAlways use tabs.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}

	skills, err := LoadSkills(project)
	if err != nil {
		t.Fatalf("LoadSkills: %v", err)
	}
	var found bool
	for _, s := range skills {
		if s.Name == "house-style" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the user's own skill was not discovered: %+v", skills)
	}

	content, err := ReadSkillContent(project, "house-style")
	if err != nil || !strings.Contains(content, "Always use tabs") {
		t.Errorf("user skill body not readable: %v / %q", err, content)
	}
}
