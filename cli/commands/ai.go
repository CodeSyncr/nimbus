package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	planOnly bool
	autoYes  bool
	permMode string
	// listOnly prints the project's saved sessions and exits.
	listOnly bool
	// continueLast resumes the most recent session without needing its id.
	continueLast bool
}

func (c *AICommand) Name() string { return "ai" }
func (c *AICommand) Description() string {
	return "AI Copilot — plan and execute architectural code changes (powered by nimbusgo.space)"
}
func (c *AICommand) Args() int { return -1 }

func (c *AICommand) Flags(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&c.model, "model", "m", "", "AI model (default: optimal on Nimbus Cloud or GPT-4o / Sonnet)")
	cmd.Flags().StringVar(&c.server, "server", "", "Nimbus Cloud server URL (default: https://nimbusgo.space)")
	cmd.Flags().StringVar(&c.resumeID, "resume", "", "Resume a saved AI session ID (see --list)")
	cmd.Flags().BoolVar(&c.continueLast, "continue", false, "Resume the most recent session in this project")
	cmd.Flags().BoolVar(&c.listOnly, "list", false, "List saved AI sessions in this project and exit")
	cmd.Flags().BoolVar(&c.dry, "dry-run", false, "Show generated diffs and plans without writing to disk")
	cmd.Flags().BoolVar(&c.autoYes, "yes", false, "Approve flagged actions automatically (for CI and non-interactive runs)")
	cmd.Flags().StringVar(&c.permMode, "permission-mode", "", "auto (assess each action, ask about risky ones) | ask (confirm every change) | allow (run anything not refused). Default: the permission_mode setting (/settings)")
	cmd.Flags().BoolVar(&c.planOnly, "plan-only", false, "Stop after generating and reviewing the plan")
}

func (c *AICommand) Run(ctx *cli.Context) error {
	initialPrompt := strings.Join(ctx.Args, " ")

	// Listing needs neither credentials nor a project scan: it reads what is
	// already on disk, and asking someone to authenticate to find out which
	// session to resume would be backwards.
	if c.listOnly {
		return c.listSessions(ctx)
	}

	// Ensure default agent skills are placed in ~/.nimbus/skills/
	_ = ai.EnsureDefaultSkills()

	serverURL := c.server
	if serverURL == "" {
		serverURL = auth.GetServerURL()
	}

	// 1. Authenticate. Nimbus AI runs on Nimbus Cloud, so a session is
	// required before anything else happens.
	creds, err := auth.LoadCredentials()
	if err != nil || creds == nil || creds.IsExpired() {
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#94a3b8"))
		fmt.Fprintln(ctx.Stdout, dimStyle.Render("🔐 Authenticating with Nimbus Cloud (nimbusgo.space)..."))
		if _, err = auth.Login(ctx, serverURL); err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
	}

	// 2. Scan Project Context
	projCtx, err := ai.ScanProject(ctx.AppRoot)
	if err != nil {
		ctx.UI.Warnf("Could not scan project directory: %v", err)
		projCtx = &ai.ProjectContext{AppRoot: ctx.AppRoot}
	}

	// 3. Resolve settings, then let explicit flags override them.
	//
	// A flag is a decision about this run; a setting is a decision about every
	// run. Flags that carry no "unset" value (--yes, --plan-only) can only
	// turn something on, which is what passing them means.
	settings := ai.LoadSettings(ctx.AppRoot)
	if c.model != "" {
		settings.Model = c.model
	}
	if c.permMode != "" {
		settings.PermissionMode = c.permMode
	}
	if c.planOnly || c.dry {
		settings.PlanFirst = true
	}

	// 4. Load or initialize Session
	var session *ai.Session
	switch {
	case c.resumeID != "":
		loaded, err := ai.LoadSession(ctx.AppRoot, c.resumeID)
		if err != nil {
			return fmt.Errorf("cannot resume session: %w", err)
		}
		session = loaded
		ctx.UI.Successf("Resumed session %s (updated: %s)", session.ID, session.UpdatedAt.Format("15:04:05"))

	case c.continueLast:
		loaded, err := ai.LatestSession(ctx.AppRoot)
		if err != nil {
			return fmt.Errorf("cannot read saved sessions: %w", err)
		}
		if loaded == nil {
			ctx.UI.Infof("No saved sessions in this project yet — starting a new one.")
			session = ai.NewSession(settings.Model)
			break
		}
		session = loaded
		ctx.UI.Successf("Resumed session %s (updated: %s)", session.ID, session.UpdatedAt.Format("15:04:05"))

	default:
		session = ai.NewSession(settings.Model)
	}

	// 5. Initialize Tools & Client
	tools := ai.NewToolExecutor(ctx.AppRoot)
	tools.AutoApprove = c.autoYes

	client, err := ai.ResolveClient(serverURL, settings.Model)
	if err != nil {
		return err
	}

	// Image generation runs through Nimbus Cloud: the CLI holds no provider
	// keys, and which model draws is server-side configuration. Wiring this
	// is what makes the generate_image tool appear in the model's tool list.
	if cloud, ok := client.(*ai.NimbusCloudClient); ok {
		tools.GenerateImage = func(ctx context.Context, prompt, size, model string) ([]byte, string, error) {
			img, err := cloud.GenerateImage(ctx, prompt, size, model)
			if err != nil {
				return nil, "", err
			}
			return img.Data, img.Model, nil
		}
	}

	// 6. Initialize Agent. ApplySettings is what makes the settings screen
	// mean something: it pushes the resolved values into the agent, the
	// session's context limits, and the tool executor's permission mode.
	agent := ai.NewAgent(client, tools, projCtx, session)
	agent.ApplySettings(settings)

	// 7. If running headlessly (e.g. tests or non-interactive pipe)
	if ctx.Stdin == nil {
		if initialPrompt != "" {
			return c.executeHeadless(ctx, agent, initialPrompt)
		}
		return nil
	}

	// 8. Launch Interactive TUI. On Windows, put the console into UTF-8 /
	// virtual-terminal mode first so the glyphs and colours render.
	enableConsoleUTF8()
	isOneShot := initialPrompt != "" && !c.interactiveModeEnabled()
	model := tui.NewModel(agent, initialPrompt, isOneShot)
	// The default flow is one conversation in which the model decides what a
	// request needs. --plan-only / --dry-run, and the plan_first setting, opt
	// into the staged pipeline with its reviewable plan.
	model.PlanFirst = settings.PlanFirst
	model.ExpandDiffs = settings.ExpandDiffs

	// The alt screen replaces the terminal's own scrollback, so without mouse
	// reporting the wheel does nothing at all inside the transcript. Cell
	// motion is the mode the viewport's wheel handling expects.
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
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
			if usage := m.Agent.Session.Usage.Summary(); usage != "" {
				fmt.Fprintf(ctx.Stdout, "\n📊 %s\n", usage)
			}
			fmt.Fprintf(ctx.Stdout, "\n⚡ Session saved. Resume anytime with: nimbus ai --resume %s\n\n", m.Agent.Session.ID)
		}
	}

	return nil
}

