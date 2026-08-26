package ai

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

// ToolDefinition describes a tool schema exposed to the AI agent.
type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

// ToolExecutor executes agent tool requests.
type ToolExecutor struct {
	AppRoot string
	// CommandTimeout bounds a single bash invocation.
	CommandTimeout time.Duration
}

// Limits applied to tool output so a single result cannot blow the context.
const (
	maxReadLines       = 1500
	maxReadBytes       = 160 * 1024
	maxGrepMatches     = 100
	maxGrepLineChars   = 300
	maxGrepFileBytes   = 2 * 1024 * 1024
	maxFindResults     = 200
	maxListEntries     = 300
	maxCommandOutput   = 16 * 1024
	defaultCmdTimeout  = 120 * time.Second
	skippedDirsPattern = ".git|node_modules|vendor|storage|.nimbus|tmp|dist|.next|__pycache__"
)

var skippedDirs = func() map[string]bool {
	m := map[string]bool{}
	for _, d := range strings.Split(skippedDirsPattern, "|") {
		m[d] = true
	}
	return m
}()

// NewToolExecutor creates a tool executor sandboxed to appRoot.
func NewToolExecutor(appRoot string) *ToolExecutor {
	abs, err := filepath.Abs(appRoot)
	if err != nil {
		abs = appRoot
	}
	return &ToolExecutor{AppRoot: abs, CommandTimeout: defaultCmdTimeout}
}

// GetToolDefinitions returns the canonical tool schemas for the AI agent.
func (t *ToolExecutor) GetToolDefinitions() []ToolDefinition {
	return append(t.ReadOnlyToolDefinitions(), t.writeToolDefinitions()...)
}

// ReadOnlyToolDefinitions returns the tools that inspect the workspace
// without changing files. Used for the exploration and planning phases.
func (t *ToolExecutor) ReadOnlyToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "read_file",
			Description: "Read a file in the workspace. Returns the full content for small files; for large files pass start_line/end_line to read a range. Always read a file before editing it.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":       map[string]any{"type": "string", "description": "Path relative to the workspace root."},
					"start_line": map[string]any{"type": "integer", "description": "Optional 1-based first line to return."},
					"end_line":   map[string]any{"type": "integer", "description": "Optional 1-based last line to return (inclusive)."},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "list_dir",
			Description: "List files and subdirectories. Pass depth (1-3) to include nested entries.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":  map[string]any{"type": "string", "description": "Directory path relative to the workspace root ('.' for root)."},
					"depth": map[string]any{"type": "integer", "description": "How many levels deep to list (default 1, max 3)."},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "find_files",
			Description: "Find files by glob pattern, e.g. '**/*.go', 'app/controllers/*.go', '**/routes*'. Use this to locate files before reading them.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pattern": map[string]any{"type": "string", "description": "Glob pattern matched against workspace-relative paths. Supports ** for any directories."},
					"path":    map[string]any{"type": "string", "description": "Optional subdirectory to search in (default '.')."},
				},
				"required": []string{"pattern"},
			},
		},
		{
			Name:        "grep",
			Description: "Search file contents with a regular expression (Go RE2 syntax; falls back to a case-insensitive literal search if the pattern is invalid). Returns 'path:line: text' matches.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pattern": map[string]any{"type": "string", "description": "Regex or literal text to search for."},
					"path":    map[string]any{"type": "string", "description": "Optional subdirectory to limit the search to (default '.')."},
					"include": map[string]any{"type": "string", "description": "Optional file-name glob filter such as '*.go' or '*.html'."},
				},
				"required": []string{"pattern"},
			},
		},
		{
			Name:        "bash",
			Description: "Run a shell command in the workspace root (e.g. 'go build ./...', 'go test ./...', 'git log --oneline -5', 'nimbus make:model Post'). Output is captured; long output is truncated. Network exfiltration and destructive system commands are blocked.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{"type": "string", "description": "Command string to run."},
				},
				"required": []string{"command"},
			},
		},
		{
			Name:        "load_skill",
			Description: "Load the full instructions and reference patterns of a specialized agent skill by name. Only load skills whose description matches the task at hand.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"skill_name": map[string]any{"type": "string", "description": "Exact skill name from the Available Skills list (e.g. 'nimbus-expert', 'database-migrations', 'livewire-components')."},
				},
				"required": []string{"skill_name"},
			},
		},
		{
			Name:        "query_skill",
			Description: "Retrieve only the sections of a skill that match a topic (e.g. 'validation', 'routing', 'wire:model') instead of loading the whole skill. Keeps context small.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"skill_name": map[string]any{"type": "string", "description": "Exact skill name from the Available Skills list."},
					"query":      map[string]any{"type": "string", "description": "Topic or keyword to retrieve."},
				},
				"required": []string{"skill_name", "query"},
			},
		},
	}
}

