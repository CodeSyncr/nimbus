package ai

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/CodeSyncr/nimbus/cli/auth"
)

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
// EnsureDefaultSkills is retained as a no-op.
//
// The Nimbus skill library is Nimbus Cloud intellectual property and now
// lives on the server, which selects the relevant skill for each turn and
// folds it into the model's instructions. It used to be embedded in this
// binary and written to ~/.nimbus/skills on every run, which shipped the
// whole library to every machine that installed the CLI.
//
// Skills the user writes themselves — in the project or in ~/.nimbus/skills —
// are unaffected: they stay local and are still discovered by LoadSkills and
// read by the load_skill tool.
func EnsureDefaultSkills() error { return nil }

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

	// Nimbus's own skills are deliberately absent: they are Nimbus Cloud
	// property, live on the server, and are applied there. Only skills the
	// user wrote are discoverable locally.

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

	// Case-insensitive fallback across all available skills
	skills, err := LoadSkills(appRoot)
	if err == nil {
		for _, s := range skills {
			if strings.EqualFold(s.Name, skillName) {
				if data, readErr := os.ReadFile(s.Path); readErr == nil {
					return extractSkillBody(string(data)), nil
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

// ReadSkillSection reads only the relevant sections matching a query from a skill document.
func ReadSkillSection(appRoot, skillName, query string) (string, error) {
	fullBody, err := ReadSkillContent(appRoot, skillName)
	if err != nil {
		return "", err
	}
	query = strings.TrimSpace(query)
	if query == "" {
		lines := strings.Split(fullBody, "\n")
		if len(lines) > 150 {
			return strings.Join(lines[:150], "\n") + "\n\n... (skill content truncated, query specific sections with query_skill)", nil
		}
		return fullBody, nil
	}

	sections := strings.Split(fullBody, "\n#")
	var matchedSections []string
	lowerQuery := strings.ToLower(query)

	for idx, sec := range sections {
		headerPrefix := ""
		if idx > 0 {
			headerPrefix = "#"
		}
		fullSec := headerPrefix + sec
		if strings.Contains(strings.ToLower(fullSec), lowerQuery) {
			matchedSections = append(matchedSections, strings.TrimSpace(fullSec))
		}
	}

	if len(matchedSections) > 0 {
		return strings.Join(matchedSections, "\n\n---\n\n"), nil
	}

	lines := strings.Split(fullBody, "\n")
	var matchedLines []string
	for idx, line := range lines {
		if strings.Contains(strings.ToLower(line), lowerQuery) {
			start := idx - 3
			if start < 0 {
				start = 0
			}
			end := idx + 4
			if end > len(lines) {
				end = len(lines)
			}
			matchedLines = append(matchedLines, strings.Join(lines[start:end], "\n"))
		}
	}

	if len(matchedLines) > 0 {
		return "Matching snippets for '" + query + "':\n\n" + strings.Join(matchedLines, "\n\n---\n\n"), nil
	}

	return fmt.Sprintf("No section matching '%s' found in skill '%s'. Showing top summary:\n\n%s", query, skillName, extractTopSummary(fullBody)), nil
}

func extractTopSummary(body string) string {
	lines := strings.Split(body, "\n")
	if len(lines) > 40 {
		return strings.Join(lines[:40], "\n")
	}
	return body
}
