//go:build !windows

package ai

import (
	"os/exec"
	"syscall"
)

// isolateProcessGroup puts the command in its own process group.
//
// A shell command routinely starts children ("npm run dev", "node server.js &",
// anything with a pipeline). Killing only the shell leaves those children
// running and, worse, holding the output pipe open — which is what made a
// backgrounded server hang the agent indefinitely. With its own group, one
// signal reaches the whole tree.
func isolateProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killProcessGroup terminates the command and everything it started.
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// The negative pid addresses the group created by isolateProcessGroup.
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err == nil {
		return nil
	}
	return cmd.Process.Kill()
}
