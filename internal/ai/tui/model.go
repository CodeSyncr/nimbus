package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/CodeSyncr/nimbus/internal/ai"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Mode int

const (
	ModeChat Mode = iota
	ModePlanning
	ModeClarification
	ModePlanReview
	ModeExecuting
	ModeCompleted
)

// ChatItem represents a structured entry in the chat history.
type ChatItem struct {
	Role      string // "user" | "assistant" | "system" | "tool"
	Content   string
	Timestamp time.Time
	Plan      *ai.PlanSummary
	Diffs     []string
	ToolName  string
	ToolArgs  map[string]any
	IsError   bool
}

type (
	chatReplyMsg   struct{ reply string; err error }
	planDeltaMsg   struct{ delta string }
	planDoneMsg    struct{ plan *ai.PlanSummary; err error }
	execDeltaMsg   struct{ delta string }
	execDoneMsg    struct{ summary string; err error }
	toolCallMsg    struct{ name string; args map[string]any }
	toolResMsg     struct{ name string; out string; err error }
	toolLogMsg     struct{ Action, Target, Detail string; Err error }
	diffMsg        struct{ path, diff string }
	requestSentMsg struct{}
)

// NimbusCloudSpinner provides an animated cloud icon with shimmering particles.
var NimbusCloudSpinner = spinner.Spinner{
	Frames: []string{
		"☁ ✦",
		"☁ ✧",
		"☁ ˖",
		"☁ ⁺",
		"☁ ⋆",
		"☁ ⁺",
		"☁ ˖",
		"☁ ✧",
		"☁ ✦",
		"☁ ✧",
	},
	FPS: time.Second / 10,
}

type Model struct {
	Agent                *ai.Agent
	Mode                 Mode
	Viewport             viewport.Model
	TextInput            textinput.Model
	StepInput            textinput.Model
	CustomInput          textinput.Model
	Spinner              spinner.Model
	SelectedStep         int
	IsEditingStep        bool
	StreamBuffer         *strings.Builder
	Messages             []ChatItem
	CurrentDiffs         []string
	History              []string
	HistoryIndex         int
	Width                int
	Height               int
	StatusText           string
	ErrorMessage         string
	OneShot              bool
	Ready                bool
	OriginalPrompt       string
	ClarificationPlan    *ai.PlanSummary
	CurrentQuestionIdx   int
	SelectedOptionIdx    int
	ClarificationAnswers map[string]string
	IsCustomInput        bool
	ExecChan             chan tea.Msg

	// Claude-Code-style thinking indicator state
	IsThinking        bool
	ThinkingStartTime time.Time
	ThinkingVerb      string
	LastThinkingVerb  string
	ThinkingTokens    int
	LastVerbChange    time.Time
}

func (m *Model) startThinking() tea.Cmd {
	m.IsThinking = true
	m.ThinkingStartTime = time.Now()
	m.ThinkingTokens = 0
	m.LastVerbChange = time.Now()
	m.ThinkingVerb = NextRandomVerb(m.LastThinkingVerb)
	return tea.Batch(m.Spinner.Tick, thinkingTickCmd())
}

func (m *Model) stopThinking() {
	m.IsThinking = false
	m.ThinkingTokens = 0
	m.ThinkingVerb = ""
}

func NewModel(agent *ai.Agent, initialPrompt string, oneShot bool) Model {
	ti := textinput.New()
	ti.Placeholder = "Ask Nimbus to build, edit, or explain... (e.g. 'create blog resource')"
	ti.Focus()
	ti.CharLimit = 2000
	ti.Prompt = "❯ "
	ti.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#D97757")).Bold(true)
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#52525B"))
	ti.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#F4F4F5"))
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#D97757"))

	si := textinput.New()
	si.CharLimit = 500
	si.Prompt = "   Edit: "
	si.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#D97757")).Bold(true)
	si.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#F4F4F5"))
	si.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#D97757"))

	ci := textinput.New()
	ci.CharLimit = 500
	ci.Placeholder = "Type custom response..."
	ci.Prompt = "❯ "
	ci.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#D97757")).Bold(true)
	ci.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#F4F4F5"))
	ci.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#D97757"))

	sp := spinner.New()
	sp.Spinner = NimbusCloudSpinner
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#38BDF8")).Bold(true)

	vp := viewport.New(80, 20)

	m := Model{
		Agent:                agent,
		Mode:                 ModeChat,
		Viewport:             vp,
		TextInput:            ti,
		StepInput:            si,
		CustomInput:          ci,
		Spinner:              sp,
		SelectedStep:         0,
		StreamBuffer:         &strings.Builder{},
		Messages:             make([]ChatItem, 0),
		CurrentDiffs:         make([]string, 0),
		History:              make([]string, 0),
		HistoryIndex:         0,
		ClarificationAnswers: make(map[string]string),
		OneShot:              oneShot,
		Ready:                false,
	}

	// Welcome message
	welcome := "✦ **Nimbus AI Copilot** is ready. I can architect features, scaffold resources, review diffs, and run test verification.\n\n" +
		"💡 *Try asking:* `create a blog resource with comments` or `how to add jwt auth middleware`"
	m.Messages = append(m.Messages, ChatItem{
		Role:      "assistant",
		Content:   welcome,
		Timestamp: time.Now(),
	})

	if initialPrompt != "" {
		m.TextInput.SetValue(initialPrompt)
	}

	return m
}

