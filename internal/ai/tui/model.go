package tui

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
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

func (m Mode) busy() bool { return m == ModePlanning || m == ModeExecuting }

// ChatItem represents a structured entry in the transcript.
type ChatItem struct {
	Role      string // "user" | "assistant" | "tool" | "phase" | "error" | "system"
	Content   string
	Timestamp time.Time
	Plan      *ai.PlanSummary
	Diffs     []string
	ToolName  string
	ToolArgs  map[string]any
	IsError   bool
	Detail    string        // short tool-result summary, e.g. "120 lines"
	Diff      string        // inline diff shown under create/edit tool lines
	Elapsed   time.Duration // for phase lines
}

type (
	chatReplyMsg struct {
		reply string
		err   error
	}
	planDeltaMsg struct{ delta string }
	planDoneMsg  struct {
		plan *ai.PlanSummary
		err  error
	}
	execDeltaMsg struct{ delta string }
	execDoneMsg  struct {
		summary string
		err     error
	}
	toolCallMsg struct {
		name string
		args map[string]any
	}
	toolResMsg struct {
		name string
		out  string
		err  error
	}
	toolLogMsg struct {
		Action, Target, Detail string
		Err                    error
		ToolName               string
		Args                   map[string]any
	}
	diffMsg        struct{ path, diff string }
	requestSentMsg struct{}
	statusMsg      struct{ text string }
)

// NimbusCloudSpinner provides an animated cloud icon with shimmering particles.
var NimbusCloudSpinner = spinner.Spinner{
	Frames: []string{"☁ ✦", "☁ ✧", "☁ ˖", "☁ ⁺", "☁ ⋆", "☁ ⁺", "☁ ˖", "☁ ✧", "☁ ✦", "☁ ✧"},
	FPS:    time.Second / 10,
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

	// Phase is the agent's current activity ("Exploring the codebase…").
	Phase      string
	PhaseStart time.Time
	ToolCalls  int
	// FinalSummary is the last completed run's summary (printed after a
	// one-shot run exits the alt screen).
	FinalSummary string

	cancel context.CancelFunc

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
	m.Phase = ""
	m.StatusText = ""
}

func NewModel(agent *ai.Agent, initialPrompt string, oneShot bool) Model {
	ti := textinput.New()
	ti.Placeholder = "Ask Nimbus to build, edit, or explain… (e.g. \"add a comments resource to posts\")"
	ti.Focus()
	ti.CharLimit = 4000
	ti.Prompt = glyphPrompt + " "
	ti.PromptStyle = sAccentBold
	ti.PlaceholderStyle = sDim
	ti.TextStyle = sText
	ti.Cursor.Style = sAccent

	si := textinput.New()
	si.CharLimit = 500
	si.Prompt = "   Edit: "
	si.PromptStyle = sAccentBold
	si.TextStyle = sText
	si.Cursor.Style = sAccent

	ci := textinput.New()
	ci.CharLimit = 500
	ci.Placeholder = "Type your own answer…"
	ci.Prompt = glyphPrompt + " "
	ci.PromptStyle = sAccentBold
	ci.TextStyle = sText
	ci.Cursor.Style = sAccent

	sp := spinner.New()
	sp.Spinner = NimbusCloudSpinner
	sp.Style = sBlue.Bold(true)

	vp := viewport.New(80, 20)

	m := Model{
		Agent:                agent,
		Mode:                 ModeChat,
		Viewport:             vp,
		TextInput:            ti,
		StepInput:            si,
		CustomInput:          ci,
		Spinner:              sp,
		StreamBuffer:         &strings.Builder{},
		Messages:             make([]ChatItem, 0),
		CurrentDiffs:         make([]string, 0),
		History:              make([]string, 0),
		ClarificationAnswers: make(map[string]string),
		OneShot:              oneShot,
	}

	m.Messages = append(m.Messages, ChatItem{Role: "system", Content: welcomeText(agent), Timestamp: time.Now()})

	if initialPrompt != "" {
		m.TextInput.SetValue(initialPrompt)
	}
	return m
}

