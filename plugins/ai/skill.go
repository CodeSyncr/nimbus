/*
|--------------------------------------------------------------------------
| AI SDK — Skills
|--------------------------------------------------------------------------
|
| A skill is a named set of instructions an agent loads only when a task
| needs it. The agent's system prompt lists each skill by name and one-line
| description; when a request matches, the model calls the load_skill tool
| and gets the full instructions (and, on request, any file shipped with
| the skill). Twenty procedures cost twenty lines of prompt, not twenty
| pages.
|
| Skills use the SKILL.md format shared with `nimbus ai` and other agent
| tools: a folder per skill with a SKILL.md whose YAML frontmatter names
| and describes it.
|
|   skills/
|     refunds/
|       SKILL.md            ---\n name: refunds\n description: …\n ---\n …
|       policy.md           extra reference, read with load_skill(file)
|     chargebacks/SKILL.md
|
|   //go:embed skills
|   var skillFiles embed.FS
|
|   agent := ai.NewAgent("You are a support agent.").
|       WithSkills(ai.MustLoadSkills(skillFiles, "skills")).
|       WithTools("lookup_order")
|
| Skills are instructions to the model, so load them from your own code or
| files, never from user input.
|
*/

package ai

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/CodeSyncr/nimbus/internal/skillmd"
)

// Skill limits, from the SKILL.md format.
const (
	MaxSkillNameLen        = 64
	MaxSkillDescriptionLen = 1024
	// MaxSkillFileBytes caps a SKILL.md or a resource file the agent reads,
	// so one oversized file cannot flood the model's context.
	MaxSkillFileBytes = 256 << 10
)

// SkillToolName is the tool an agent with skills calls to load one.
const SkillToolName = "load_skill"

var skillNameRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Skill is one named set of instructions.
type Skill struct {
	Name        string
	Description string
	body        string
	fsys        fs.FS  // the skill's folder, for resource files; nil for inline skills
	dir         string // path of that folder inside fsys
}

// NewSkill builds a skill in code.
func NewSkill(name, description, instructions string) Skill {
	return Skill{Name: name, Description: description, body: strings.TrimSpace(instructions)}
}

// Instructions returns the skill's full text (the SKILL.md body).
func (s Skill) Instructions() string { return s.body }