func (t *ToolExecutor) writeToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "write_file",
			Description: "Create a new file, or fully replace the content of an existing file you have already read in this conversation. Parent directories are created automatically. Prefer edit_file for changes to existing files.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    map[string]any{"type": "string", "description": "Path relative to the workspace root."},
					"content": map[string]any{"type": "string", "description": "Complete file content."},
				},
				"required": []string{"path", "content"},
			},
		},
		{
			Name:        "edit_file",
			Description: "Make a targeted change to an existing file by replacing an exact, unique substring. Include enough surrounding lines in target to make it unique. Read the file first so target matches exactly (whitespace included).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":        map[string]any{"type": "string", "description": "Path relative to the workspace root."},
					"target":      map[string]any{"type": "string", "description": "Exact text to replace. Must occur exactly once unless replace_all is true."},
					"replacement": map[string]any{"type": "string", "description": "Replacement text."},
					"replace_all": map[string]any{"type": "boolean", "description": "Replace every occurrence instead of requiring a unique match."},
				},
				"required": []string{"path", "target", "replacement"},
			},
		},
		{
			Name:        "delete_file",
			Description: "Delete a file from the workspace.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string", "description": "Path relative to the workspace root."},
				},
				"required": []string{"path"},
			},
		},
	}
}

// ExecuteTool runs the requested tool and returns output string and optional diff string.
func (t *ToolExecutor) ExecuteTool(ctx context.Context, name string, args map[string]any) (output string, diff string, err error) {
	switch name {
	case "read_file", "read":
		path, _ := args["path"].(string)
		out, err := t.ReadFileRange(path, intArg(args, "start_line"), intArg(args, "end_line"))
		return out, "", err

	case "load_skill", "read_skill":
		skillName, _ := args["skill_name"].(string)
		if skillName == "" {
			skillName, _ = args["name"].(string)
		}
		out, err := t.LoadSkill(skillName)
		return out, "", err

	case "query_skill":
		skillName, _ := args["skill_name"].(string)
		if skillName == "" {
			skillName, _ = args["name"].(string)
		}
		query, _ := args["query"].(string)
		out, err := t.QuerySkill(skillName, query)
		return out, "", err

	case "list_dir":
		path, _ := args["path"].(string)
		if path == "" {
			path = "."
		}
		out, err := t.ListDirDepth(path, intArg(args, "depth"))
		return out, "", err

	case "find_files", "glob":
		pattern, _ := args["pattern"].(string)
		path, _ := args["path"].(string)
		out, err := t.FindFiles(pattern, path)
		return out, "", err

	case "grep", "search":
		pattern, _ := args["pattern"].(string)
		path, _ := args["path"].(string)
		include, _ := args["include"].(string)
		if path == "" {
			path = "."
		}
		out, err := t.GrepFiltered(pattern, path, include)
		return out, "", err

	case "write_file", "create_file", "create", "write":
		path, _ := args["path"].(string)
		content, _ := args["content"].(string)
		return t.WriteFile(path, content)

	case "edit_file", "edit":
		path, _ := args["path"].(string)
		target, _ := args["target"].(string)
		replacement, _ := args["replacement"].(string)
		replaceAll, _ := args["replace_all"].(bool)
		return t.EditFileAll(path, target, replacement, replaceAll)

	case "delete_file", "delete":
		path, _ := args["path"].(string)
		out, diff, err := t.DeleteFile(path)
		return out, diff, err

	case "bash", "run_command", "command", "shell":
		cmdStr, _ := args["command"].(string)
		out, err := t.Bash(ctx, cmdStr)
		return out, "", err

	default:
		return "", "", fmt.Errorf("unknown tool: %s", name)
	}
}

