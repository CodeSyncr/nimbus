package command

import "github.com/spf13/cobra"

// Ctx is passed to RunE and provides access to arguments, flags, and terminal UI.
type Ctx struct {
	args []string
	cmd  *cobra.Command
	ui   *UI
}

// UI returns the terminal UI (logger, colors, table, sticker). AdonisJS Ace style.
func (c *Ctx) UI() *UI {
	if c.ui == nil {
		c.ui = NewUI()
	}
	return c.ui
}

// Logger returns ctx.UI().Logger() for convenience.
func (c *Ctx) Logger() *Logger {
	return c.UI().Logger()
}

// Colors returns ctx.UI().Colors() for convenience.
func (c *Ctx) Colors() *Colors {
	return c.UI().Colors()
}

// Args returns all positional arguments.
func (c *Ctx) Args() []string {
	return c.args
}

// Arg returns the argument at index i, or empty string if out of range.
func (c *Ctx) Arg(i int) string {
	if i < 0 || i >= len(c.args) {
		return ""
	}
	return c.args[i]
}

// Bool returns the value of a boolean flag.
func (c *Ctx) Bool(name string) bool {
	v, _ := c.cmd.Flags().GetBool(name)
	return v
}

// String returns the value of a string flag.
func (c *Ctx) String(name string) string {
	v, _ := c.cmd.Flags().GetString(name)
	return v
}

// Int returns the value of an int flag.
func (c *Ctx) Int(name string) int {
	v, _ := c.cmd.Flags().GetInt(name)
	return v
}