// welcomeText summarises what the agent knows about the workspace.
func welcomeText(agent *ai.Agent) string {
	if agent == nil || agent.Context == nil {
		return "Nimbus AI is ready."
	}
	c := agent.Context
	var facts []string
	if len(c.Controllers) > 0 {
		facts = append(facts, fmt.Sprintf("%d controllers", len(c.Controllers)))
	}
	if len(c.Models) > 0 {
		facts = append(facts, fmt.Sprintf("%d models", len(c.Models)))
	}
	if len(c.Migrations) > 0 {
		facts = append(facts, fmt.Sprintf("%d migrations", len(c.Migrations)))
	}
	if len(c.InstructionFiles) > 0 {
		facts = append(facts, "instructions from "+strings.Join(c.InstructionFiles, ", "))
	}
	if len(c.Skills) > 0 {
		facts = append(facts, fmt.Sprintf("%d skills", len(c.Skills)))
	}
	summary := ""
	if len(facts) > 0 {
		summary = "  " + strings.Join(facts, " · ")
	}
	name := c.ProjectName
	if name == "" {
		name = "this project"
	}
	return fmt.Sprintf("Working in %s.%s\nI read the code before planning, show every file I touch, and verify the build. Try: \"add a comments resource to posts\" or \"how does auth work here?\"", name, summary)
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{textinput.Blink, m.Spinner.Tick}
	// A prompt given on the command line runs immediately, like `claude "…"`.
	// (Init has a value receiver, so the transcript is updated in Update.)
	if v := strings.TrimSpace(m.TextInput.Value()); v != "" {
		cmds = append(cmds, func() tea.Msg { return submitMsg{prompt: v} })
	}
	return tea.Batch(cmds...)
}

