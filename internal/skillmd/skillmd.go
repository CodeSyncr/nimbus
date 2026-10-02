// Package skillmd parses SKILL.md files: YAML frontmatter with a name and
// description, then the skill's instructions. It is shared by the AI SDK
// (plugins/ai) and the `nimbus ai` CLI so both read skills the same way,
// without the CLI depending on the SDK.
package skillmd

import (
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// ErrNoFrontmatter is returned for a file that does not open with a
// --- frontmatter block.
var ErrNoFrontmatter = errors.New("missing --- frontmatter with name and description")

// Parse splits a SKILL.md into its frontmatter name and description and its
// body. Descriptions may span lines (YAML folded or plain); whitespace in
// them is collapsed to single spaces.
func Parse(content string) (name, description, body string, err error) {
	content = strings.TrimPrefix(content, "\uFEFF")
	norm := strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(norm, "---\n") {
		return "", "", "", ErrNoFrontmatter
	}
	rest := norm[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", "", "", ErrNoFrontmatter
	}
	front, after := rest[:end], rest[end+len("\n---"):]
	if i := strings.IndexByte(after, '\n'); i >= 0 {
		after = after[i+1:]
	} else {
		after = ""
	}
	var meta struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	if err := yaml.Unmarshal([]byte(front), &meta); err != nil {
		return "", "", "", fmt.Errorf("frontmatter: %w", err)
	}
	return strings.TrimSpace(meta.Name), strings.Join(strings.Fields(meta.Description), " "), strings.TrimSpace(after), nil
}
