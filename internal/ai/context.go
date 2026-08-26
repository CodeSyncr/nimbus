package ai

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ProjectContext contains scanned information about the current Nimbus project.
type ProjectContext struct {
	AppRoot        string   `json:"app_root"`
	ProjectName    string   `json:"project_name"`
	GoModName      string   `json:"go_mod_name,omitempty"`
	GoVersion      string   `json:"go_version,omitempty"`
	NimbusModules  []string `json:"nimbus_modules,omitempty"`
	NimbusJSON     string   `json:"nimbus_json,omitempty"`
	DirectoryTree  string   `json:"directory_tree"`
	GitBranch      string   `json:"git_branch,omitempty"`
	GitDiffSummary string   `json:"git_diff_summary,omitempty"`
	RootFiles      []string `json:"root_files,omitempty"`
	Models         []string `json:"models,omitempty"`
	Controllers    []string `json:"controllers,omitempty"`
	Migrations     []string `json:"migrations,omitempty"`
	RoutesSummary  string   `json:"routes_summary,omitempty"`
	Skills         []Skill  `json:"skills,omitempty"`
	// ActiveSkillFrame holds the most recently loaded skill so the server can
	// keep it in the system prompt rather than the message history.
	ActiveSkillFrame string `json:"active_skill_frame,omitempty"`
	// Instructions holds project-level guidance for the agent, read from
	// AGENTS.md / NIMBUS.md / CLAUDE.md / .nimbus/instructions.md. It is the
	// project's persistent memory: conventions, do's and don'ts, commands.
	Instructions string `json:"instructions,omitempty"`
	// InstructionFiles lists which instruction files were found.
	InstructionFiles []string `json:"instruction_files,omitempty"`
	// Stack summarises non-Go tooling detected (package.json, Vite, Tailwind…).
	Stack []string `json:"stack,omitempty"`
	// OS is the host operating system, so shell commands can be phrased correctly.
	OS string `json:"os,omitempty"`
}

// instructionFileCandidates are checked in order; every one that exists is
// included so teams can keep both a generic AGENTS.md and Nimbus-specific notes.
var instructionFileCandidates = []string{
	"AGENTS.md",
	"NIMBUS.md",
	"CLAUDE.md",
	filepath.Join(".nimbus", "instructions.md"),
}

const maxInstructionBytes = 12 * 1024

// ScanProject scans the given project directory and constructs a ProjectContext.
func ScanProject(appRoot string) (*ProjectContext, error) {
	absRoot, err := filepath.Abs(appRoot)
	if err != nil {
		absRoot = appRoot
	}

	ctx := &ProjectContext{
		AppRoot:       absRoot,
		ProjectName:   filepath.Base(absRoot),
		NimbusModules: make([]string, 0),
		Models:        make([]string, 0),
		Controllers:   make([]string, 0),
		Migrations:    make([]string, 0),
		Skills:        make([]Skill, 0),
		OS:            hostOS(),
	}

	// 0. Top-level files (quick orientation for non-Nimbus projects too)
	ctx.RootFiles = scanRootFiles(absRoot)

	// 1. Read go.mod
	goModPath := filepath.Join(absRoot, "go.mod")
	if data, err := os.ReadFile(goModPath); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "module ") {
				ctx.GoModName = strings.TrimSpace(strings.TrimPrefix(line, "module"))
			} else if strings.HasPrefix(line, "go ") {
				ctx.GoVersion = strings.TrimSpace(strings.TrimPrefix(line, "go"))
			}
		}
	}

	// 2. Read nimbus.json if present
	nimbusJSONPath := filepath.Join(absRoot, "nimbus.json")
	if data, err := os.ReadFile(nimbusJSONPath); err == nil {
		ctx.NimbusJSON = string(data)
	}

	// 3. Detect Nimbus sub-modules in Go source files
	ctx.NimbusModules = detectNimbusModules(absRoot)

	// 4. Scan key application components
	ctx.Models = scanGoFiles(filepath.Join(absRoot, "app", "models"))
	ctx.Controllers = scanGoFiles(filepath.Join(absRoot, "app", "controllers"))
	ctx.Migrations = scanGoFiles(filepath.Join(absRoot, "database", "migrations"))

	// 5. Read start/routes.go snippet if available
	routesPath := filepath.Join(absRoot, "start", "routes.go")
	if data, err := os.ReadFile(routesPath); err == nil {
		ctx.RoutesSummary = summarizeRoutes(string(data))
	}

	// 6. Build directory tree (max depth 3, skipping vendor, .git, etc.)
	ctx.DirectoryTree = buildDirectoryTree(absRoot, 3)

	// 7. Git diff & branch summary
	ctx.GitBranch = getGitBranch(absRoot)
	ctx.GitDiffSummary = getGitDiffSummary(absRoot)

	// 8. Discover and load active agent skills
	if skills, sErr := LoadSkills(absRoot); sErr == nil {
		ctx.Skills = skills
	}

	// 9. Project instructions (persistent agent memory) and stack hints
	ctx.Instructions, ctx.InstructionFiles = loadProjectInstructions(absRoot)
	ctx.Stack = detectStack(absRoot)

	return ctx, nil
}

