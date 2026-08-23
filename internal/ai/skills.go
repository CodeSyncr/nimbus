package ai

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/CodeSyncr/nimbus/cli/auth"
)

//go:embed default_skills/*
var defaultSkillsFS embed.FS

// Skill represents a lightweight index entry for an agent skill.
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
	Source      string `json:"source"` // "project" | "global" | "embedded"
}

var (
	reFrontmatter = regexp.MustCompile(`(?s)^---\r?\n(.*?)\r?\n---\r?\n(.*)$`)
	reName        = regexp.MustCompile(`(?m)^name:\s*(.+)$`)
	reDesc        = regexp.MustCompile(`(?m)^description:\s*(.+)$`)
)

// EnsureDefaultSkills writes embedded default skills to ~/.nimbus/skills/ if not already present.
func EnsureDefaultSkills() error {
	configDir, err := auth.ConfigDir()
	if err != nil {
		home, hErr := os.UserHomeDir()
		if hErr != nil {
			return hErr
		}
		configDir = filepath.Join(home, ".nimbus")
	}

	skillsDir := filepath.Join(configDir, "skills")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		return err
	}

	// Walk embedded filesystem and copy missing skills
	return fs.WalkDir(defaultSkillsFS, "default_skills", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel("default_skills", path)
		if err != nil {
			return err
		}

		targetPath := filepath.Join(skillsDir, relPath)
		if mkErr := os.MkdirAll(filepath.Dir(targetPath), 0755); mkErr != nil {
			return mkErr
		}

		content, readErr := defaultSkillsFS.ReadFile(path)
		if readErr != nil {
			return readErr
		}

		return os.WriteFile(targetPath, content, 0644)
	})
}

// LoadSkills discovers and builds a lightweight index of skills (name + description only).
func LoadSkills(appRoot string) ([]Skill, error) {
	skillsMap := make(map[string]Skill)

	// 1. Scan project-level skills: <appRoot>/.nimbus/skills/
	if appRoot != "" {
		projectSkillsDir := filepath.Join(appRoot, ".nimbus", "skills")
		scanSkillsDirectory(projectSkillsDir, "project", skillsMap)
	}

	// 2. Scan global skills: ~/.nimbus/skills/
	if configDir, err := auth.ConfigDir(); err == nil {
		globalSkillsDir := filepath.Join(configDir, "skills")
		scanSkillsDirectory(globalSkillsDir, "global", skillsMap)
	}

	// 3. Fallback to embedded default skills if nothing on disk
	if len(skillsMap) == 0 {
		_ = fs.WalkDir(defaultSkillsFS, "default_skills", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, "SKILL.md") {
				return nil
			}
			contentBytes, readErr := defaultSkillsFS.ReadFile(path)
			if readErr != nil {
				return nil
			}
			skill := parseSkillHeader(string(contentBytes), path, "embedded")
			if skill.Name != "" {
				skillsMap[skill.Name] = skill
			}
			return nil
		})
	}

	var result []Skill
	for _, s := range skillsMap {
		result = append(result, s)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result, nil
}

func scanSkillsDirectory(dir string, source string, out map[string]Skill) {
	if _, err := os.Stat(dir); err != nil {
		return
	}

	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), "SKILL.md") {
			return nil
		}

		contentBytes, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}

		skill := parseSkillHeader(string(contentBytes), path, source)
		if skill.Name != "" {
			// Project-level skills override global skills
			if _, exists := out[skill.Name]; !exists || source == "project" {
				out[skill.Name] = skill
			}
		}
		return nil
	})
}

// parseSkillHeader extracts ONLY the minimal YAML frontmatter (name + description) for the lightweight index.
func parseSkillHeader(content, path, source string) Skill {
	skill := Skill{
		Path:   path,
		Source: source,
	}

	matches := reFrontmatter.FindStringSubmatch(content)
	if len(matches) == 3 {
		frontmatter := matches[1]
		if nameMatch := reName.FindStringSubmatch(frontmatter); len(nameMatch) == 2 {
			skill.Name = strings.TrimSpace(nameMatch[1])
		}
		if descMatch := reDesc.FindStringSubmatch(frontmatter); len(descMatch) == 2 {
			skill.Description = strings.TrimSpace(descMatch[1])
		}
	}

	if skill.Name == "" {
		parentDir := filepath.Base(filepath.Dir(path))
		if parentDir != "." && parentDir != "skills" {
			skill.Name = parentDir
		} else {
			skill.Name = strings.TrimSuffix(filepath.Base(path), ".md")
		}
	}

	return skill
}

// ReadSkillContent reads the full SKILL.md body for a given skill on demand.
func ReadSkillContent(appRoot, skillName string) (string, error) {
	skillName = strings.TrimSpace(skillName)
	if skillName == "" {
		return "", fmt.Errorf("skill name cannot be empty")
	}

	// 1. Check project-level skill
	if appRoot != "" {
		candidates := []string{
			filepath.Join(appRoot, ".nimbus", "skills", skillName, "SKILL.md"),
			filepath.Join(appRoot, ".nimbus", "skills", skillName+".md"),
		}
		for _, c := range candidates {
			if data, err := os.ReadFile(c); err == nil {
				return extractSkillBody(string(data)), nil
			}
		}
	}

	// 2. Check global skill
	if configDir, err := auth.ConfigDir(); err == nil {
		candidates := []string{
			filepath.Join(configDir, "skills", skillName, "SKILL.md"),
			filepath.Join(configDir, "skills", skillName+".md"),
		}
		for _, c := range candidates {
			if data, err := os.ReadFile(c); err == nil {
				return extractSkillBody(string(data)), nil
			}
		}
	}

	// 3. Check embedded default skills
	embeddedPath := fmt.Sprintf("default_skills/%s/SKILL.md", skillName)
	if data, err := defaultSkillsFS.ReadFile(embeddedPath); err == nil {
		return extractSkillBody(string(data)), nil
	}

	// Case-insensitive fallback across all available skills
	skills, err := LoadSkills(appRoot)
	if err == nil {
		for _, s := range skills {
			if strings.EqualFold(s.Name, skillName) {
				if s.Source == "embedded" {
					if data, readErr := defaultSkillsFS.ReadFile(s.Path); readErr == nil {
						return extractSkillBody(string(data)), nil
					}
				} else {
					if data, readErr := os.ReadFile(s.Path); readErr == nil {
						return extractSkillBody(string(data)), nil
					}
				}
			}
		}
	}

	var available []string
	if skills != nil {
		for _, s := range skills {
			available = append(available, s.Name)
		}
	}
	return "", fmt.Errorf("skill '%s' not found. Available skills: %s", skillName, strings.Join(available, ", "))
}

func extractSkillBody(content string) string {
	matches := reFrontmatter.FindStringSubmatch(content)
	if len(matches) == 3 {
		return strings.TrimSpace(matches[2])
	}
	return strings.TrimSpace(content)
}

// FormatSkillsSummary formats the lightweight skill index as a bullet list for the system prompt.
func FormatSkillsSummary(skills []Skill) string {
	if len(skills) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Available Agent Skills\n\n")
	sb.WriteString("Check the Available Skills list below. If a skill's description matches the current task, call load_skill before proceeding with related work. Don't load skills that aren't relevant:\n\n")

	for _, s := range skills {
		desc := s.Description
		if desc == "" {
			desc = "Specialized engineering instructions and reference patterns."
		}
		sb.WriteString(fmt.Sprintf("- **%s**: %s\n", s.Name, desc))
	}
	sb.WriteString("\n")
	return sb.String()
}
