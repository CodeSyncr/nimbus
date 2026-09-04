//go:build windows

package ai

import "os/exec"

// isolateProcessGroup is a no-op on Windows: process groups work differently,
// and cmd.Cancel plus WaitDelay already unstick a command whose children hold
// the output pipe open.
func isolateProcessGroup(cmd *exec.Cmd) {}

// killProcessGroup terminates the command.
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