type submitMsg struct{ prompt string }

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

	case submitMsg:
		// Initial prompt from the command line.
		m.History = append(m.History, msg.prompt)
		m.HistoryIndex = len(m.History)
		m.Messages = append(m.Messages, ChatItem{Role: "user", Content: msg.prompt, Timestamp: time.Now()})
		m.TextInput.Reset()
		m.OriginalPrompt = msg.prompt
		m.updateViewportContent()
		return m, m.submitPromptCmd(msg.prompt)

	case requestSentMsg:
		m.IsThinking = true
		if m.ThinkingStartTime.IsZero() {
			m.ThinkingStartTime = time.Now()
		}
		return m, tea.Batch(m.listenForExecEventsCmd(), thinkingTickCmd())

	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		m.resizeViewport()
		m.TextInput.Width = msg.Width - 8
		m.StepInput.Width = msg.Width - 16
		m.CustomInput.Width = msg.Width - 12
		if !m.Ready {
			m.Ready = true
		}
		m.updateViewportContent()

	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			if m.Mode.busy() && m.cancel != nil {
				m.interrupt()
				return m, nil
			}
			return m, tea.Quit
		}

		switch m.Mode {
		case ModePlanning, ModeExecuting:
			switch msg.Type {
			case tea.KeyEsc:
				m.interrupt()
				return m, nil
			case tea.KeyUp, tea.KeyDown, tea.KeyPgUp, tea.KeyPgDown:
				m.Viewport, cmd = m.Viewport.Update(msg)
				return m, cmd
			}
			return m, nil

		case ModeChat:
			switch msg.Type {
			case tea.KeyEnter:
				val := strings.TrimSpace(m.TextInput.Value())
				if val == "" {
					return m, nil
				}
				if handled, quit := m.handleSlashCommand(val); handled {
					m.TextInput.Reset()
					m.updateViewportContent()
					if quit {
						return m, tea.Quit
					}
					return m, nil
				}
				m.History = append(m.History, val)
				m.HistoryIndex = len(m.History)
				m.Messages = append(m.Messages, ChatItem{Role: "user", Content: val, Timestamp: time.Now()})
				m.TextInput.Reset()
				m.OriginalPrompt = val
				m.updateViewportContent()
				return m, m.submitPromptCmd(val)

			case tea.KeyUp:
				if len(m.History) > 0 && m.HistoryIndex > 0 {
					m.HistoryIndex--
					m.TextInput.SetValue(m.History[m.HistoryIndex])
					m.TextInput.CursorEnd()
				}
				return m, nil
			case tea.KeyDown:
				if m.HistoryIndex < len(m.History)-1 {
					m.HistoryIndex++
					m.TextInput.SetValue(m.History[m.HistoryIndex])
					m.TextInput.CursorEnd()
				} else {
					m.HistoryIndex = len(m.History)
					m.TextInput.Reset()
				}
				return m, nil
			case tea.KeyPgUp, tea.KeyPgDown:
				m.Viewport, cmd = m.Viewport.Update(msg)
				return m, cmd
			}

		case ModeClarification:
			return m.updateClarification(msg)

		case ModePlanReview:
			switch msg.String() {
			case "q":
				return m, tea.Quit
			case "r", "esc", "n":
				m.Mode = ModeChat
				m.Messages = append(m.Messages, ChatItem{Role: "system", Content: "Plan rejected. Tell me what to change, or ask for something else.", Timestamp: time.Now()})
				m.updateViewportContent()
				return m, nil
			case "enter", "y", "a":
				if m.Agent.Session.Plan != nil {
					for i := range m.Agent.Session.Plan.Steps {
						m.Agent.Session.Plan.Steps[i].Approved = true
					}
				}
				m.Mode = ModeExecuting
				m.CurrentDiffs = make([]string, 0)
				m.Messages = append(m.Messages, ChatItem{Role: "system", Content: "Plan approved.", Timestamp: time.Now()})
				m.updateViewportContent()
				return m, m.executeApprovedPlanCmd()
			case "up", "down", "pgup", "pgdown", "k", "j":
				m.Viewport, cmd = m.Viewport.Update(msg)
				return m, cmd
			}
			return m, nil

		case ModeCompleted:
			m.Mode = ModeChat
		}

	case chatReplyMsg:
		m.Mode = ModeChat
		m.stopThinking()
		if msg.err != nil {
			m.Messages = append(m.Messages, ChatItem{Role: "error", Content: msg.err.Error(), Timestamp: time.Now(), IsError: true})
		} else {
			m.Messages = append(m.Messages, ChatItem{Role: "assistant", Content: msg.reply, Timestamp: time.Now()})
		}
		m.updateViewportContent()

	case planDeltaMsg:
		m.StreamBuffer.WriteString(msg.delta)

	case statusMsg:
		m.setPhase(msg.text)
		m.updateViewportContent()
		return m, m.listenForExecEventsCmd()

	case planDoneMsg:
		m.finishPhase()
		m.stopThinking()
		m.StreamBuffer = &strings.Builder{}
		if msg.err != nil {
			m.Mode = ModeChat
			m.appendError(msg.err)
			m.updateViewportContent()
			return m, nil
		}
		if msg.plan != nil && msg.plan.NeedsClarification && len(msg.plan.Questions) > 0 {
			m.Mode = ModeClarification
			m.ClarificationPlan = msg.plan
			m.CurrentQuestionIdx = 0
			m.SelectedOptionIdx = 0
			m.ClarificationAnswers = make(map[string]string)
			m.IsCustomInput = false
			m.CustomInput.Reset()
			return m, nil
		}
		if msg.plan == nil || len(msg.plan.Steps) == 0 {
			// Conversational answer.
			m.Mode = ModeChat
			reply := "I couldn't work out a plan for that request."
			if msg.plan != nil && strings.TrimSpace(msg.plan.Summary) != "" {
				reply = msg.plan.Summary
			}
			m.Messages = append(m.Messages, ChatItem{Role: "assistant", Content: reply, Timestamp: time.Now()})
			m.updateViewportContent()
			return m, nil
		}
		m.Mode = ModePlanReview
		m.SelectedStep = 0
		m.Viewport.SetContent(renderPlanView(&m))
		m.Viewport.GotoTop()

	case toolCallMsg:
		target := toolTarget(msg.args)
		m.StatusText = fmt.Sprintf("%s %s", toolVerb(msg.name, msg.args), target)
		return m, m.listenForExecEventsCmd()

	case toolLogMsg:
		m.ToolCalls++
		item := ChatItem{
			Role:      "tool",
			ToolName:  msg.ToolName,
			ToolArgs:  msg.Args,
			Content:   msg.Target,
			Detail:    toolDetail(msg.ToolName, msg.Detail, msg.Err),
			Timestamp: time.Now(),
		}
		if msg.Err != nil {
			item.IsError = true
			item.Detail = msg.Err.Error()
		} else if isCommandFailure(msg.ToolName, msg.Detail) {
			item.IsError = true
		}
		m.Messages = append(m.Messages, item)
		m.StatusText = ""
		m.updateViewportContent()
		return m, m.listenForExecEventsCmd()

	case diffMsg:
		m.CurrentDiffs = append(m.CurrentDiffs, msg.diff)
		// Attach to the tool line it belongs to (the most recent write/edit).
		for i := len(m.Messages) - 1; i >= 0; i-- {
			if m.Messages[i].Role == "tool" && m.Messages[i].Diff == "" && isWriteTool(m.Messages[i].ToolName) {
				m.Messages[i].Diff = msg.diff
				m.Messages[i].Detail = diffStats(msg.diff)
				break
			}
		}
		m.updateViewportContent()
		return m, m.listenForExecEventsCmd()

	case execDeltaMsg:
		m.StreamBuffer.WriteString(msg.delta)
		m.ThinkingTokens += EstimateDeltaTokens(msg.delta)
		m.updateViewportContent()
		return m, m.listenForExecEventsCmd()

	case execDoneMsg:
		m.finishPhase()
		m.stopThinking()
		m.StreamBuffer = &strings.Builder{}
		m.Mode = ModeChat
		if msg.err != nil {
			m.appendError(msg.err)
		} else {
			summary := strings.TrimSpace(msg.summary)
			if summary == "" {
				summary = "Done — all approved steps were executed."
			}
			m.FinalSummary = summary
			m.Messages = append(m.Messages, ChatItem{Role: "assistant", Content: summary, Timestamp: time.Now()})
		}
		m.updateViewportContent()
		if m.OneShot {
			return m, tea.Quit
		}
		return m, nil
	}

	if m.Mode == ModeChat {
		m.TextInput, cmd = m.TextInput.Update(msg)
		cmds = append(cmds, cmd)
	}

	m.Spinner, cmd = m.Spinner.Update(msg)
	cmds = append(cmds, cmd)

	m.Viewport, cmd = m.Viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

