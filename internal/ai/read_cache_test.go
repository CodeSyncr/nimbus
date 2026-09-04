package ai

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFixture(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// An agent investigating a codebase re-reads the same files. The content is
// already in the conversation, so sending it again burns the context it needs
// to finish the work.
func TestUnchangedFileIsNotSentTwice(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "components/nav.css", strings.Repeat(".nav { color: red }\n", 40))
	exec := &ToolExecutor{AppRoot: dir}

	first, err := exec.ReadFile("components/nav.css")
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if !strings.Contains(first, ".nav") {
		t.Fatalf("first read did not return the file: %q", first)
	}

	second, err := exec.ReadFile("components/nav.css")
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if strings.Contains(second, ".nav {") {
		t.Error("the file was sent again in full")
	}
	if !strings.Contains(second, "unchanged") || !strings.Contains(second, "nav.css") {
		t.Errorf("the reminder should name the file and say why: %q", second)
	}
}

// A file that changed on disk must be delivered again — otherwise the agent
// works from a stale copy.
func TestChangedFileIsReadAgain(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "main.go", "package main\n")
	exec := &ToolExecutor{AppRoot: dir}

	if _, err := exec.ReadFile("main.go"); err != nil {
		t.Fatal(err)
	}
	// Rewrite through the tool, which is what an edit does.
	if _, _, err := exec.WriteFile("main.go", "package main\n\nfunc main() {}\n"); err != nil {
		t.Fatal(err)
	}

	after, err := exec.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(after, "func main") {
		t.Errorf("a rewritten file was not re-read: %q", after)
	}
}

// A model that keeps asking has usually lost the earlier output; refusing
// forever would strand it.
func TestPersistentRereadEventuallySucceeds(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "a.txt", "hello\n")
	exec := &ToolExecutor{AppRoot: dir}

	if _, err := exec.ReadFile("a.txt"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxReadSuppressions; i++ {
		out, _ := exec.ReadFile("a.txt")
		if !strings.Contains(out, "unchanged") {
			t.Fatalf("read %d should have been suppressed: %q", i+2, out)
		}
	}
	out, err := exec.ReadFile("a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("an insistent re-read should get the content back: %q", out)
	}
}

// A ranged read asks for a slice, not the file, and must never be suppressed.
func TestRangedReadsAreAlwaysServed(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "big.txt", "one\ntwo\nthree\nfour\n")
	exec := &ToolExecutor{AppRoot: dir}

	if _, err := exec.ReadFile("big.txt"); err != nil {
		t.Fatal(err)
	}
	out, err := exec.ReadFileRange("big.txt", 2, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "two") {
		t.Errorf("a ranged read was suppressed: %q", out)
	}
}

// The cache is per executor, so it follows the session rather than leaking
// between projects.
func TestReadCacheIsPerExecutor(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "x.txt", "content\n")

	first := &ToolExecutor{AppRoot: dir}
	if _, err := first.ReadFile("x.txt"); err != nil {
		t.Fatal(err)
	}

	second := &ToolExecutor{AppRoot: dir}
	out, err := second.ReadFile("x.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "content") {
		t.Errorf("a fresh executor should read the file: %q", out)
	}
	_ = context.Background()
}
