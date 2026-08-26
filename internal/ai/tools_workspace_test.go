package ai

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupWorkspace(t *testing.T) *ToolExecutor {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"main.go":                    "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n",
		"app/controllers/user.go":    "package controllers\n\n// UserController handles users\ntype UserController struct{}\n",
		"app/models/user.go":         "package models\n\ntype User struct{ Name string }\n",
		"resources/views/index.html": "<h1>Hello</h1>\n",
		"node_modules/x/index.js":    "ignored",
	}
	for rel, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return NewToolExecutor(dir)
}

func TestFindFiles(t *testing.T) {
	tools := setupWorkspace(t)
	out, err := tools.FindFiles("**/*.go", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"main.go", "app/controllers/user.go", "app/models/user.go"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "node_modules") {
		t.Errorf("node_modules should be skipped")
	}
	out, _ = tools.FindFiles("app/models/*.go", "")
	if strings.Contains(out, "controllers") || !strings.Contains(out, "app/models/user.go") {
		t.Errorf("directory-scoped glob wrong:\n%s", out)
	}
	out, _ = tools.FindFiles("*.html", "")
	if !strings.Contains(out, "resources/views/index.html") {
		t.Errorf("basename glob should match nested files:\n%s", out)
	}
}

func TestGrepWithInclude(t *testing.T) {
	tools := setupWorkspace(t)
	out, err := tools.GrepFiltered("User", ".", "*.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "app/controllers/user.go:3") || !strings.Contains(out, "app/models/user.go:3") {
		t.Errorf("unexpected grep output:\n%s", out)
	}
	out, _ = tools.GrepFiltered("Hello", ".", "*.go")
	if !strings.Contains(out, "No matches") {
		t.Errorf("include filter not applied:\n%s", out)
	}
}

func TestReadFileRange(t *testing.T) {
	tools := setupWorkspace(t)
	out, err := tools.ReadFileRange("main.go", 3, 4)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "showing 3-4") || !strings.Contains(out, "func main()") || strings.Contains(out, "package main") {
		t.Errorf("range read wrong:\n%s", out)
	}
	if _, err := tools.ReadFileRange("../outside.go", 0, 0); err == nil {
		t.Errorf("path traversal must be rejected")
	}
}

func TestEditFileToleratesCRLF(t *testing.T) {
	tools := setupWorkspace(t)
	crlf := "package a\r\n\r\nfunc A() {}\r\n"
	if err := os.WriteFile(filepath.Join(tools.AppRoot, "a.go"), []byte(crlf), 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := tools.EditFileAll("a.go", "func A() {}\n", "func A() int { return 1 }\n", false); err != nil {
		t.Fatalf("CRLF-tolerant edit failed: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(tools.AppRoot, "a.go"))
	if !strings.Contains(string(data), "return 1") {
		t.Errorf("edit not applied: %q", data)
	}
	if _, _, err := tools.EditFileAll("a.go", "nope", "x", false); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not-found error, got %v", err)
	}
}

func TestBashReportsFailureAsOutput(t *testing.T) {
	tools := setupWorkspace(t)
	out, ok := tools.RunCommand(context.Background(), "exit 3")
	if ok || !strings.Contains(out, "Command failed") {
		t.Errorf("expected failure report, got ok=%v out=%q", ok, out)
	}
	out, ok = tools.RunCommand(context.Background(), "echo hello")
	if !ok || !strings.Contains(out, "hello") {
		t.Errorf("expected success, got ok=%v out=%q", ok, out)
	}
	if _, err := tools.Bash(context.Background(), "curl http://example.com"); err == nil {
		t.Errorf("blocked command should error")
	}
}

func TestListDirDepth(t *testing.T) {
	tools := setupWorkspace(t)
	out, err := tools.ListDirDepth(".", 2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[DIR]  app/") || !strings.Contains(out, "controllers/") || strings.Contains(out, "node_modules") {
		t.Errorf("unexpected listing:\n%s", out)
	}
}