func (m Model) Init() tea.Cmd {
	var cmds []tea.Cmd
	cmds = append(cmds, textinput.Blink, m.Spinner.Tick)

	if m.TextInput.Value() != "" && m.OneShot {
		cmds = append(cmds, m.submitPromptCmd(m.TextInput.Value()))
	}
	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	switch msg := msg.(type) {
	case spinner.TickMsg:
		var spCmd tea.Cmd
		m.Spinner, spCmd = m.Spinner.Update(msg)
		return m, spCmd

	case thinkingTickMsg:
		if !m.IsThinking {
			return m, nil
		}
		if time.Since(m.LastVerbChange) >= 2500*time.Millisecond {
			m.LastThinkingVerb = m.ThinkingVerb
			m.ThinkingVerb = NextRandomVerb(m.LastThinkingVerb)
			m.LastVerbChange = msg.t
		}
		return m, thinkingTickCmd()

	case requestSentMsg:
		m.IsThinking = true
		if m.ThinkingStartTime.IsZero() {
			m.ThinkingStartTime = time.Now()
		}
		m.LastVerbChange = time.Now()
		m.ThinkingVerb = NextRandomVerb(m.LastThinkingVerb)
		return m, tea.Batch(m.listenForExecEventsCmd(), thinkingTickCmd())

	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		headerView := renderHeader(&m)
		footerView := renderFooter(&m)
		headerHeight := lipgloss.Height(headerView)
		footerHeight := lipgloss.Height(footerView)
		vpHeight := msg.Height - headerHeight - footerHeight
		if vpHeight < 4 {
			vpHeight = 4
		}
		if !m.Ready {
			m.Viewport = viewport.New(msg.Width-4, vpHeight)
			m.Ready = true
		} else {
			m.Viewport.Width = msg.Width - 4
			m.Viewport.Height = vpHeight
		}
		m.TextInput.Width = msg.Width - 10
		m.StepInput.Width = msg.Width - 16
		m.updateViewportContent()

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		}

		switch m.Mode {
		case ModeChat:
			switch msg.Type {
			case tea.KeyEnter:
				val := strings.TrimSpace(m.TextInput.Value())
				if val == "" {
					return m, nil
				}
				if val == "/exit" || val == "/quit" || val == "exit" || val == "quit" || val == "q" {
					return m, tea.Quit
				}
				if val == "/clear" || val == "clear" {
					m.Messages = make([]ChatItem, 0)
					m.StreamBuffer = &strings.Builder{}
					m.TextInput.Reset()
					m.updateViewportContent()
					return m, nil
				}
				if val == "/help" || val == "help" {
					m.Messages = append(m.Messages, ChatItem{
						Role:      "assistant",
						Content:   "**Nimbus AI Commands:**\n- `/skills` : List active agent skills\n- `/context` : View scanned project context\n- `/clear` : Clear chat history\n- `/exit` : Quit Nimbus AI\n- `nimbus ai --resume <id>` : Resume previous session",
						Timestamp: time.Now(),
					})
					m.TextInput.Reset()
					m.updateViewportContent()
					return m, nil
				}
				if val == "/skills" {
					skillsContent := "**Loaded Agent Skills (`~/.nimbus/skills`):**\n\n"
					if len(m.Agent.Context.Skills) == 0 {
						skillsContent += "No skills loaded."
					} else {
						for _, s := range m.Agent.Context.Skills {
							skillsContent += fmt.Sprintf("• **`%s`** (%s)\n  %s\n\n", s.Name, s.Source, s.Description)
						}
					}
					m.Messages = append(m.Messages, ChatItem{
						Role:      "assistant",
						Content:   strings.TrimSpace(skillsContent),
						Timestamp: time.Now(),
					})
					m.TextInput.Reset()
					m.updateViewportContent()
					return m, nil
				}
				if val == "/context" {
					m.Messages = append(m.Messages, ChatItem{
						Role:      "assistant",
						Content:   m.Agent.Context.FormatSystemContext(),
						Timestamp: time.Now(),
					})
					m.TextInput.Reset()
					m.updateViewportContent()
					return m, nil
				}

				m.History = append(m.History, val)
				m.HistoryIndex = len(m.History)
				m.Messages = append(m.Messages, ChatItem{
					Role:      "user",
					Content:   val,
					Timestamp: time.Now(),
				})
				m.TextInput.Reset()
				m.updateViewportContent()
				m.OriginalPrompt = val
				return m, m.submitPromptCmd(val)

			case tea.KeyUp:
				if len(m.History) > 0 && m.HistoryIndex > 0 {
					m.HistoryIndex--
					m.TextInput.SetValue(m.History[m.HistoryIndex])
				}
			case tea.KeyDown:
				if m.HistoryIndex < len(m.History)-1 {
					m.HistoryIndex++
					m.TextInput.SetValue(m.History[m.HistoryIndex])
				} else if m.HistoryIndex >= len(m.History)-1 {
					m.HistoryIndex = len(m.History)
					m.TextInput.Reset()
				}
			}

		case ModeClarification:
			if m.ClarificationPlan == nil || len(m.ClarificationPlan.Questions) == 0 {
				m.Mode = ModeChat
				return m, nil
			}

			q := m.ClarificationPlan.Questions[m.CurrentQuestionIdx]

			if m.IsCustomInput {
				switch msg.Type {
				case tea.KeyEnter:
					ans := strings.TrimSpace(m.CustomInput.Value())
					if ans == "" && q.Default != "" {
						ans = q.Default
					}
					if ans != "" {
						m.ClarificationAnswers[q.ID] = ans
						m.IsCustomInput = false
						m.CustomInput.Reset()
						if m.CurrentQuestionIdx < len(m.ClarificationPlan.Questions)-1 {
							m.CurrentQuestionIdx++
							m.SelectedOptionIdx = 0
						} else {
							return m.finishClarification()
						}
					}
				case tea.KeyEsc:
					m.IsCustomInput = false
					m.CustomInput.Reset()
				default:
					m.CustomInput, cmd = m.CustomInput.Update(msg)
					return m, cmd
				}
				return m, nil
			}

			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "up", "k":
				if m.SelectedOptionIdx > 0 {
					m.SelectedOptionIdx--
				}
			case "down", "j":
				if m.SelectedOptionIdx < len(q.Options)-1 {
					m.SelectedOptionIdx++
				}
			case "c":
				m.IsCustomInput = true
				m.CustomInput.Focus()
				return m, nil
			case "1", "2", "3", "4", "5", "6", "7", "8", "9":
				idx := int(msg.String()[0] - '1')
				if idx >= 0 && idx < len(q.Options) {
					m.SelectedOptionIdx = idx
					m.ClarificationAnswers[q.ID] = q.Options[idx]
					if m.CurrentQuestionIdx < len(m.ClarificationPlan.Questions)-1 {
						m.CurrentQuestionIdx++
						m.SelectedOptionIdx = 0
					} else {
						return m.finishClarification()
					}
				}
			case "enter":
				selectedAns := ""
				if m.SelectedOptionIdx >= 0 && m.SelectedOptionIdx < len(q.Options) {
					selectedAns = q.Options[m.SelectedOptionIdx]
				} else if q.Default != "" {
					selectedAns = q.Default
				}
				if selectedAns != "" {
					m.ClarificationAnswers[q.ID] = selectedAns
					if m.CurrentQuestionIdx < len(m.ClarificationPlan.Questions)-1 {
						m.CurrentQuestionIdx++
						m.SelectedOptionIdx = 0
					} else {
						return m.finishClarification()
					}
				}
			case "esc":
				m.Mode = ModeChat
				m.StatusText = "Clarification cancelled."
				m.Messages = append(m.Messages, ChatItem{
					Role:      "assistant",
					Content:   "Clarification cancelled. You can enter a new request or provide more details.",
					Timestamp: time.Now(),
				})
				m.updateViewportContent()
			}

		case ModePlanReview:
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "r", "esc":
				m.Mode = ModeChat
				m.StatusText = "Plan rejected. Enter a new request."
				m.Messages = append(m.Messages, ChatItem{
					Role:      "assistant",
					Content:   "Plan rejected. You can provide additional clarification or ask another question.",
					Timestamp: time.Now(),
				})
				m.updateViewportContent()
			case "enter":
				if m.Agent.Session.Plan != nil {
					for i := range m.Agent.Session.Plan.Steps {
						m.Agent.Session.Plan.Steps[i].Approved = true
					}
				}
				m.Mode = ModeExecuting
				m.StatusText = "Executing approved architectural plan..."
				m.CurrentDiffs = make([]string, 0)
				execCmd := m.executeApprovedPlanCmd() // sets m.ExecChan before m is captured in return
				return m, execCmd
			}

		case ModeCompleted:
			switch msg.Type {
			case tea.KeyEnter, tea.KeyEsc:
				if m.OneShot {
					return m, tea.Quit
				}
				m.Mode = ModeChat
				m.StatusText = "Ready for next request."
				m.updateViewportContent()
			}
		}

	case chatReplyMsg:
		m.Mode = ModeChat
		m.StatusText = ""
		m.stopThinking()
		if msg.err != nil {
			m.Messages = append(m.Messages, ChatItem{
				Role:      "assistant",
				Content:   fmt.Sprintf("❌ **Error:** %v", msg.err),
				Timestamp: time.Now(),
				IsError:   true,
			})
		} else {
			m.Messages = append(m.Messages, ChatItem{
				Role:      "assistant",
				Content:   msg.reply,
				Timestamp: time.Now(),
			})
		}
		m.updateViewportContent()

	case planDeltaMsg:
		m.StreamBuffer.WriteString(msg.delta)

	case planDoneMsg:
		m.stopThinking()
		if msg.err != nil {
			m.Mode = ModeChat
			m.ErrorMessage = fmt.Sprintf("Planning Error: %v", msg.err)
			m.Messages = append(m.Messages, ChatItem{
				Role:      "assistant",
				Content:   fmt.Sprintf("❌ **Planning Error:** %v", msg.err),
				Timestamp: time.Now(),
				IsError:   true,
			})
			m.updateViewportContent()
			return m, nil
		}

		// Check if AI model decided clarification is needed
		if msg.plan != nil && msg.plan.NeedsClarification && len(msg.plan.Questions) > 0 {
			m.Mode = ModeClarification
			m.ClarificationPlan = msg.plan
			m.CurrentQuestionIdx = 0
			m.SelectedOptionIdx = 0
			m.ClarificationAnswers = make(map[string]string)
			m.IsCustomInput = false
			m.CustomInput.Reset()
			m.StatusText = "Please answer a few questions to help refine the architectural plan."
			return m, nil
		}

		// If the plan has no steps, the AI returned a conversational response
		// Display it as a chat message instead of entering Plan Review
		if msg.plan == nil || len(msg.plan.Steps) == 0 {
			m.Mode = ModeChat
			m.StatusText = ""
			reply := "I couldn't generate a specific plan for that request."
			if msg.plan != nil && msg.plan.Summary != "" {
				reply = msg.plan.Summary
			}
			m.Messages = append(m.Messages, ChatItem{
				Role:      "assistant",
				Content:   reply,
				Timestamp: time.Now(),
			})
			m.updateViewportContent()
			return m, nil
		}
		m.Mode = ModePlanReview
		m.SelectedStep = 0
		m.StatusText = "Plan synthesized. Review and approve steps below."

	case toolCallMsg:
		target := ""
		if p, ok := msg.args["path"].(string); ok {
			target = p
		} else if c, ok := msg.args["command"].(string); ok {
			target = c
		}
		m.StatusText = fmt.Sprintf("⚡ %s %s...", msg.name, target)
		return m, m.listenForExecEventsCmd()

	case toolLogMsg:
		if msg.Err != nil {
			m.StatusText = fmt.Sprintf("❌ Failed to %s %s", strings.ToLower(msg.Action), msg.Target)
			m.Messages = append(m.Messages, ChatItem{
				Role:      "tool",
				ToolName:  msg.Action,
				Content:   fmt.Sprintf("%s (Error: %v)", msg.Target, msg.Err),
				Timestamp: time.Now(),
				IsError:   true,
			})
		} else {
			m.StatusText = fmt.Sprintf("✓ %s %s", msg.Action, msg.Target)
			m.Messages = append(m.Messages, ChatItem{
				Role:      "tool",
				ToolName:  msg.Action,
				Content:   msg.Target,
				Timestamp: time.Now(),
			})
		}
		m.updateViewportContent()
		return m, m.listenForExecEventsCmd()

	case diffMsg:
		m.CurrentDiffs = append(m.CurrentDiffs, msg.diff)
		return m, m.listenForExecEventsCmd()

	case execDeltaMsg:
		m.StreamBuffer.WriteString(msg.delta)
		m.ThinkingTokens += EstimateDeltaTokens(msg.delta)
		return m, m.listenForExecEventsCmd()

	case execDoneMsg:
		m.Mode = ModeCompleted
		m.stopThinking()
		if msg.err != nil {
			m.ErrorMessage = fmt.Sprintf("Execution Error: %v", msg.err)
			m.Messages = append(m.Messages, ChatItem{
				Role:      "assistant",
				Content:   fmt.Sprintf("❌ **Execution Error:** %v", msg.err),
				Timestamp: time.Now(),
				IsError:   true,
			})
		} else {
			m.StatusText = "✓ Execution completed successfully!"
			summary := msg.summary
			if summary == "" {
				summary = "✨ All architectural plan steps have been executed and saved to workspace."
			}
			m.Messages = append(m.Messages, ChatItem{
				Role:      "assistant",
				Content:   summary,
				Timestamp: time.Now(),
				Diffs:     m.CurrentDiffs,
			})
		}
		m.updateViewportContent()
	}

	m.TextInput, cmd = m.TextInput.Update(msg)
	cmds = append(cmds, cmd)

	m.Spinner, cmd = m.Spinner.Update(msg)
	cmds = append(cmds, cmd)

	m.Viewport, cmd = m.Viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m *Model) submitChatCmd(prompt string) tea.Cmd {
	m.Mode = ModeChat
	m.StatusText = "Thinking..."
	m.ErrorMessage = ""

	chatCmd := func() tea.Msg {
		ctx := context.Background()
		reply, err := m.Agent.Client.Chat(ctx, prompt, m.Agent.Model, m.Agent.Context)
		return chatReplyMsg{reply: reply, err: err}
	}
	return tea.Batch(chatCmd, m.startThinking())
}