// handleSlashCommand executes /commands typed in chat. Returns (handled, quit).
func (m *Model) handleSlashCommand(val string) (bool, bool) {
	switch strings.ToLower(val) {
	case "/exit", "/quit", "exit", "quit", "q":
		return true, true
	case "/clear", "clear":
		m.Messages = make([]ChatItem, 0)
		m.StreamBuffer = &strings.Builder{}
		return true, false
	case "/help", "help", "?":
		m.Messages = append(m.Messages, ChatItem{Role: "system", Timestamp: time.Now(), Content: strings.Join([]string{
			"Commands",
			"  /context   what I know about this project",
			"  /skills    available agent skills",
			"  /session   session id and memory",
			"  /clear     clear the transcript",
			"  /exit      quit",
			"",
			"Keys",
			"  Enter send · ↑/↓ history · PgUp/PgDn scroll · Esc interrupt a running task · Ctrl+C quit",
		}, "\n")})
		return true, false
	case "/skills":
		var sb strings.Builder
		if len(m.Agent.Context.Skills) == 0 {
			sb.WriteString("No skills loaded.")
		} else {
			sb.WriteString("Agent skills (loaded on demand when relevant):\n")
			for _, s := range m.Agent.Context.Skills {
				sb.WriteString(fmt.Sprintf("  • %s — %s\n", s.Name, s.Description))
			}
		}
		m.Messages = append(m.Messages, ChatItem{Role: "system", Content: strings.TrimRight(sb.String(), "\n"), Timestamp: time.Now()})
		return true, false
	case "/context":
		m.Messages = append(m.Messages, ChatItem{Role: "assistant", Content: m.Agent.Context.FormatSystemContext(), Timestamp: time.Now()})
		return true, false
	case "/session":
		s := m.Agent.Session
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Session %s · %d turns remembered\n", s.ID, len(s.Turns)))
		if mem := s.ConversationSummary(); mem != "" {
			sb.WriteString(mem)
		}
		sb.WriteString(fmt.Sprintf("\nResume later with: nimbus ai --resume %s", s.ID))
		m.Messages = append(m.Messages, ChatItem{Role: "system", Content: sb.String(), Timestamp: time.Now()})
		return true, false
	}
	return false, false
}