// listSessions prints what can be resumed.
//
// ListSessions has existed since sessions did, but nothing called it: resuming
// meant already knowing a hex id, which is only knowable by reading the
// directory by hand.
func (c *AICommand) listSessions(ctx *cli.Context) error {
	sessions, err := ai.ListSessions(ctx.AppRoot)
	if err != nil {
		return fmt.Errorf("cannot read saved sessions: %w", err)
	}
	if len(sessions) == 0 {
		fmt.Fprintln(ctx.Stdout, "No saved AI sessions in this project yet.")
		return nil
	}

	now := time.Now()
	fmt.Fprintf(ctx.Stdout, "\n%s\n", ai.SessionListHeader())
	for _, s := range sessions {
		fmt.Fprintln(ctx.Stdout, s.Describe(now))
	}
	fmt.Fprintf(ctx.Stdout, "\nResume one with:  nimbus ai --resume %s\n", sessions[0].ID)
	fmt.Fprintf(ctx.Stdout, "Or the latest with:  nimbus ai --continue\n\n")
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

	// Without --plan-only/--dry-run this is one conversational turn: the model
	// answers questions directly and makes changes when asked, with no plan or
	// approval gate in between.
	// The agent carries the resolved settings, so a headless run honours
	// plan_first exactly as the interactive one does.
	if !agent.Settings().PlanFirst {
		res, err := agent.Run(ctxBg, prompt)
		if err != nil {
			return c.reportRunError(ctx, err)
		}
		if res != nil && res.Text != "" {
			fmt.Fprintln(ctx.Stdout, res.Text)
		}
		c.reportUsage(ctx, agent)
		return nil
	}

	plan, err := agent.GeneratePlan(ctxBg, prompt)
	if err != nil {
		return c.reportRunError(ctx, err)
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
	c.reportUsage(ctx, agent)
	return nil
}

// reportRunError turns a subscription failure into the upgrade message and
// passes anything else through unchanged.
func (c *AICommand) reportRunError(ctx *cli.Context, err error) error {
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "subscription") || strings.Contains(msg, "payment") || strings.Contains(msg, "402") {
		fmt.Fprintln(ctx.Stdout, "⚠️ Subscription Required: Please upgrade to Pro at https://nimbusgo.space/pricing")
		return nil
	}
	return err
}

// reportUsage prints what the run consumed, when the server reported it.
func (c *AICommand) reportUsage(ctx *cli.Context, agent *ai.Agent) {
	if agent.Session == nil {
		return
	}
	if usage := agent.Session.Usage.Summary(); usage != "" {
		fmt.Fprintf(ctx.Stdout, "\n%s\n", usage)
	}
}

func (c *AICommand) interactiveModeEnabled() bool {
	return true
}