func (m *Model) submitPromptCmd(prompt string) tea.Cmd {
	m.Mode = ModePlanning
	m.StreamBuffer = &strings.Builder{}
	m.StatusText = "Synthesizing architectural plan..."
	m.ErrorMessage = ""

	planCmd := func() tea.Msg {
		ctx := context.Background()
		plan, err := m.Agent.GeneratePlan(ctx, prompt)
		return planDoneMsg{plan: plan, err: err}
	}

	return tea.Batch(planCmd, m.startThinking())
}

func (m *Model) regenerateStepCmd(stepIdx int, newDesc string) tea.Cmd {
	regenCmd := func() tea.Msg {
		ctx := context.Background()
		plan, err := m.Agent.RegenerateStep(ctx, stepIdx, newDesc)
		return planDoneMsg{plan: plan, err: err}
	}
	return tea.Batch(regenCmd, m.startThinking())
}

func (m *Model) executeApprovedPlanCmd() tea.Cmd {
	ch := make(chan tea.Msg, 100)
	m.ExecChan = ch

	// Wire agent callbacks to channel for real-time TUI progress
	m.Agent.Callbacks = ai.AgentCallbacks{
		OnRequestSent: func() {
			ch <- requestSentMsg{}
		},
		OnStreamDelta: func(delta string) {
			ch <- execDeltaMsg{delta: delta}
		},
		OnToolCall: func(toolName string, args map[string]any) {
			action := "ANALYZING"
			switch strings.ToLower(toolName) {
			case "write_file", "create_file", "create", "write":
				action = "CREATING"
			case "edit_file", "edit":
				action = "MODIFYING"
			case "load_skill", "read_skill", "skill":
				action = "LOADING SKILL"
			case "read_file", "read", "grep", "list_dir":
				action = "ANALYZING"
			case "bash", "command", "run_command":
				action = "EXECUTING"
			case "delete_file", "delete":
				action = "DELETING"
			}
			ch <- toolCallMsg{name: action, args: args}
		},
		OnToolResult: func(toolName string, args map[string]any, output string, err error) {
			action := "ANALYZED"
			target := ""
			if p, ok := args["path"].(string); ok && p != "" {
				target = p
			} else if s, ok := args["skill_name"].(string); ok && s != "" {
				target = s
			} else if n, ok := args["name"].(string); ok && n != "" {
				target = n
			} else if f, ok := args["file"].(string); ok && f != "" {
				target = f
			} else if c, ok := args["command"].(string); ok && c != "" {
				target = c
			} else if p, ok := args["pattern"].(string); ok && p != "" {
				target = p
			} else if t, ok := args["target"].(string); ok && t != "" {
				target = t
			}

			switch strings.ToLower(toolName) {
			case "write_file", "create_file", "create", "write":
				if strings.Contains(output, "MODIFIED") {
					action = "MODIFIED"
				} else {
					action = "CREATED"
				}
			case "edit_file", "edit":
				action = "MODIFIED"
			case "load_skill", "read_skill", "skill":
				action = "LOADED SKILL"
			case "read_file", "read", "grep", "list_dir":
				action = "ANALYZED"
			case "bash", "command", "run_command":
				action = "EXECUTED"
			case "delete_file", "delete":
				action = "DELETED"
			}

			ch <- toolLogMsg{
				Action: action,
				Target: target,
				Detail: output,
				Err:    err,
			}
		},
		OnDiffGenerated: func(filePath, diff string) {
			ch <- diffMsg{path: filePath, diff: diff}
		},
	}

	go func() {
		ctx := context.Background()
		summary, err := m.Agent.ExecuteApprovedPlan(ctx, m.Agent.Session.Plan)
		ch <- execDoneMsg{summary: summary, err: err}
	}()

	return tea.Batch(m.listenForExecEventsCmd(), m.startThinking())
}