// Refresh re-reads the cheap, fast-changing parts of the context (git state,
// directory tree, instructions) so later phases see files created earlier.
func (p *ProjectContext) Refresh() {
	if p == nil || p.AppRoot == "" {
		return
	}
	p.DirectoryTree = buildDirectoryTree(p.AppRoot, 3)
	p.RootFiles = scanRootFiles(p.AppRoot)
	p.GitBranch = getGitBranch(p.AppRoot)
	p.GitDiffSummary = getGitDiffSummary(p.AppRoot)
	p.Models = scanGoFiles(filepath.Join(p.AppRoot, "app", "models"))
	p.Controllers = scanGoFiles(filepath.Join(p.AppRoot, "app", "controllers"))
	p.Migrations = scanGoFiles(filepath.Join(p.AppRoot, "database", "migrations"))
	if data, err := os.ReadFile(filepath.Join(p.AppRoot, "start", "routes.go")); err == nil {
		p.RoutesSummary = summarizeRoutes(string(data))
	}
	p.Instructions, p.InstructionFiles = loadProjectInstructions(p.AppRoot)
}

func hostOS() string {
	switch runtime.GOOS {
	case "darwin":
		return "macos"
	default:
		return runtime.GOOS
	}
}

func scanRootFiles(root string) []string {
	files := make([]string, 0)
	entries, err := os.ReadDir(root)
	if err != nil {
		return files
	}
	for _, e := range entries {
		if !e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			files = append(files, e.Name())
		}
	}
	return files
}

// loadProjectInstructions concatenates every instruction file present.
func loadProjectInstructions(root string) (string, []string) {
	var sb strings.Builder
	var found []string
	for _, rel := range instructionFileCandidates {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		text := strings.TrimSpace(string(data))
		if text == "" {
			continue
		}
		if len(text) > maxInstructionBytes {
			text = text[:maxInstructionBytes] + "\n… (truncated)"
		}
		found = append(found, rel)
		sb.WriteString(fmt.Sprintf("### %s\n%s\n\n", filepath.ToSlash(rel), text))
	}
	return strings.TrimSpace(sb.String()), found
}

// detectStack reports non-Go tooling so the agent respects the real stack.
func detectStack(root string) []string {
	var stack []string
	exists := func(rel string) bool {
		_, err := os.Stat(filepath.Join(root, rel))
		return err == nil
	}
	if exists("go.mod") {
		stack = append(stack, "go")
	}
	if exists("package.json") {
		stack = append(stack, "node")
		if data, err := os.ReadFile(filepath.Join(root, "package.json")); err == nil {
			s := string(data)
			for _, dep := range []string{"vite", "react", "vue", "svelte", "@inertiajs", "tailwindcss", "typescript"} {
				if strings.Contains(s, "\""+dep) {
					stack = append(stack, strings.TrimPrefix(dep, "@"))
				}
			}
		}
	}
	if exists("inertia") {
		stack = append(stack, "inertia")
	}
	if exists(filepath.Join("resources", "views")) {
		stack = append(stack, "html-templates")
	}
	if exists("Dockerfile") {
		stack = append(stack, "docker")
	}
	return stack
}

// FormatSystemContext formats the ProjectContext into a rich markdown block for AI system prompts.
func (p *ProjectContext) FormatSystemContext() string {
	var sb strings.Builder

	sb.WriteString("## Project Context\n")
	sb.WriteString(fmt.Sprintf("- **Project Root:** `%s`\n", p.AppRoot))
	if p.GoModName != "" {
		sb.WriteString(fmt.Sprintf("- **Module:** `%s`\n", p.GoModName))
	}
	if p.GoVersion != "" {
		sb.WriteString(fmt.Sprintf("- **Go Version:** `%s`\n", p.GoVersion))
	}
	if p.OS != "" {
		sb.WriteString(fmt.Sprintf("- **Host OS:** `%s`\n", p.OS))
	}
	if len(p.Stack) > 0 {
		sb.WriteString(fmt.Sprintf("- **Stack:** %s\n", strings.Join(p.Stack, ", ")))
	}
	if p.GitBranch != "" {
		sb.WriteString(fmt.Sprintf("- **Git Branch:** `%s`\n", p.GitBranch))
	}

	if len(p.RootFiles) > 0 {
		sb.WriteString(fmt.Sprintf("- **Root Directory Files:** `%s`\n", strings.Join(p.RootFiles, "`, `")))
	}

	if len(p.NimbusModules) > 0 {
		sb.WriteString(fmt.Sprintf("- **Imported Nimbus Modules:** %s\n", strings.Join(p.NimbusModules, ", ")))
	}

	if len(p.Models) > 0 {
		sb.WriteString(fmt.Sprintf("- **Models:** `%s`\n", strings.Join(p.Models, "`, `")))
	}
	if len(p.Controllers) > 0 {
		sb.WriteString(fmt.Sprintf("- **Controllers:** `%s`\n", strings.Join(p.Controllers, "`, `")))
	}
	if len(p.Migrations) > 0 {
		sb.WriteString(fmt.Sprintf("- **Migrations Count:** %d\n", len(p.Migrations)))
	}

	if p.GitDiffSummary != "" {
		sb.WriteString("\n### Uncommitted Git Changes (`git diff --stat`):\n```\n")
		sb.WriteString(p.GitDiffSummary)
		sb.WriteString("\n```\n")
	}

	if p.RoutesSummary != "" {
		sb.WriteString("\n### Registered Routes Snippet (`start/routes.go`):\n```go\n")
		sb.WriteString(p.RoutesSummary)
		sb.WriteString("\n```\n")
	}

	sb.WriteString("\n### Directory Layout:\n```\n")
	sb.WriteString(p.DirectoryTree)
	sb.WriteString("\n```\n\n")

	if p.Instructions != "" {
		sb.WriteString(fmt.Sprintf("### Project Instructions (%s)\n%s\n\n", strings.Join(p.InstructionFiles, ", "), p.Instructions))
	}

	if len(p.Skills) > 0 {
		sb.WriteString(FormatSkillsSummary(p.Skills))
	}

	if p.ActiveSkillFrame != "" {
		sb.WriteString("\n### Active System Skill Frame\n")
		sb.WriteString(p.ActiveSkillFrame)
		sb.WriteString("\n\n")
	}

	return sb.String()
}