func (m *Model) updateClarification(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if m.ClarificationPlan == nil || len(m.ClarificationPlan.Questions) == 0 {
		m.Mode = ModeChat
		return m, nil
	}
	q := m.ClarificationPlan.Questions[m.CurrentQuestionIdx]

	advance := func(ans string) (tea.Model, tea.Cmd) {
		m.ClarificationAnswers[q.ID] = ans
		if m.CurrentQuestionIdx < len(m.ClarificationPlan.Questions)-1 {
			m.CurrentQuestionIdx++
			m.SelectedOptionIdx = 0
			return m, nil
		}
		return m.finishClarification()
	}

	if m.IsCustomInput {
		switch msg.Type {
		case tea.KeyEnter:
			ans := strings.TrimSpace(m.CustomInput.Value())
			if ans == "" {
				ans = q.Default
			}
			if ans != "" {
				m.IsCustomInput = false
				m.CustomInput.Reset()
				return advance(ans)
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
	case "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		if m.SelectedOptionIdx > 0 {
			m.SelectedOptionIdx--
		}
	case "down", "j":
		if m.SelectedOptionIdx < len(q.Options)-1 {
			m.SelectedOptionIdx++
		}
	case "c", "o":
		m.IsCustomInput = true
		m.CustomInput.Focus()
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		idx := int(msg.String()[0] - '1')
		if idx >= 0 && idx < len(q.Options) {
			m.SelectedOptionIdx = idx
			return advance(q.Options[idx])
		}
	case "enter":
		ans := q.Default
		if m.SelectedOptionIdx >= 0 && m.SelectedOptionIdx < len(q.Options) {
			ans = q.Options[m.SelectedOptionIdx]
		}
		if ans != "" {
			return advance(ans)
		}
	case "esc":
		m.Mode = ModeChat
		m.Messages = append(m.Messages, ChatItem{Role: "system", Content: "Cancelled. Add more detail to your request and try again.", Timestamp: time.Now()})
		m.updateViewportContent()
	}
	return m, nil
}

// interrupt cancels the running agent task.
func (m *Model) interrupt() {
	if m.cancel != nil {
		m.cancel()
	}
	m.StatusText = "Interrupting…"
}

func (m *Model) appendError(err error) {
	if errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "context canceled") {
		m.Messages = append(m.Messages, ChatItem{Role: "system", Content: "Interrupted.", Timestamp: time.Now()})
		return
	}
	m.Messages = append(m.Messages, ChatItem{Role: "error", Content: err.Error(), Timestamp: time.Now(), IsError: true})
}

// setPhase records a new agent phase as a transcript line.
func (m *Model) setPhase(text string) {
	text = strings.TrimSpace(text)
	if text == "" || text == m.Phase {
		return
	}
	m.finishPhase()
	m.Phase = text
	m.PhaseStart = time.Now()
	m.StatusText = ""
	m.Messages = append(m.Messages, ChatItem{Role: "phase", Content: strings.TrimRight(text, "…."), Timestamp: time.Now()})
}

// finishPhase stamps the elapsed time onto the current phase line.
func (m *Model) finishPhase() {
	if m.Phase == "" {
		return
	}
	for i := len(m.Messages) - 1; i >= 0; i-- {
		if m.Messages[i].Role == "phase" {
			m.Messages[i].Elapsed = time.Since(m.PhaseStart)
			break
		}
	}
	m.Phase = ""
}

func (m *Model) resizeViewport() {
	headerHeight := lipgloss.Height(renderHeader(m))
	footerHeight := lipgloss.Height(renderFooter(m))
	vpHeight := m.Height - headerHeight - footerHeight
	if vpHeight < 4 {
		vpHeight = 4
	}
	if !m.Ready {
		m.Viewport = viewport.New(m.Width, vpHeight)
	} else {
		m.Viewport.Width = m.Width
		m.Viewport.Height = vpHeight
	}
}