func intArg(args map[string]any, key string) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	case string:
		var n int
		_, _ = fmt.Sscanf(strings.TrimSpace(v), "%d", &n)
		return n
	}
	return 0
}

// resolvePath ensures path stays strictly inside AppRoot.
func (t *ToolExecutor) resolvePath(relPath string) (string, error) {
	relPath = strings.TrimSpace(relPath)
	if relPath == "" {
		return "", errors.New("path cannot be empty")
	}
	clean := filepath.Clean(relPath)
	if filepath.IsAbs(clean) {
		rel, err := filepath.Rel(t.AppRoot, clean)
		if err != nil || strings.HasPrefix(rel, "..") {
			return "", fmt.Errorf("access denied: path '%s' is outside the project root", relPath)
		}
		clean = rel
	}
	fullPath := filepath.Join(t.AppRoot, clean)
	rel, err := filepath.Rel(t.AppRoot, fullPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("access denied: path '%s' traverses outside project root", relPath)
	}
	return fullPath, nil
}

func (t *ToolExecutor) relPath(full string) string {
	rel, err := filepath.Rel(t.AppRoot, full)
	if err != nil {
		return full
	}
	return filepath.ToSlash(rel)
}

// ReadFile reads a whole file (subject to size limits).
func (t *ToolExecutor) ReadFile(relPath string) (string, error) {
	return t.ReadFileRange(relPath, 0, 0)
}

// ReadFileRange reads a file, optionally restricted to a 1-based inclusive
// line range. Oversized reads are truncated with a hint to use ranges.
func (t *ToolExecutor) ReadFileRange(relPath string, startLine, endLine int) (string, error) {
	fullPath, err := t.resolvePath(relPath)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(fullPath)
	if err != nil {
		return "", fmt.Errorf("read error: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory; use list_dir", relPath)
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("read error: %w", err)
	}
	if isBinary(data) {
		return fmt.Sprintf("File: %s (%d bytes, binary — content not shown)", relPath, len(data)), nil
	}
	lines := strings.Split(string(data), "\n")
	total := len(lines)

	if startLine > 0 || endLine > 0 {
		if startLine <= 0 {
			startLine = 1
		}
		if endLine <= 0 || endLine > total {
			endLine = total
		}
		if startLine > total {
			return "", fmt.Errorf("start_line %d is beyond end of file (%d lines)", startLine, total)
		}
		if endLine < startLine {
			return "", fmt.Errorf("end_line must be >= start_line")
		}
		if endLine-startLine+1 > maxReadLines {
			endLine = startLine + maxReadLines - 1
		}
		chunk := strings.Join(lines[startLine-1:endLine], "\n")
		return fmt.Sprintf("File: %s (%d lines total, showing %d-%d)\n\n%s", relPath, total, startLine, endLine, chunk), nil
	}

	if total > maxReadLines || len(data) > maxReadBytes {
		shown := lines
		if len(shown) > maxReadLines {
			shown = shown[:maxReadLines]
		}
		chunk := strings.Join(shown, "\n")
		if len(chunk) > maxReadBytes {
			chunk = chunk[:maxReadBytes]
		}
		return fmt.Sprintf("File: %s (%d lines total, TRUNCATED — showing lines 1-%d; use start_line/end_line to read more)\n\n%s",
			relPath, total, len(shown), chunk), nil
	}
	return fmt.Sprintf("File: %s (%d lines)\n\n%s", relPath, total, string(data)), nil
}

func isBinary(data []byte) bool {
	probe := data
	if len(probe) > 512 {
		probe = probe[:512]
	}
	return bytes.IndexByte(probe, 0) != -1
}

