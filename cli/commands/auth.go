package commands

import (
	"fmt"

	"github.com/CodeSyncr/nimbus/cli"
	"github.com/CodeSyncr/nimbus/cli/auth"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

func init() {
	cli.RegisterCommand(&LoginCommand{})
	cli.RegisterCommand(&LogoutCommand{})
	cli.RegisterCommand(&WhoamiCommand{})
}

// ---------------------------------------------------------------------------
// Login Command
// ---------------------------------------------------------------------------

// LoginCommand handles logging in to Nimbus Cloud.
type LoginCommand struct {
	server string
}

func (c *LoginCommand) Name() string        { return "login" }
func (c *LoginCommand) Description() string { return "Log in to your Nimbus Cloud account (nimbusgo.space)" }
func (c *LoginCommand) Args() int           { return 0 }
func (c *LoginCommand) Aliases() []string   { return []string{"auth:login"} }

func (c *LoginCommand) Flags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&c.server, "server", "", "Custom Nimbus Cloud server URL (default: https://nimbusgo.space)")
}

func (c *LoginCommand) Run(ctx *cli.Context) error {
	_, err := auth.Login(ctx, c.server)
	return err
}

// ---------------------------------------------------------------------------
// Logout Command
// ---------------------------------------------------------------------------

// LogoutCommand clears saved Nimbus Cloud credentials.
type LogoutCommand struct{}

func (c *LogoutCommand) Name() string        { return "logout" }
func (c *LogoutCommand) Description() string { return "Log out from your Nimbus Cloud account" }
func (c *LogoutCommand) Args() int           { return 0 }
func (c *LogoutCommand) Aliases() []string   { return []string{"auth:logout"} }

func (c *LogoutCommand) Flags(cmd *cobra.Command) {}

func (c *LogoutCommand) Run(ctx *cli.Context) error {
	if err := auth.ClearCredentials(); err != nil {
		return fmt.Errorf("failed to clear credentials: %w", err)
	}
	ctx.UI.Successf("Logged out successfully. Credentials cleared.")
	return nil
}

// ---------------------------------------------------------------------------
// Whoami Command
// ---------------------------------------------------------------------------

// WhoamiCommand displays the authenticated user profile and subscription.
type WhoamiCommand struct{}

func (c *WhoamiCommand) Name() string        { return "whoami" }
func (c *WhoamiCommand) Description() string { return "Display the logged-in Nimbus Cloud account and subscription plan" }
func (c *WhoamiCommand) Args() int           { return 0 }
func (c *WhoamiCommand) Aliases() []string   { return []string{"auth:status", "auth:whoami", "account"} }

func (c *WhoamiCommand) Flags(cmd *cobra.Command) {}

func (c *WhoamiCommand) Run(ctx *cli.Context) error {
	creds, err := auth.LoadCredentials()
	if err != nil {
		return fmt.Errorf("failed to load credentials: %w", err)
	}

	if creds == nil || creds.AccessToken == "" {
		ctx.UI.Warnf("Not currently logged in to Nimbus Cloud.")
		ctx.UI.Infof("Run 'nimbus login' to authenticate with nimbusgo.space")
		return nil
	}

	headerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#818cf8")).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#94a3b8"))
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#f8fafc")).Bold(true)
	planStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e")).Bold(true)
	warningStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#f59e0b"))

	fmt.Fprintln(ctx.Stdout, headerStyle.Render("⚡ Nimbus Cloud Account"))
	fmt.Fprintf(ctx.Stdout, "  %s %s\n", labelStyle.Render("Server:"), valueStyle.Render(creds.ServerURL))
	if creds.Email != "" {
		fmt.Fprintf(ctx.Stdout, "  %s  %s\n", labelStyle.Render("Email:"), valueStyle.Render(creds.Email))
	}
	if creds.Name != "" {
		fmt.Fprintf(ctx.Stdout, "  %s   %s\n", labelStyle.Render("Name:"), valueStyle.Render(creds.Name))
	}

	plan := creds.Plan
	if plan == "" {
		plan = "pro"
	}
	fmt.Fprintf(ctx.Stdout, "  %s   %s\n", labelStyle.Render("Plan:"), planStyle.Render(plan))

	if !creds.HasSub && plan == "free" {
		fmt.Fprintln(ctx.Stdout)
		fmt.Fprintln(ctx.Stdout, warningStyle.Render("  ⚠  No active AI subscription. Upgrade at https://nimbusgo.space/pricing to unlock AI Copilot."))
	}

	return nil
}
