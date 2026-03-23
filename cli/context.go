package cli

import (
	"io"
	"os"
	"path/filepath"

	"github.com/CodeSyncr/nimbus/cli/ui"
	"github.com/spf13/cobra"
)

// Context provides per-command information and helpers, similar to
// Laravel Artisan's Command context. It wraps Cobra's command and args
// and adds app root discovery and a UI helper.
type Context struct {
	Cmd     *cobra.Command
	Args    []string
	AppRoot string
	Stdout  io.Writer
	Stderr  io.Writer
	Stdin   io.Reader
	UI      *ui.UI
}

// NewContext builds a Context for a running Cobra command.
func NewContext(cmd *cobra.Command, args []string) *Context {
	root := findAppRoot()
	u := ui.NewUI(os.Stdin, os.Stdout, os.Stderr)
	return &Context{
		Cmd:     cmd,
		Args:    args,
		AppRoot: root,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		Stdin:   os.Stdin,
		UI:      u,
	}
}

// findAppRoot walks up from the current working directory looking for go.mod.
// If not found, it returns the original working directory.
func findAppRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return wd
		}
		dir = parent
	}
}