func (m *Model) submitChatCmd(prompt string) tea.Cmd {
	m.Mode = ModeChat
	chatCmd := func() tea.Msg {
		reply, err := m.Agent.Client.Chat(context.Background(), prompt, m.Agent.Model, m.Agent.Context)
		return chatReplyMsg{reply: reply, err: err}
	}
	return tea.Batch(chatCmd, m.startThinking())
}

func (m *Model) submitPromptCmd(prompt string) tea.Cmd {
	m.Mode = ModePlanning
	m.StreamBuffer = &strings.Builder{}
	m.ErrorMessage = ""
	m.ToolCalls = 0

	ch, ctx := m.wireAgentCallbacks()
	go func() {
		plan, err := m.Agent.GeneratePlan(ctx, prompt)
		ch <- planDoneMsg{plan: plan, err: err}
	}()
	return tea.Batch(m.listenForExecEventsCmd(), m.startThinking())
}

func (m *Model) regenerateStepCmd(stepIdx int, newDesc string) tea.Cmd {
	regenCmd := func() tea.Msg {
		plan, err := m.Agent.RegenerateStep(context.Background(), stepIdx, newDesc)
		return planDoneMsg{plan: plan, err: err}
	}
	return tea.Batch(regenCmd, m.startThinking())
}

func (m *Model) executeApprovedPlanCmd() tea.Cmd {
	ch, ctx := m.wireAgentCallbacks()
	go func() {
		summary, err := m.Agent.ExecuteApprovedPlan(ctx, m.Agent.Session.Plan)
		ch <- execDoneMsg{summary: summary, err: err}
	}()
	return tea.Batch(m.listenForExecEventsCmd(), m.startThinking())
}

// wireAgentCallbacks creates the event channel and cancellable context for a
// planning or execution run and points the agent's callbacks at it.
func (m *Model) wireAgentCallbacks() (chan tea.Msg, context.Context) {
	ch := make(chan tea.Msg, 256)
	m.ExecChan = ch
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel

	m.Agent.Callbacks = ai.AgentCallbacks{
		OnRequestSent: func() { ch <- requestSentMsg{} },
		OnStatus:      func(text string) { ch <- statusMsg{text: text} },
		OnStreamDelta: func(delta string) { ch <- execDeltaMsg{delta: delta} },
		OnToolCall: func(toolName string, args map[string]any) {
			ch <- toolCallMsg{name: toolName, args: args}
		},
		OnToolResult: func(toolName string, args map[string]any, output string, err error) {
			ch <- toolLogMsg{ToolName: toolName, Args: args, Target: toolTarget(args), Detail: output, Err: err}
		},
		OnDiffGenerated: func(filePath, diff string) { ch <- diffMsg{path: filePath, diff: diff} },
	}
	return ch, ctx
}

