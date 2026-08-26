package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/CodeSyncr/nimbus/cli"
	"github.com/CodeSyncr/nimbus/cli/auth"
	"github.com/CodeSyncr/nimbus/internal/ai"
	"github.com/CodeSyncr/nimbus/internal/ai/tui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

func init() {
	cli.RegisterCommand(&AICommand{})
}

// AICommand launches the architectural AI copilot.
type AICommand struct {
	model    string
	server   string
	resumeID string
	dry      bool
	offline  bool
	planOnly bool
}

func (c *AICommand) Name() string        { return "ai" }
func (c *AICommand) Description() string { return "AI Copilot — plan and execute architectural code changes (powered by nimbusgo.space)" }
func (c *AICommand) Args() int           { return -1 }

func (c *AICommand) Flags(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&c.model, "model", "m", "", "AI model (default: optimal on Nimbus Cloud or GPT-4o / Sonnet)")
	cmd.Flags().StringVar(&c.server, "server", "", "Nimbus Cloud server URL (default: https://nimbusgo.space)")
	cmd.Flags().StringVar(&c.resumeID, "resume", "", "Resume a saved AI session ID (.nimbus/ai-sessions/<id>.json)")
	cmd.Flags().BoolVar(&c.dry, "dry-run", false, "Show generated diffs and plans without writing to disk")
	cmd.Flags().BoolVar(&c.offline, "offline", false, "Run in offline mode without cloud AI connection")
	cmd.Flags().BoolVar(&c.planOnly, "plan-only", false, "Stop after generating and reviewing the plan")
}

func (c *AICommand) Run(ctx *cli.Context) error {
	initialPrompt := strings.Join(ctx.Args, " ")

	// Ensure default agent skills are placed in ~/.nimbus/skills/
	_ = ai.EnsureDefaultSkills()

	serverURL := c.server
	if serverURL == "" {
		serverURL = auth.GetServerURL()
	}

	// 1. Check or prompt authentication if connecting to Nimbus Cloud
	if !c.offline {
		creds, err := auth.LoadCredentials()
		if err != nil || creds == nil || creds.IsExpired() {
			dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#94a3b8"))
			fmt.Fprintln(ctx.Stdout, dimStyle.Render("🔐 Authenticating with Nimbus Cloud (nimbusgo.space)..."))
			creds, err = auth.Login(ctx, serverURL)
			if err != nil {
				return fmt.Errorf("authentication failed: %w", err)
			}
		}
	}

	// 2. Scan Project Context
	projCtx, err := ai.ScanProject(ctx.AppRoot)
	if err != nil {
		ctx.UI.Warnf("Could not scan project directory: %v", err)
		projCtx = &ai.ProjectContext{AppRoot: ctx.AppRoot}
	}

	// 3. Load or initialize Session
	var session *ai.Session
	if c.resumeID != "" {
		loaded, err := ai.LoadSession(ctx.AppRoot, c.resumeID)
		if err != nil {
			return fmt.Errorf("cannot resume session: %w", err)
		}
		session = loaded
		ctx.UI.Successf("Resumed session %s (updated: %s)", session.ID, session.UpdatedAt.Format("15:04:05"))
	} else {
		session = ai.NewSession(c.model)
	}

	// 4. Initialize Tools & Client
	tools := ai.NewToolExecutor(ctx.AppRoot)

	client, err := ai.ResolveClient(serverURL, c.model)
	if err != nil {
		return err
	}

	// 5. Initialize Agent
	agent := ai.NewAgent(client, tools, projCtx, session)

	// 6. If running headlessly (e.g. tests or non-interactive pipe)
	if ctx.Stdin == nil {
		if initialPrompt != "" {
			return c.executeHeadless(ctx, agent, initialPrompt)
		}
		return nil
	}

	// 7. Launch Interactive TUI. On Windows, put the console into UTF-8 /
	// virtual-terminal mode first so the glyphs and colours render.
	enableConsoleUTF8()
	isOneShot := initialPrompt != "" && !c.interactiveModeEnabled()
	model := tui.NewModel(agent, initialPrompt, isOneShot)

	p := tea.NewProgram(model, tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		return fmt.Errorf("TUI execution failed: %w", err)
	}

	if m, ok := finalModel.(tui.Model); ok {
		if m.OneShot && m.FinalSummary != "" {
			// The alt screen is gone; repeat the outcome on the normal screen.
			fmt.Fprintln(ctx.Stdout, m.FinalSummary)
		}
		if m.Agent.Session != nil {
			_ = ai.SaveSession(ctx.AppRoot, m.Agent.Session)
			fmt.Fprintf(ctx.Stdout, "\n⚡ Session saved. Resume anytime with: nimbus ai --resume %s\n\n", m.Agent.Session.ID)
		}
	}

	return nil
}

func (c *AICommand) executeHeadless(ctx *cli.Context, agent *ai.Agent, prompt string) error {
	ctxBg := context.Background()

	// Surface the agent's investigation and edits so a headless run (CI,
	// pipes) still shows what was read and changed.
	agent.Callbacks = ai.AgentCallbacks{
		OnStatus: func(text string) {
			fmt.Fprintf(ctx.Stdout, "» %s\n", text)
		},
		OnToolResult: func(toolName string, args map[string]any, output string, err error) {
			target, _ := args["path"].(string)
			if target == "" {
				target, _ = args["command"].(string)
			}
			if target == "" {
				target, _ = args["pattern"].(string)
			}
			if err != nil {
				fmt.Fprintf(ctx.Stdout, "  ✖ %s %s: %v\n", toolName, target, err)
				return
			}
			fmt.Fprintf(ctx.Stdout, "  ✓ %s %s\n", toolName, target)
		},
	}

	plan, err := agent.GeneratePlan(ctxBg, prompt)
	if err != nil {
		errMsg := strings.ToLower(err.Error())
		if strings.Contains(errMsg, "subscription") || strings.Contains(errMsg, "payment") || strings.Contains(errMsg, "402") {
			fmt.Fprintln(ctx.Stdout, "⚠️ Subscription Required: Please upgrade to Pro at https://nimbusgo.space/pricing")
			return nil
		}
		return err
	}

	if c.dry || c.planOnly {
		fmt.Fprintf(ctx.Stdout, "Plan generated: %s (%d steps)\n", plan.Summary, len(plan.Steps))
		return nil
	}

	// A question or an answer that needs no changes: print it and stop.
	if len(plan.Steps) == 0 {
		if plan.NeedsClarification && len(plan.Questions) > 0 {
			fmt.Fprintln(ctx.Stdout, "The request needs clarification (run interactively to answer):")
			for _, q := range plan.Questions {
				fmt.Fprintf(ctx.Stdout, "  - %s\n", q.Question)
			}
			return nil
		}
		fmt.Fprintln(ctx.Stdout, plan.Summary)
		return nil
	}

	// Execute steps directly
	for _, step := range plan.Steps {
		if step.Action == "create_file" || step.Action == "write_file" {
			targetPath := filepath.Join(ctx.AppRoot, step.Target)
			_ = os.MkdirAll(filepath.Dir(targetPath), 0755)
		}
	}

	summary, err := agent.ExecuteApprovedPlan(ctxBg, plan)
	if err != nil {
		return err
	}
	if summary != "" {
		fmt.Fprintln(ctx.Stdout, summary)
	}
	return nil
}

func (c *AICommand) interactiveModeEnabled() bool {
	return true
}
