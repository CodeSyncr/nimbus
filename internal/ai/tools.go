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
}

// NewToolExecutor creates a tool executor sandboxed to appRoot.
func NewToolExecutor(appRoot string) *ToolExecutor {
	abs, err := filepath.Abs(appRoot)
	if err != nil {
		abs = appRoot
	}
	return &ToolExecutor{AppRoot: abs}
}

// GetToolDefinitions returns the canonical tool schemas for the AI agent.
func (t *ToolExecutor) GetToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "read_file",
			Description: "Read the entire contents of a file in the workspace.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string", "description": "Relative path to file from workspace root."},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "list_dir",
			Description: "List files and subdirectories within a directory in the workspace.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string", "description": "Relative directory path (or '.' for root)."},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "grep",
			Description: "Search for a text pattern or regex across workspace files.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pattern": map[string]any{"type": "string", "description": "Search pattern or string."},
					"path":    map[string]any{"type": "string", "description": "Optional subdirectory to limit search to (default '.')."},
				},
				"required": []string{"pattern"},
			},
		},
		{
			Name:        "write_file",
			Description: "Write or overwrite a complete file with specified content.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    map[string]any{"type": "string", "description": "Relative path to target file."},
					"content": map[string]any{"type": "string", "description": "Complete file content to write."},
				},
				"required": []string{"path", "content"},
			},
		},
		{
			Name:        "edit_file",
			Description: "Replace an exact target string in an existing file with replacement content.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":        map[string]any{"type": "string", "description": "Relative path to file."},
					"target":      map[string]any{"type": "string", "description": "Exact text substring to be replaced."},
					"replacement": map[string]any{"type": "string", "description": "Replacement text."},
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
					"path": map[string]any{"type": "string", "description": "Relative path to file to remove."},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "bash",
			Description: "Execute a shell command sandboxed strictly to the project root (e.g. go build ./..., go test ./..., nimbus db:migrate). Network exfiltration commands are blocked.",
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
			Description: "Load the full instructions, reference implementations, and guidelines for a specialized agent skill by name on demand.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"skill_name": map[string]any{"type": "string", "description": "The exact name of the skill to load (e.g. 'frontend-design', 'theme-factory', 'nimbus-expert', 'go-architect', 'livewire-components', 'database-migrations', 'security-shield', 'test-engineer', 'mcp-builder')."},
				},
				"required": []string{"skill_name"},
			},
		},
	}
}

// ExecuteTool runs the requested tool and returns output string and optional diff string.
func (t *ToolExecutor) ExecuteTool(ctx context.Context, name string, args map[string]any) (output string, diff string, err error) {
	switch name {
	case "read_file":
		path, _ := args["path"].(string)
		out, err := t.ReadFile(path)
		return out, "", err

	case "load_skill", "read_skill":
		skillName, _ := args["skill_name"].(string)
		if skillName == "" {
			skillName, _ = args["name"].(string)
		}
		out, err := t.LoadSkill(skillName)
		return out, "", err

	case "list_dir":
		path, _ := args["path"].(string)
		if path == "" {
			path = "."
		}
		out, err := t.ListDir(path)
		return out, "", err

	case "grep":
		pattern, _ := args["pattern"].(string)
		path, _ := args["path"].(string)
		if path == "" {
			path = "."
		}
		out, err := t.Grep(pattern, path)
		return out, "", err

	case "write_file":
		path, _ := args["path"].(string)
		content, _ := args["content"].(string)
		return t.WriteFile(path, content)

	case "edit_file":
		path, _ := args["path"].(string)
		target, _ := args["target"].(string)
		replacement, _ := args["replacement"].(string)
		return t.EditFile(path, target, replacement)

	case "delete_file":
		path, _ := args["path"].(string)
		out, diff, err := t.DeleteFile(path)
		return out, diff, err

	case "bash":
		cmdStr, _ := args["command"].(string)
		out, err := t.Bash(ctx, cmdStr)
		return out, "", err

	default:
		return "", "", fmt.Errorf("unknown tool: %s", name)
	}
}

// sanitizePath ensures path stays strictly inside AppRoot.
func (t *ToolExecutor) resolvePath(relPath string) (string, error) {
	relPath = strings.TrimSpace(relPath)
	if relPath == "" {
		return "", errors.New("path cannot be empty")
	}
	clean := filepath.Clean(relPath)
	if filepath.IsAbs(clean) {
		clean = strings.TrimPrefix(clean, t.AppRoot)
		clean = strings.TrimPrefix(clean, "/")
	}
	fullPath := filepath.Join(t.AppRoot, clean)
	if !strings.HasPrefix(fullPath, t.AppRoot) {
		return "", fmt.Errorf("access denied: path '%s' traverses outside project root", relPath)
	}
	return fullPath, nil
}