// LoadSkill loads the full content and documentation of a skill by name on demand.
func (t *ToolExecutor) LoadSkill(skillName string) (string, error) {
	skillName = strings.TrimSpace(skillName)
	if skillName == "" {
		return "", errors.New("skill_name cannot be empty")
	}

	body, err := ReadSkillContent(t.AppRoot, skillName)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("# Skill: %s\n\n%s", skillName, body), nil
}

// ReadSkill is an alias for LoadSkill.
func (t *ToolExecutor) ReadSkill(name string) (string, error) {
	return t.LoadSkill(name)
}

// QuerySkill reads specific sections or topics from a skill to keep context lightweight.
func (t *ToolExecutor) QuerySkill(skillName, query string) (string, error) {
	skillName = strings.TrimSpace(skillName)
	if skillName == "" {
		return "", errors.New("skill_name cannot be empty")
	}

	section, err := ReadSkillSection(t.AppRoot, skillName, query)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("# Skill Query: %s (Topic: %s)\n\n%s", skillName, query, section), nil
}

// ListDir lists a single directory level.
func (t *ToolExecutor) ListDir(relPath string) (string, error) {
	return t.ListDirDepth(relPath, 1)
}

// ListDirDepth lists a directory up to depth levels deep (1-3).
func (t *ToolExecutor) ListDirDepth(relPath string, depth int) (string, error) {
	fullPath, err := t.resolvePath(relPath)
	if err != nil {
		return "", err
	}
	if depth <= 0 {
		depth = 1
	}
	if depth > 3 {
		depth = 3
	}
	if _, err := os.Stat(fullPath); err != nil {
		return "", fmt.Errorf("list error: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Directory %s:\n", relPath))
	count := 0
	truncated := false

	var walk func(dir, indent string, level int)
	walk = func(dir, indent string, level int) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if truncated {
				return
			}
			name := e.Name()
			if skippedDirs[name] {
				continue
			}
			if count >= maxListEntries {
				truncated = true
				return
			}
			count++
			if e.IsDir() {
				sb.WriteString(fmt.Sprintf("%s[DIR]  %s/\n", indent, name))
				if level < depth {
					walk(filepath.Join(dir, name), indent+"  ", level+1)
				}
			} else {
				sb.WriteString(fmt.Sprintf("%s[FILE] %s\n", indent, name))
			}
		}
	}
	walk(fullPath, "  ", 1)
	if truncated {
		sb.WriteString(fmt.Sprintf("  ... (truncated at %d entries; list a subdirectory for more)\n", maxListEntries))
	}
	return sb.String(), nil
}

// FindFiles returns workspace-relative paths matching a glob pattern.
func (t *ToolExecutor) FindFiles(pattern, relPath string) (string, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return "", errors.New("pattern cannot be empty")
	}
	if relPath == "" {
		relPath = "."
	}
	fullPath, err := t.resolvePath(relPath)
	if err != nil {
		return "", err
	}
	re, err := globToRegexp(pattern)
	if err != nil {
		return "", err
	}

	var matches []string
	truncated := false
	_ = filepath.WalkDir(fullPath, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			if path != fullPath && skippedDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		rel := t.relPath(path)
		base := d.Name()
		if re.MatchString(rel) || (!strings.ContainsAny(pattern, "/\\") && re.MatchString(base)) {
			if len(matches) >= maxFindResults {
				truncated = true
				return errors.New("stop")
			}
			matches = append(matches, rel)
		}
		return nil
	})
	if len(matches) == 0 {
		return fmt.Sprintf("No files match '%s' under %s", pattern, relPath), nil
	}
	sort.Strings(matches)
	out := strings.Join(matches, "\n")
	if truncated {
		out += fmt.Sprintf("\n... (truncated at %d results; narrow the pattern)", maxFindResults)
	}
	return out, nil
}

