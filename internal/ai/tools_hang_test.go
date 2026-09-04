package ai

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The reported hang: "node src/server.js &" ran for 364s against a 120s
// timeout. The shell exits immediately, but the backgrounded child inherits
// the output pipe and never closes it, so Wait blocks forever.
func TestBackgroundedCommandIsRefusedNotHung(t *testing.T) {
	exec := &ToolExecutor{AppRoot: t.TempDir()}

	done := make(chan struct{})
	var out string
	var err error
	go func() {
		out, err = exec.Bash(context.Background(), "node src/server.js &")
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("a backgrounded command hung the tool")
	}

	if err == nil {
		t.Fatalf("expected a refusal, got output %q", out)
	}
	if !strings.Contains(err.Error(), "background") {
		t.Errorf("the error should explain the problem, got: %v", err)
	}
}

func TestBackgroundDetection(t *testing.T) {
	background := []string{
		"node server.js &",
		"npm run dev &",
		"  python -m http.server 8000 &  ",
		"node server.js & sleep 1",
	}
	for _, cmd := range background {
		if !isBackgrounded(cmd) {
			t.Errorf("isBackgrounded(%q) = false, want true", cmd)
		}
	}

	// "&&" is a conjunction; these must still run.
	foreground := []string{
		"go build ./... && go test ./...",
		"npm install && npm run build",
		"echo hello",
		"grep -r 'a && b' .",
	}
	for _, cmd := range foreground {
		if isBackgrounded(cmd) {
			t.Errorf("isBackgrounded(%q) = true, want false", cmd)
		}
	}
}

// A command whose child holds the output pipe open must not outlive its
// timeout — this is what WaitDelay and the process-group kill guarantee.
func TestCommandWithLingeringChildStillReturns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process-group semantics differ on Windows")
	}
	exec := &ToolExecutor{AppRoot: t.TempDir(), CommandTimeout: 2 * time.Second}

	start := time.Now()
	done := make(chan struct{})
	go func() {
		// The subshell keeps the inherited stdout open for 60s while the
		// parent shell exits immediately.
		_, _ = exec.Bash(context.Background(), "(sleep 60) & echo started")
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("command with a lingering child never returned")
	}
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Errorf("took %v; the pipe wait is not bounded", elapsed)
	}
}
