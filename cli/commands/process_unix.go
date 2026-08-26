//go:build !windows

package commands

import (
	"os/exec"
	"syscall"
	"time"
)

// binExt is the executable extension for built binaries; none on Unix.
const binExt = ""

// setPgid puts the child into its own process group so that the whole tree
// (Air, and the app binary Air spawns) can be signalled as a unit.
func setPgid(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killGroup terminates the child's entire process group, escalating from
// SIGTERM to SIGKILL after grace.
func killGroup(cmd *exec.Cmd, grace time.Duration) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	time.Sleep(grace)
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