// globToRegexp converts a glob (with ** support) into an anchored regexp
// that matches forward-slash relative paths.
func globToRegexp(glob string) (*regexp.Regexp, error) {
	glob = filepath.ToSlash(glob)
	glob = strings.TrimPrefix(glob, "./")
	var sb strings.Builder
	sb.WriteString("^")
	for i := 0; i < len(glob); i++ {
		c := glob[i]
		switch c {
		case '*':
			if i+1 < len(glob) && glob[i+1] == '*' {
				// "**/" matches zero or more directories; bare "**" matches anything.
				if i+2 < len(glob) && glob[i+2] == '/' {
					sb.WriteString("(?:.*/)?")
					i += 2
				} else {
					sb.WriteString(".*")
					i++
				}
			} else {
				sb.WriteString("[^/]*")
			}
		case '?':
			sb.WriteString("[^/]")
		case '.', '+', '(', ')', '|', '^', '$', '{', '}', '[', ']', '\\':
			sb.WriteString("\\")
			sb.WriteByte(c)
		default:
			sb.WriteByte(c)
		}
	}
	sb.WriteString("$")
	return regexp.Compile(sb.String())
}

// Grep searches file contents with a regex (no include filter).
func (t *ToolExecutor) Grep(pattern, relPath string) (string, error) {
	return t.GrepFiltered(pattern, relPath, "")
}

// GrepFiltered searches file contents, optionally restricted to files whose
// name matches the include glob.
func (t *ToolExecutor) GrepFiltered(pattern, relPath, include string) (string, error) {
	if strings.TrimSpace(pattern) == "" {
		return "", errors.New("pattern cannot be empty")
	}
	fullPath, err := t.resolvePath(relPath)
	if err != nil {
		return "", err
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		// Fallback to literal case-insensitive substring
		re, err = regexp.Compile("(?i)" + regexp.QuoteMeta(pattern))
		if err != nil {
			return "", err
		}
	}
	include = strings.TrimSpace(include)

	var matches []string
	truncated := false
	_ = filepath.WalkDir(fullPath, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			if path != fullPath && skippedDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if include != "" {
			if ok, _ := filepath.Match(include, d.Name()); !ok {
				return nil
			}
		}
		info, err := d.Info()
		if err != nil || info.Size() > maxGrepFileBytes {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil || isBinary(data) {
			return nil
		}
		rel := t.relPath(path)
		lines := strings.Split(string(data), "\n")
		for idx, l := range lines {
			if !re.MatchString(l) {
				continue
			}
			if len(matches) >= maxGrepMatches {
				truncated = true
				return errors.New("stop")
			}
			text := strings.TrimSpace(l)
			if len(text) > maxGrepLineChars {
				text = text[:maxGrepLineChars] + "…"
			}
			matches = append(matches, fmt.Sprintf("%s:%d: %s", rel, idx+1, text))
		}
		return nil
	})

	if len(matches) == 0 {
		return fmt.Sprintf("No matches found for '%s'", pattern), nil
	}
	out := strings.Join(matches, "\n")
	if truncated {
		out += fmt.Sprintf("\n... (truncated at %d matches; narrow the pattern, path, or include filter)", maxGrepMatches)
	}
	return out, nil
}

func (t *ToolExecutor) WriteFile(relPath, newContent string) (string, string, error) {
	fullPath, err := t.resolvePath(relPath)
	if err != nil {
		return "", "", err
	}

	oldContent := ""
	existed := false
	if data, err := os.ReadFile(fullPath); err == nil {
		oldContent = string(data)
		existed = true
	}

	diff := GenerateUnifiedDiff(oldContent, newContent, relPath)

	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return "", "", fmt.Errorf("mkdir error: %w", err)
	}
	if err := os.WriteFile(fullPath, []byte(newContent), 0644); err != nil {
		return "", "", fmt.Errorf("write error: %w", err)
	}

	if existed {
		return fmt.Sprintf("MODIFIED %s (%d bytes, replaced previous content)", relPath, len(newContent)), diff, nil
	}
	return fmt.Sprintf("Successfully wrote %d bytes to %s", len(newContent), relPath), diff, nil
}