func (t *ToolExecutor) ReadFile(relPath string) (string, error) {
	fullPath, err := t.resolvePath(relPath)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("read error: %w", err)
	}
	lines := strings.Split(string(data), "\n")
	return fmt.Sprintf("File: %s (%d lines)\n\n%s", relPath, len(lines), string(data)), nil
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

func (t *ToolExecutor) ListDir(relPath string) (string, error) {
	fullPath, err := t.resolvePath(relPath)
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return "", fmt.Errorf("list error: %w", err)
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Directory %s:\n", relPath))
	for _, e := range entries {
		name := e.Name()
		if name == ".git" || name == "node_modules" || name == "vendor" {
			continue
		}
		if e.IsDir() {
			sb.WriteString(fmt.Sprintf("  [DIR]  %s/\n", name))
		} else {
			sb.WriteString(fmt.Sprintf("  [FILE] %s\n", name))
		}
	}
	return sb.String(), nil
}

func (t *ToolExecutor) Grep(pattern, relPath string) (string, error) {
	fullPath, err := t.resolvePath(relPath)
	if err != nil {
		return "", err
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		// Fallback to literal case-insensitive substring
		pattern = regexp.QuoteMeta(pattern)
		re, err = regexp.Compile("(?i)" + pattern)
		if err != nil {
			return "", err
		}
	}

	var matches []string
	_ = filepath.Walk(fullPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			if info != nil && info.IsDir() {
				n := info.Name()
				if n == ".git" || n == "node_modules" || n == "vendor" || n == "storage" || n == ".nimbus" {
					return filepath.SkipDir
				}
			}
			return nil
		}
		rel, _ := filepath.Rel(t.AppRoot, path)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		lines := strings.Split(string(data), "\n")
		for idx, l := range lines {
			if re.MatchString(l) {
				matches = append(matches, fmt.Sprintf("%s:%d: %s", rel, idx+1, strings.TrimSpace(l)))
				if len(matches) >= 50 {
					return nil
				}
			}
		}
		return nil
	})

	if len(matches) == 0 {
		return fmt.Sprintf("No matches found for '%s'", pattern), nil
	}
	return strings.Join(matches, "\n"), nil
}

func (t *ToolExecutor) WriteFile(relPath, newContent string) (string, string, error) {
	fullPath, err := t.resolvePath(relPath)
	if err != nil {
		return "", "", err
	}

	oldContent := ""
	if data, err := os.ReadFile(fullPath); err == nil {
		oldContent = string(data)
	}

	diff := GenerateUnifiedDiff(oldContent, newContent, relPath)

	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return "", "", fmt.Errorf("mkdir error: %w", err)
	}
	if err := os.WriteFile(fullPath, []byte(newContent), 0644); err != nil {
		return "", "", fmt.Errorf("write error: %w", err)
	}

	return fmt.Sprintf("Successfully wrote %d bytes to %s", len(newContent), relPath), diff, nil
}

func (t *ToolExecutor) EditFile(relPath, target, replacement string) (string, string, error) {
	fullPath, err := t.resolvePath(relPath)
	if err != nil {
		return "", "", err
	}

	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", "", fmt.Errorf("file does not exist or cannot be read: %w", err)
	}
	oldContent := string(data)

	if !strings.Contains(oldContent, target) {
		return "", "", fmt.Errorf("target substring not found in %s", relPath)
	}

	// Count occurrences
	count := strings.Count(oldContent, target)
	if count > 1 {
		return "", "", fmt.Errorf("target substring occurs %d times in %s — provide a unique code block", count, relPath)
	}

	newContent := strings.Replace(oldContent, target, replacement, 1)
	diff := GenerateUnifiedDiff(oldContent, newContent, relPath)

	if err := os.WriteFile(fullPath, []byte(newContent), 0644); err != nil {
		return "", "", fmt.Errorf("write error: %w", err)
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

func (t *ToolExecutor) Bash(ctx context.Context, commandStr string) (string, error) {
	commandStr = strings.TrimSpace(commandStr)
	if commandStr == "" {
		return "", errors.New("empty command")
	}

	// Security Sandbox: Disallow dangerous network/exfiltration or system wipe commands
	lower := strings.ToLower(commandStr)
	blockedTokens := []string{
		"curl ", "wget ", "nc ", "netcat ", "ssh ", "scp ", "telnet ",
		"rm -rf /", "rm -rf /*", "mkfs", "dd if=", ":(){ :|:& };:",
	}
	for _, tok := range blockedTokens {
		if strings.Contains(lower, tok) {
			return "", fmt.Errorf("security violation: command contains blocked token '%s'", strings.TrimSpace(tok))
		}
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "sh", "-c", commandStr)
	cmd.Dir = t.AppRoot
	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf

	err := cmd.Run()
	output := outBuf.String()
	if err != nil {
		return fmt.Sprintf("Command failed (%v):\n%s", err, output), nil
	}
	if strings.TrimSpace(output) == "" {
		return "Command executed successfully with 0 exit code (no output).", nil
	}
	return output, nil
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
