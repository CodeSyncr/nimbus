// Package command provides an AdonisJS Ace-style CLI for Nimbus applications.
// It abstracts the underlying implementation so apps use nimbus/command, not cobra directly.
package command

import (
	"github.com/spf13/cobra"
)

// Command represents a CLI command. Create with New and chain builder methods.
type Command struct {
	c *cobra.Command
}

// New creates a new command with the given name and short description.
func New(use, short string) *Command {
	return &Command{
		c: &cobra.Command{
			Use:   use,
			Short: short,
		},
	}
}

// Long sets the long description shown with --help.
func (c *Command) Long(s string) *Command {
	c.c.Long = s
	return c
}

// Aliases sets alternative names for the command.
func (c *Command) Aliases(aliases ...string) *Command {
	c.c.Aliases = aliases
	return c
}

// ArgsExact requires exactly n arguments.
func (c *Command) ArgsExact(n int) *Command {
	c.c.Args = cobra.ExactArgs(n)
	return c
}

// ArgsMin requires at least n arguments.
func (c *Command) ArgsMin(n int) *Command {
	c.c.Args = cobra.MinimumNArgs(n)
	return c
}

// ArgsMax requires at most n arguments.
func (c *Command) ArgsMax(n int) *Command {
	c.c.Args = cobra.MaximumNArgs(n)
	return c
}

// RunE sets the command's run function. The Ctx provides args, flags, and terminal UI.
func (c *Command) RunE(fn func(ctx *Ctx) error) *Command {
	c.c.RunE = func(cmd *cobra.Command, args []string) error {
		return fn(&Ctx{args: args, cmd: cmd, ui: NewUI()})
	}
	return c
}

// BoolFlag adds a boolean flag.
func (c *Command) BoolFlag(name, shorthand string, defaultVal bool, usage string) *Command {
	c.c.Flags().BoolP(name, shorthand, defaultVal, usage)
	return c
}

// StringFlag adds a string flag.
func (c *Command) StringFlag(name, shorthand string, defaultVal string, usage string) *Command {
	c.c.Flags().StringP(name, shorthand, defaultVal, usage)
	return c
}

// IntFlag adds an int flag.
func (c *Command) IntFlag(name, shorthand string, defaultVal int, usage string) *Command {
	c.c.Flags().IntP(name, shorthand, defaultVal, usage)
	return c
}

// cobra returns the underlying cobra command (internal use).
func (c *Command) cobra() *cobra.Command {
	return c.c
}