// EditFile replaces a unique target substring.
func (t *ToolExecutor) EditFile(relPath, target, replacement string) (string, string, error) {
	return t.EditFileAll(relPath, target, replacement, false)
}

// EditFileAll replaces the target substring; with replaceAll every
// occurrence is replaced, otherwise the target must be unique.
func (t *ToolExecutor) EditFileAll(relPath, target, replacement string, replaceAll bool) (string, string, error) {
	fullPath, err := t.resolvePath(relPath)
	if err != nil {
		return "", "", err
	}
	if target == "" {
		return "", "", errors.New("target cannot be empty")
	}

	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", "", fmt.Errorf("file does not exist or cannot be read: %w (use write_file to create new files)", err)
	}
	oldContent := string(data)

	count := strings.Count(oldContent, target)
	if count == 0 {
		// Tolerate CRLF/LF mismatches, a common cause of false "not found".
		alt := strings.ReplaceAll(target, "\r\n", "\n")
		if alt != target && strings.Count(oldContent, alt) > 0 {
			target = alt
			count = strings.Count(oldContent, target)
		} else if strings.Contains(oldContent, "\r\n") {
			crlf := strings.ReplaceAll(alt, "\n", "\r\n")
			if strings.Count(oldContent, crlf) > 0 {
				target = crlf
				count = strings.Count(oldContent, target)
			}
		}
	}
	if count == 0 {
		return "", "", fmt.Errorf("target substring not found in %s — read the file and copy the exact text (including indentation) to replace", relPath)
	}
	if count > 1 && !replaceAll {
		return "", "", fmt.Errorf("target substring occurs %d times in %s — include more surrounding context to make it unique, or set replace_all", count, relPath)
	}

	var newContent string
	if replaceAll {
		newContent = strings.ReplaceAll(oldContent, target, replacement)
	} else {
		newContent = strings.Replace(oldContent, target, replacement, 1)
	}
	diff := GenerateUnifiedDiff(oldContent, newContent, relPath)

	if err := os.WriteFile(fullPath, []byte(newContent), 0644); err != nil {
		return "", "", fmt.Errorf("write error: %w", err)
	}

	if replaceAll {
		return fmt.Sprintf("Successfully edited %s (%d occurrences replaced)", relPath, count), diff, nil
	}
	return fmt.Sprintf("Successfully edited %s", relPath), diff, nil
}

func (t *ToolExecutor) DeleteFile(relPath string) (string, string, error) {
	fullPath, err := t.resolvePath(relPath)
	if err != nil {
		return "", "", err
	}
	data, err := os.ReadFile(fullPath)
	oldContent := ""
	if err == nil {
		oldContent = string(data)
	}
	diff := GenerateUnifiedDiff(oldContent, "", relPath)

	if err := os.Remove(fullPath); err != nil {
		return "", "", fmt.Errorf("remove error: %w", err)
	}
	return fmt.Sprintf("Deleted %s", relPath), diff, nil
}

// Bash runs a shell command and returns its combined output. A failing
// command is not an error at the tool level: the failure text is returned
// so the model can read and act on it.
func (t *ToolExecutor) Bash(ctx context.Context, commandStr string) (string, error) {
	out, _, err := t.runCommand(ctx, commandStr)
	return out, err
}

// RunCommand runs a command and reports whether it exited successfully.
func (t *ToolExecutor) RunCommand(ctx context.Context, commandStr string) (string, bool) {
	out, ok, err := t.runCommand(ctx, commandStr)
	if err != nil {
		return err.Error(), false
	}
	return out, ok
}