// detectNimbusModules checks for nimbus/str, nimbus/collect, nimbus/timex, nimbus/pipeline, etc.
func detectNimbusModules(appRoot string) []string {
	found := make(map[string]bool)
	nimbusPrefixes := []string{
		"github.com/CodeSyncr/nimbus/str",
		"github.com/CodeSyncr/nimbus/collect",
		"github.com/CodeSyncr/nimbus/timex",
		"github.com/CodeSyncr/nimbus/pipeline",
		"github.com/CodeSyncr/nimbus/http",
		"github.com/CodeSyncr/nimbus/router",
		"github.com/CodeSyncr/nimbus/database",
		"github.com/CodeSyncr/nimbus/shield",
		"github.com/CodeSyncr/nimbus/packages/shield",
		"github.com/CodeSyncr/nimbus/auth",
		"github.com/CodeSyncr/nimbus/plugins/cashier",
		"github.com/CodeSyncr/nimbus/plugins/ai",
		"github.com/CodeSyncr/nimbus/plugins/livewire",
	}

	_ = filepath.Walk(appRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			name := info.Name()
			if name == "vendor" || name == ".git" || name == "node_modules" || name == ".nimbus" || name == "storage" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".go") {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		strContent := string(content)
		for _, mod := range nimbusPrefixes {
			if strings.Contains(strContent, mod) {
				shortName := strings.TrimPrefix(mod, "github.com/CodeSyncr/nimbus/")
				found[shortName] = true
			}
		}
		return nil
	})

	var result []string
	for k := range found {
		result = append(result, k)
	}
	return result
}

func scanGoFiles(dir string) []string {
	var files []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		return files
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") && !strings.HasSuffix(e.Name(), "_test.go") {
			files = append(files, e.Name())
		}
	}
	return files
}

func summarizeRoutes(content string) string {
	lines := strings.Split(content, "\n")
	var relevant []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "app.Router.") || strings.HasPrefix(trimmed, "api.") || strings.HasPrefix(trimmed, "v1.") {
			relevant = append(relevant, line)
		}
	}
	if len(relevant) > 40 {
		relevant = relevant[:40]
		relevant = append(relevant, "\t// ... (more routes truncated — read start/routes.go for the full list)")
	}
	return strings.Join(relevant, "\n")
}

func buildDirectoryTree(root string, maxDepth int) string {
	var sb strings.Builder
	sb.WriteString(filepath.Base(root) + "/\n")

	var walk func(dir string, prefix string, depth int)
	walk = func(dir string, prefix string, depth int) {
		if depth > maxDepth {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}

		var filtered []os.DirEntry
		for _, e := range entries {
			name := e.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "bin" || name == "storage" || name == "tmp" {
				continue
			}
			filtered = append(filtered, e)
		}

		for i, e := range filtered {
			isLast := i == len(filtered)-1
			branch := "├── "
			nextPrefix := prefix + "│   "
			if isLast {
				branch = "└── "
				nextPrefix = prefix + "    "
			}

			if e.IsDir() {
				sb.WriteString(prefix + branch + e.Name() + "/\n")
				walk(filepath.Join(dir, e.Name()), nextPrefix, depth+1)
			} else {
				sb.WriteString(prefix + branch + e.Name() + "\n")
			}
		}
	}

	walk(root, "", 1)
	return sb.String()
}

func getGitBranch(root string) string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = root
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err == nil {
		return strings.TrimSpace(out.String())
	}
	return ""
}

func getGitDiffSummary(root string) string {
	cmd := exec.Command("git", "diff", "--stat")
	cmd.Dir = root
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err == nil {
		return strings.TrimSpace(out.String())
	}
	return ""
}