func (m *Model) listenForExecEventsCmd() tea.Cmd {
	ch := m.ExecChan
	return func() tea.Msg {
		if ch == nil {
			return nil
		}
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

func (m Model) finishClarification() (Model, tea.Cmd) {
	var sb strings.Builder
	sb.WriteString(m.OriginalPrompt)
	sb.WriteString("\n\nDecisions:")
	for _, q := range m.ClarificationPlan.Questions {
		if ans := m.ClarificationAnswers[q.ID]; ans != "" {
			sb.WriteString(fmt.Sprintf("\n- %s: %s", q.Question, ans))
		}
	}
	m.Messages = append(m.Messages, ChatItem{Role: "system", Content: "Thanks — planning with your choices.", Timestamp: time.Now()})
	m.updateViewportContent()
	return m, m.submitPromptCmd(sb.String())
}

func (m *Model) updateViewportContent() {
	if m.Mode == ModePlanReview {
		m.Viewport.SetContent(renderPlanView(m))
		return
	}
	m.Viewport.SetContent(renderChatHistory(m))
	m.Viewport.GotoBottom()
}

// ---------------------------------------------------------------------------
// Tool-line helpers
// ---------------------------------------------------------------------------

var reLinesInfo = regexp.MustCompile(`\((\d+) lines`)

// toolVerb maps a tool to the verb shown on its transcript line.
func toolVerb(name string, args map[string]any) string {
	switch strings.ToLower(name) {
	case "read_file", "read":
		return "Read"
	case "grep", "search":
		return "Search"
	case "find_files", "glob":
		return "Glob"
	case "list_dir":
		return "List"
	case "write_file", "create_file", "create", "write":
		return "Write"
	case "edit_file", "edit":
		return "Edit"
	case "delete_file", "delete":
		return "Delete"
	case "bash", "command", "run_command", "shell":
		return "Bash"
	case "load_skill", "read_skill", "skill", "query_skill":
		return "Skill"
	}
	return strings.Title(strings.ReplaceAll(name, "_", " "))
}

func toolTarget(args map[string]any) string {
	for _, k := range []string{"path", "command", "pattern", "skill_name", "name", "file", "target"} {
		if v, ok := args[k].(string); ok && strings.TrimSpace(v) != "" {
			v = strings.TrimSpace(v)
			if k == "pattern" {
				return "\"" + v + "\""
			}
			return v
		}
	}
	return ""
}

func isWriteTool(name string) bool {
	switch strings.ToLower(name) {
	case "write_file", "create_file", "create", "write", "edit_file", "edit", "delete_file", "delete":
		return true
	}
	return false
}

func isCommandFailure(name, out string) bool {
	switch strings.ToLower(name) {
	case "bash", "command", "run_command", "shell":
		return strings.HasPrefix(out, "Command failed") || strings.HasPrefix(out, "Command timed out")
	}
	return false
}

// toolDetail summarises a tool result for the transcript line.
func toolDetail(name, out string, err error) string {
	if err != nil {
		return ""
	}
	lower := strings.ToLower(name)
	switch lower {
	case "read_file", "read":
		if m := reLinesInfo.FindStringSubmatch(out); len(m) == 2 {
			return m[1] + " lines"
		}
	case "grep", "search":
		if strings.HasPrefix(out, "No matches") {
			return "no matches"
		}
		return strconv.Itoa(countLines(out)) + " matches"
	case "find_files", "glob":
		if strings.HasPrefix(out, "No files") {
			return "no files"
		}
		return strconv.Itoa(countLines(out)) + " files"
	case "list_dir":
		n := countLines(out) - 1
		if n < 0 {
			n = 0
		}
		return strconv.Itoa(n) + " entries"
	case "bash", "command", "run_command", "shell":
		if strings.HasPrefix(out, "Command failed") {
			return "failed"
		}
		if strings.HasPrefix(out, "Command timed out") {
			return "timed out"
		}
		return "ok"
	case "load_skill", "read_skill", "skill", "query_skill":
		return "loaded"
	case "delete_file", "delete":
		return "deleted"
	case "write_file", "create_file", "create", "write":
		if strings.HasPrefix(out, "MODIFIED") {
			return "rewrote"
		}
		return "created"
	case "edit_file", "edit":
		return "edited"
	}
	return ""
}

func countLines(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

// diffStats returns "+a −b" for a unified diff.
func diffStats(diff string) string {
	add, del := 0, 0
	for _, l := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(l, "+++"), strings.HasPrefix(l, "---"):
		case strings.HasPrefix(l, "+"):
			add++
		case strings.HasPrefix(l, "-"):
			del++
		}
	}
	return fmt.Sprintf("+%d −%d", add, del)
}

func isPlanIntent(prompt string) bool {
	lower := strings.TrimSpace(strings.ToLower(prompt))
	for _, q := range []string{"analyze", "analyse", "explain", "what", "how", "why", "tell me", "check", "review", "inspect", "who", "help"} {
		if strings.Contains(lower, q) {
			return false
		}
	}
	if strings.HasSuffix(lower, "?") {
		return false
	}
	for _, k := range []string{"create", "generate", "scaffold", "make:", "make ", "add ", "build ", "implement ", "delete file", "remove file", "fix ", "refactor", "update "} {
		if strings.Contains(lower, k) {
			return true
		}
	}
	return false
}