func (t *ToolExecutor) runCommand(ctx context.Context, commandStr string) (string, bool, error) {
	commandStr = strings.TrimSpace(commandStr)
	if commandStr == "" {
		return "", false, errors.New("empty command")
	}

	// Security Sandbox: Disallow dangerous network/exfiltration or system wipe commands
	lower := strings.ToLower(commandStr)
	blockedTokens := []string{
		"curl ", "wget ", "nc ", "netcat ", "ssh ", "scp ", "telnet ",
		"rm -rf /", "rm -rf /*", "mkfs", "dd if=", ":(){ :|:& };:",
		"format c:", "remove-item -recurse -force c:\\", "rd /s /q c:\\",
	}
	for _, tok := range blockedTokens {
		if strings.Contains(lower, tok) {
			return "", false, fmt.Errorf("security violation: command contains blocked token '%s'", strings.TrimSpace(tok))
		}
	}

	timeout := t.CommandTimeout
	if timeout <= 0 {
		timeout = defaultCmdTimeout
	}
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := shellCommand(cmdCtx, commandStr)
	cmd.Dir = t.AppRoot
	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf

	err := cmd.Run()
	output := truncateOutput(outBuf.String(), maxCommandOutput)
	if cmdCtx.Err() == context.DeadlineExceeded {
		return fmt.Sprintf("Command timed out after %s:\n%s", timeout, output), false, nil
	}
	if err != nil {
		return fmt.Sprintf("Command failed (%v):\n%s", err, output), false, nil
	}
	if strings.TrimSpace(output) == "" {
		return "Command executed successfully with 0 exit code (no output).", true, nil
	}
	return output, true, nil
}

// shellCommand picks a shell for the host: sh where available (Unix, Git
// Bash on Windows), otherwise cmd.exe so serve/build commands still run on a
// stock Windows install.
func shellCommand(ctx context.Context, commandStr string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("sh"); err == nil {
			return exec.CommandContext(ctx, "sh", "-c", commandStr)
		}
		return exec.CommandContext(ctx, "cmd", "/C", commandStr)
	}
	return exec.CommandContext(ctx, "sh", "-c", commandStr)
}

// truncateOutput keeps the head and tail of long output so both the start
// of a failure and its final error lines survive.
func truncateOutput(s string, max int) string {
	if len(s) <= max {
		return s
	}
	head := max / 4
	tail := max - head
	return s[:head] + fmt.Sprintf("\n... [%d bytes truncated] ...\n", len(s)-max) + s[len(s)-tail:]
}

// GenerateUnifiedDiff builds a simple unified line diff.
func GenerateUnifiedDiff(oldContent, newContent, filePath string) string {
	oldLines := strings.Split(oldContent, "\n")
	newLines := strings.Split(newContent, "\n")

	if oldContent == "" {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("--- /dev/null\n+++ %s\n", filePath))
		for _, l := range newLines {
			sb.WriteString("+" + l + "\n")
		}
		return sb.String()
	}

	if newContent == "" {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("--- %s\n+++ /dev/null\n", filePath))
		for _, l := range oldLines {
			sb.WriteString("-" + l + "\n")
		}
		return sb.String()
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("--- a/%s\n+++ b/%s\n", filePath, filePath))

	// Simple LCS diff for display
	lcs := computeLCS(oldLines, newLines)
	i, j := 0, 0
	for _, match := range lcs {
		for i < match.OldIdx {
			sb.WriteString("-" + oldLines[i] + "\n")
			i++
		}
		for j < match.NewIdx {
			sb.WriteString("+" + newLines[j] + "\n")
			j++
		}
		sb.WriteString(" " + oldLines[i] + "\n")
		i++
		j++
	}
	for i < len(oldLines) {
		sb.WriteString("-" + oldLines[i] + "\n")
		i++
	}
	for j < len(newLines) {
		sb.WriteString("+" + newLines[j] + "\n")
		j++
	}

	return sb.String()
}

type diffMatch struct {
	OldIdx int
	NewIdx int
}

func computeLCS(a, b []string) []diffMatch {
	n, m := len(a), len(b)
	if n > 2000 || m > 2000 {
		// Cap for performance on massive files
		return nil
	}
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}

	var matches []diffMatch
	i, j := n, m
	for i > 0 && j > 0 {
		if a[i-1] == b[j-1] {
			matches = append([]diffMatch{{OldIdx: i - 1, NewIdx: j - 1}}, matches...)
			i--
			j--
		} else if dp[i-1][j] >= dp[i][j-1] {
			i--
		} else {
			j--
		}
	}
	return matches
}