// Files lists the resource files shipped in the skill's folder, besides
// SKILL.md, as paths relative to it. Inline skills have none.
func (s Skill) Files() []string {
	if s.fsys == nil {
		return nil
	}
	var out []string
	_ = fs.WalkDir(s.fsys, s.dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(p, s.dir), "/")
		if rel != "" && rel != "SKILL.md" {
			out = append(out, rel)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// ReadFile returns one resource file from the skill's folder. The path is
// relative to the folder and cannot leave it.
func (s Skill) ReadFile(name string) (string, error) {
	if s.fsys == nil {
		return "", fmt.Errorf("ai: skill %q has no files", s.Name)
	}
	clean := path.Clean("/" + strings.TrimSpace(name))[1:]
	if clean == "" || clean == "SKILL.md" {
		return s.body, nil
	}
	data, err := fs.ReadFile(s.fsys, path.Join(s.dir, clean))
	if err != nil {
		return "", fmt.Errorf("ai: skill %q has no file %q (files: %s)", s.Name, clean, strings.Join(s.Files(), ", "))
	}
	if len(data) > MaxSkillFileBytes {
		return "", fmt.Errorf("ai: skill %q file %q is larger than %d KB", s.Name, clean, MaxSkillFileBytes>>10)
	}
	return string(data), nil
}

func (s Skill) validate() error {
	if !skillNameRe.MatchString(s.Name) || len(s.Name) > MaxSkillNameLen {
		return fmt.Errorf("ai: skill name %q must be lowercase letters, digits and hyphens, at most %d characters", s.Name, MaxSkillNameLen)
	}
	if strings.TrimSpace(s.Description) == "" {
		return fmt.Errorf("ai: skill %q needs a description: it is how the model decides to load it", s.Name)
	}
	if len(s.Description) > MaxSkillDescriptionLen {
		return fmt.Errorf("ai: skill %q description is longer than %d characters", s.Name, MaxSkillDescriptionLen)
	}
	if s.body == "" {
		return fmt.Errorf("ai: skill %q has no instructions", s.Name)
	}
	return nil
}

// SkillSet is a collection of skills with unique names.
type SkillSet struct {
	skills []Skill
}

// NewSkillSet groups skills, rejecting invalid ones and duplicate names.
func NewSkillSet(skills ...Skill) (*SkillSet, error) {
	set := &SkillSet{}
	seen := map[string]bool{}
	for _, s := range skills {
		if err := s.validate(); err != nil {
			return nil, err
		}
		if seen[s.Name] {
			return nil, fmt.Errorf("ai: two skills are named %q", s.Name)
		}
		seen[s.Name] = true
		set.skills = append(set.skills, s)
	}
	sort.Slice(set.skills, func(i, j int) bool { return set.skills[i].Name < set.skills[j].Name })
	return set, nil
}

// Skills returns the skills, sorted by name.
func (s *SkillSet) Skills() []Skill {
	if s == nil {
		return nil
	}
	return append([]Skill(nil), s.skills...)
}

// Get returns the skill with this name.
func (s *SkillSet) Get(name string) (Skill, bool) {
	if s == nil {
		return Skill{}, false
	}
	name = strings.TrimSpace(name)
	for _, sk := range s.skills {
		if sk.Name == name || strings.EqualFold(sk.Name, name) {
			return sk, true
		}
	}
	return Skill{}, false
}

// Len returns the number of skills.
func (s *SkillSet) Len() int {
	if s == nil {
		return 0
	}
	return len(s.skills)
}

// LoadSkills reads every skill under dir in fsys: each <dir>/<name>/SKILL.md
// is a skill whose folder may hold resource files, and each <dir>/<name>.md
// is a single-file skill. Use it with embed.FS to compile skills into the
// binary, or with os.DirFS (see LoadSkillsDir).
func LoadSkills(fsys fs.FS, dir string) (*SkillSet, error) {
	dir = path.Clean(strings.TrimPrefix(dir, "./"))
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("ai: read skills in %q: %w", dir, err)
	}
	var skills []Skill
	for _, e := range entries {
		var (
			file, folder string
		)
		switch {
		case e.IsDir():
			folder = path.Join(dir, e.Name())
			file = path.Join(folder, "SKILL.md")
			if _, err := fs.Stat(fsys, file); err != nil {
				continue // a folder without SKILL.md is not a skill
			}
		case strings.HasSuffix(e.Name(), ".md") && !strings.EqualFold(e.Name(), "README.md"):
			file = path.Join(dir, e.Name())
		default:
			continue
		}
		data, err := fs.ReadFile(fsys, file)
		if err != nil {
			return nil, fmt.Errorf("ai: read skill %q: %w", file, err)
		}
		if len(data) > MaxSkillFileBytes {
			return nil, fmt.Errorf("ai: skill %q is larger than %d KB", file, MaxSkillFileBytes>>10)
		}
		name, desc, body, err := ParseSkill(string(data))
		if err != nil {
			return nil, fmt.Errorf("ai: skill %q: %w", file, err)
		}
		if name == "" {
			if folder != "" {
				name = e.Name()
			} else {
				name = strings.TrimSuffix(e.Name(), ".md")
			}
		}
		sk := NewSkill(name, desc, body)
		if folder != "" {
			sk.fsys, sk.dir = fsys, folder
		}
		skills = append(skills, sk)
	}
	return NewSkillSet(skills...)
}

// LoadSkillsDir reads skills from a directory on disk.
func LoadSkillsDir(dir string) (*SkillSet, error) {
	return LoadSkills(os.DirFS(dir), ".")
}

// MustLoadSkills is LoadSkills that panics, for package-level setup.
func MustLoadSkills(fsys fs.FS, dir string) *SkillSet {
	set, err := LoadSkills(fsys, dir)
	if err != nil {
		panic(err)
	}
	return set
}

// ParseSkill splits a SKILL.md into its frontmatter name and description
// and its body. A file without frontmatter is an error, since the
// description is what the model chooses skills by.
func ParseSkill(content string) (name, description, body string, err error) {
	return skillmd.Parse(content)
}

// skillIndex is the system-prompt section that lists the agent's skills.
func skillIndex(set *SkillSet) string {
	if set.Len() == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Skills\n\n")
	b.WriteString("You have skills: detailed instructions for specific tasks. When a request matches a skill's description, call the " + SkillToolName + " tool with its name and follow what it returns before you answer. Load only skills that match. A skill may mention files; load one with " + SkillToolName + " and the file argument.\n\n")
	for _, s := range set.skills {
		b.WriteString("- " + s.Name + ": " + s.Description + "\n")
	}
	return b.String()
}

// skillTool is the load_skill tool for an agent's skills.
func skillTool(set *SkillSet) *Tool {
	type input struct {
		Name string `json:"name" description:"The skill's name, from the Skills list."`
		File string `json:"file,omitempty" description:"Optional: a file the skill mentions, relative to the skill."`
	}
	t, err := NewTool(SkillToolName).
		Desc("Load a skill's full instructions, or one of its files, before working on a task the skill covers.").
		Handler(func(_ context.Context, in input) (string, error) {
			sk, ok := set.Get(in.Name)
			if !ok {
				names := make([]string, 0, set.Len())
				for _, s := range set.skills {
					names = append(names, s.Name)
				}
				return "", fmt.Errorf("no skill named %q; skills: %s", in.Name, strings.Join(names, ", "))
			}
			if strings.TrimSpace(in.File) != "" {
				return sk.ReadFile(in.File)
			}
			text := sk.Instructions()
			if files := sk.Files(); len(files) > 0 {
				text += "\n\n(Files in this skill: " + strings.Join(files, ", ") + ")"
			}
			return text, nil
		}).
		Build()
	if err != nil {
		panic(err) // the handler signature is fixed above
	}
	return t
}
