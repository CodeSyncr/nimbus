// Package repl provides an interactive Go REPL. It wraps github.com/x-motemen/gore
// and is not exposed in the public Nimbus API.
package repl

import (
	"io"
	"os"

	"github.com/x-motemen/gore"
)

// Run starts an interactive Go REPL. It uses the current working directory
// for the Go module context. Use :quit or Ctrl-D to exit.
func Run() error {
	g := gore.New(
		gore.OutWriter(os.Stdout),
		gore.ErrWriter(os.Stderr),
	)
	return g.Run()
}

// RunWithOptions starts a REPL with custom options. Used internally.
func RunWithOptions(out, errOut io.Writer, opts ...gore.Option) error {
	all := append([]gore.Option{
		gore.OutWriter(out),
		gore.ErrWriter(errOut),
	}, opts...)
	g := gore.New(all...)
	return g.Run()
}