func (m *Model) listenForExecEventsCmd() tea.Cmd {
	return func() tea.Msg {
		if m.ExecChan == nil {
			return nil
		}
		msg, ok := <-m.ExecChan
		if !ok {
			return nil
		}
		return msg
	}
}

func (m Model) finishClarification() (Model, tea.Cmd) {
	var sb strings.Builder
	sb.WriteString(m.OriginalPrompt)
	sb.WriteString("\n\nArchitectural Choices & Developer Preferences:")
	for _, q := range m.ClarificationPlan.Questions {
		ans := m.ClarificationAnswers[q.ID]
		if ans != "" {
			sb.WriteString(fmt.Sprintf("\n- %s: %s", q.Question, ans))
		}
	}

	refinedPrompt := sb.String()
	m.Mode = ModePlanning
	m.StatusText = "Synthesizing architectural plan with your specified tech stack..."
	return m, m.submitPromptCmd(refinedPrompt)
}

func (m *Model) updateViewportContent() {
	m.Viewport.SetContent(renderChatHistory(m))
	m.Viewport.GotoBottom()
}

func isPlanIntent(prompt string) bool {
	lower := strings.TrimSpace(strings.ToLower(prompt))

	// Analysis, question, inspection, greeting -> direct AI response
	if strings.Contains(lower, "analyze") ||
		strings.Contains(lower, "analyse") ||
		strings.Contains(lower, "explain") ||
		strings.Contains(lower, "what") ||
		strings.Contains(lower, "how") ||
		strings.Contains(lower, "why") ||
		strings.Contains(lower, "tell me") ||
		strings.Contains(lower, "check") ||
		strings.Contains(lower, "review") ||
		strings.Contains(lower, "inspect") ||
		strings.Contains(lower, "who") ||
		strings.Contains(lower, "help") ||
		lower == "hi" || lower == "hello" || lower == "hey" ||
		strings.HasSuffix(lower, "?") {
		return false
	}

	// Explicit code scaffolding / generation commands
	return strings.Contains(lower, "create") ||
		strings.Contains(lower, "generate") ||
		strings.Contains(lower, "scaffold") ||
		strings.Contains(lower, "make:") ||
		strings.Contains(lower, "make ") ||
		strings.Contains(lower, "add model") ||
		strings.Contains(lower, "add controller") ||
		strings.Contains(lower, "add migration") ||
		strings.Contains(lower, "add route") ||
		strings.Contains(lower, "build ") ||
		strings.Contains(lower, "implement ") ||
		strings.Contains(lower, "delete file") ||
		strings.Contains(lower, "remove file")
}
