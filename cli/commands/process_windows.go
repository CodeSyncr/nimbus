//go:build windows

package commands

import (
	"os/exec"
	"strconv"
	"time"
)

// binExt is the executable extension Air's Windows runner requires: it
// launches the built binary via `cmd /c`, which refuses extension-less files.
const binExt = ".exe"

// setPgid is a no-op on Windows, which has no POSIX process groups.
func setPgid(cmd *exec.Cmd) {}

// killGroup terminates the child and its whole process tree. Windows has no
// process-group signal, so grace is unused; taskkill /T reaches the
// grandchildren (Air and the app binary under `go run`) that a plain
// Process.Kill would orphan, leaving the port bound for the next serve.
func killGroup(cmd *exec.Cmd, grace time.Duration) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := strconv.Itoa(cmd.Process.Pid)
	if err := exec.Command("taskkill", "/T", "/F", "/PID", pid).Run(); err != nil {
		_ = cmd.Process.Kill()
	}
}
