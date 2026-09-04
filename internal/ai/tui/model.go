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
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
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
	// ModeCommandApproval pauses a run to ask about a command the policy
	// flagged (see internal/ai/command_policy.go).
	ModeCommandApproval
	// ModeSettings is the /settings screen (see settings_view.go).
	ModeSettings
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
	Agent    *ai.Agent
	Mode     Mode
	Viewport viewport.Model
	// TextInput is a textarea, not a single-line input: prompts are often
	// several sentences or contain pasted code, and a box that cannot grow
	// hides everything above the last line.
	TextInput          textarea.Model
	StepInput          textinput.Model
	CustomInput        textinput.Model
	Spinner            spinner.Model
	SelectedStep       int
	IsEditingStep      bool
	StreamBuffer       *strings.Builder
	Messages           []ChatItem
	CurrentDiffs       []string
	History            []string
	HistoryIndex       int
	Width              int
	Height             int
	StatusText         string
	ErrorMessage       string
	OneShot            bool
	Ready              bool
	OriginalPrompt     string
	ClarificationPlan  *ai.PlanSummary
	CurrentQuestionIdx int
	PendingApproval    *pendingApproval
	// PlanFirst routes requests through the staged plan → approve → execute
	// pipeline instead of the conversational loop (nimbus ai --plan-only).
	PlanFirst    bool
	SessionUsage ai.SessionUsage
	// ExpandDiffs shows full diffs instead of the collapsed change summary,
	// toggled with ctrl+o.
	ExpandDiffs bool
	// PaletteIndex is the highlighted row of the slash-command menu, which is
	// open whenever the input begins with "/".
	PaletteIndex int
	// QueuedPrompt is a message typed while the agent was working. It is sent
	// as soon as the current turn finishes, so a thought does not have to wait
	// for the run to end before it can be written down.
	QueuedPrompt string
	// segmentStart marks when the model was last asked something, so each
	// stretch of thinking can be reported as its own "Thought for Xs".
	segmentStart time.Time
	// thoughtLogged guards against recording that line more than once per
	// stretch — a turn can produce narration and then several tool calls.
	thoughtLogged        bool
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

	// SettingsUI is the /settings screen's state, live only in ModeSettings.
	SettingsUI settingsState

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

// Input box height: one line at rest, growing with the text up to a cap so a
// long prompt stays visible without swallowing the transcript.
const (
	inputMinHeight = 1
	inputMaxHeight = 8
)

func NewModel(agent *ai.Agent, initialPrompt string, oneShot bool) Model {
	ti := textarea.New()
	ti.Placeholder = "Ask Nimbus to build, edit, or explain… (e.g. \"add a comments resource to posts\")"
	ti.Focus()
	ti.CharLimit = 8000
	ti.Prompt = glyphPrompt + " "
	ti.ShowLineNumbers = false
	ti.SetHeight(inputMinHeight)
	// Enter sends the message; a newline is Alt+Enter or Ctrl+J, so a prompt
	// can span lines without submitting halfway through.
	ti.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter", "ctrl+j"))
	ti.FocusedStyle.Prompt = sAccentBold
	ti.FocusedStyle.Placeholder = sDim
	ti.FocusedStyle.Text = sText
	ti.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ti.BlurredStyle.Prompt = sAccentBold
	ti.BlurredStyle.Text = sText
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

	// The banner comes first, then any conversation being resumed: a restored
	// session should read top to bottom in the order it happened, with the
	// greeting above the history rather than below it.
	m.show(ChatItem{Role: "system", Content: welcomeText(agent), Timestamp: time.Now()})
	m.Messages = append(m.Messages, restoreTranscript(agent.Session)...)

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
	case tea.MouseMsg:
		// Only the wheel scrolls. Clicks and drags are left alone so the
		// terminal's own selection behaviour is not second-guessed.
		if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
			m.Viewport, cmd = m.Viewport.Update(msg)
			return m, cmd
		}
		return m, nil

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
		m.say(ChatItem{Role: "user", Content: msg.prompt, Timestamp: time.Now()})
		m.TextInput.Reset()
		m.OriginalPrompt = msg.prompt
		m.updateViewportContent()
		return m, m.submitPromptCmd(msg.prompt)

	case compactDoneMsg:
		m.finishPhase()
		m.stopThinking()
		m.Mode = ModeChat
		if msg.err != nil {
			m.appendError(fmt.Errorf("could not compact: %w", msg.err))
		} else if msg.result != nil {
			usage := m.Agent.Session.ContextUsage()
			if msg.result.Summarised == 0 {
				m.say(ChatItem{
					Role:      "system",
					Content:   fmt.Sprintf("Nothing to compact yet — the conversation is %d%% of the context window.", usage.Percent()),
					Timestamp: time.Now(),
				})
			} else {
				m.say(ChatItem{
					Role: "system",
					Content: fmt.Sprintf("Compacted %d messages into a summary — about %s tokens reclaimed, now %d%% of the window.",
						msg.result.Summarised, ai.FormatTokens(msg.result.Saved()), usage.Percent()),
					Timestamp: time.Now(),
				})
			}
		}
		m.updateViewportContent()
		return m, nil

	case usageMsg:
		m.SessionUsage = msg.session
		return m, m.listenForExecEventsCmd()

	case approvalRequestMsg:
		prev := m.Mode
		if prev == ModeCommandApproval {
			prev = ModeExecuting
		}
		m.PendingApproval = &pendingApproval{
			Command: msg.command,
			Reason:  msg.reason,
			reply:   msg.reply,
			prev:    prev,
		}
		m.Mode = ModeCommandApproval
		m.updateViewportContent()
		return m, m.listenForExecEventsCmd()

	case requestSentMsg:
		m.IsThinking = true
		if m.ThinkingStartTime.IsZero() {
			m.ThinkingStartTime = time.Now()
		}
		m.segmentStart = time.Now()
		m.thoughtLogged = false
		return m, tea.Batch(m.listenForExecEventsCmd(), thinkingTickCmd())

	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		m.resizeViewport()
		m.TextInput.SetWidth(msg.Width - 8)
		m.StepInput.Width = msg.Width - 16
		m.CustomInput.Width = msg.Width - 12
		if !m.Ready {
			m.Ready = true
		}
		m.updateViewportContent()

	case tea.KeyMsg:
		// ctrl+o toggles between the collapsed change summary and full diffs.
		if msg.String() == "ctrl+o" {
			m.ExpandDiffs = !m.ExpandDiffs
			m.updateViewportContent()
			return m, nil
		}
		if msg.Type == tea.KeyCtrlC {
			if m.Mode.busy() && m.cancel != nil {
				m.interrupt()
				return m, nil
			}
			return m, tea.Quit
		}

		switch m.Mode {
		case ModeSettings:
			return m.updateSettings(msg)

		case ModeCommandApproval:
			switch strings.ToLower(msg.String()) {
			case "y", "enter":
				m.answerApproval(true)
			case "n", "esc":
				m.answerApproval(false)
			}
			return m, nil

		case ModePlanning, ModeExecuting:
			switch msg.Type {
			case tea.KeyEsc:
				// Interrupting hands back whatever was being typed, so a
				// redirection can be edited rather than retyped.
				if m.QueuedPrompt != "" {
					m.TextInput.SetValue(m.QueuedPrompt)
					m.QueuedPrompt = ""
					m.growInput()
				}
				m.interrupt()
				return m, nil

			case tea.KeyPgUp, tea.KeyPgDown:
				m.Viewport, cmd = m.Viewport.Update(msg)
				return m, cmd

			case tea.KeyEnter:
				if msg.Alt {
					break // newline, handled by the textarea below
				}
				val := strings.TrimSpace(m.TextInput.Value())
				if val == "" {
					return m, nil
				}
				m.QueuedPrompt = val
				m.TextInput.Reset()
				m.growInput()
				m.say(ChatItem{
					Role:      "system",
					Content:   "Queued — I'll pick this up when the current task finishes: " + val,
					Timestamp: time.Now(),
				})
				m.updateViewportContent()
				return m, nil
			}
			// Reading back matters most here, while output is still arriving.
			switch msg.String() {
			case "shift+up":
				m.Viewport.LineUp(1)
				return m, nil
			case "shift+down":
				m.Viewport.LineDown(1)
				return m, nil
			}

			// Anything else is typing: let the input have it so a message can
			// be composed while the agent works.
			m.TextInput, cmd = m.TextInput.Update(msg)
			m.growInput()
			return m, cmd

		case ModeChat:
			// The command menu takes the navigation keys while it is open, so
			// a command can be picked without knowing its name.
			if matches := matchCommands(m.TextInput.Value()); len(matches) > 0 {
				switch msg.Type {
				case tea.KeyUp:
					if m.PaletteIndex > 0 {
						m.PaletteIndex--
					}
					return m, nil
				case tea.KeyDown:
					if m.PaletteIndex < len(matches)-1 {
						m.PaletteIndex++
					}
					return m, nil
				case tea.KeyTab:
					m.completeCommand(matches)
					return m, nil
				case tea.KeyEsc:
					m.TextInput.Reset()
					m.PaletteIndex = 0
					m.growInput()
					return m, nil
				case tea.KeyEnter:
					if !msg.Alt {
						// Enter on a highlighted row runs that command, so a
						// half-typed name does the obvious thing.
						m.completeCommand(matches)
					}
				}
			}

			switch msg.Type {
			case tea.KeyEnter:
				// alt+enter arrives as Enter with the Alt flag set; it inserts
				// a newline, so it must fall through to the textarea rather
				// than submitting a half-written prompt.
				if msg.Alt {
					break
				}
				val := strings.TrimSpace(m.TextInput.Value())
				if val == "" {
					return m, nil
				}
				if handled, quit, slashCmd := m.handleSlashCommand(val); handled {
					m.TextInput.Reset()
					m.growInput()
					m.updateViewportContent()
					if quit {
						return m, tea.Quit
					}
					return m, slashCmd
				}
				m.History = append(m.History, val)
				m.HistoryIndex = len(m.History)
				m.say(ChatItem{Role: "user", Content: val, Timestamp: time.Now()})
				m.TextInput.Reset()
				m.growInput()
				m.OriginalPrompt = val
				m.updateViewportContent()
				m.scrollToBottom()
				return m, m.submitPromptCmd(val)

			case tea.KeyUp:
				// Inside a multi-line prompt the arrows move the cursor;
				// history is only recalled from the first line.
				if m.TextInput.Line() > 0 {
					break
				}
				if len(m.History) > 0 && m.HistoryIndex > 0 {
					m.HistoryIndex--
					m.TextInput.SetValue(m.History[m.HistoryIndex])
					m.TextInput.CursorEnd()
				}
				return m, nil
			case tea.KeyDown:
				if m.TextInput.Line() < m.TextInput.LineCount()-1 {
					break
				}
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

			// Line-at-a-time scrolling. Plain ↑/↓ recall history and the
			// letter keys have to stay typeable, so the shifted arrows are
			// what is left for reading back through the transcript.
			switch msg.String() {
			case "shift+up":
				m.Viewport.LineUp(1)
				return m, nil
			case "shift+down":
				m.Viewport.LineDown(1)
				return m, nil
			}

		case ModeClarification:
			return m.updateClarification(msg)

		case ModePlanReview:
			switch msg.String() {
			case "q":
				return m, tea.Quit
			case "r", "esc", "n":
				m.Mode = ModeChat
				m.say(ChatItem{Role: "system", Content: "Plan rejected. Tell me what to change, or ask for something else.", Timestamp: time.Now()})
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
				m.say(ChatItem{Role: "system", Content: "Plan approved.", Timestamp: time.Now()})
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
			m.say(ChatItem{Role: "error", Content: msg.err.Error(), Timestamp: time.Now(), IsError: true})
		} else {
			m.say(ChatItem{Role: "assistant", Content: msg.reply, Timestamp: time.Now()})
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
			m.say(ChatItem{Role: "assistant", Content: reply, Timestamp: time.Now()})
			m.updateViewportContent()
			if cmd := m.dispatchQueued(); cmd != nil {
				return m, cmd
			}
			return m, nil
		}
		m.Mode = ModePlanReview
		m.SelectedStep = 0
		m.Viewport.SetContent(renderPlanView(&m))
		m.Viewport.GotoTop()

	case toolCallMsg:
		// Commit whatever the model said before acting, so the reasoning sits
		// above the tool lines it explains instead of trailing the whole run.
		m.recordThought()
		m.flushNarration()
		target := toolTarget(msg.args)
		m.StatusText = fmt.Sprintf("%s %s", toolVerb(msg.name, msg.args), target)
		m.updateViewportContent()
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
		m.say(item)
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
				// The diff arrives after the tool line was recorded, so the
				// stored copy has to be caught up or a reopened session shows
				// the change with no diff under it.
				m.updateStoredDiff(m.Messages[i])
				break
			}
		}
		m.updateViewportContent()
		return m, m.listenForExecEventsCmd()

	case execDeltaMsg:
		// The model has started writing, so the silent thinking for this
		// stretch is over: record how long it took, then stream the text.
		m.recordThought()
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
				summary = "Done."
			}
			m.FinalSummary = summary
			m.say(ChatItem{Role: "assistant", Content: summary, Timestamp: time.Now()})
		}
		m.updateViewportContent()
		if m.OneShot {
			return m, tea.Quit
		}
		if cmd := m.dispatchQueued(); cmd != nil {
			return m, cmd
		}
		return m, nil
	}

	if m.Mode == ModeChat {
		before := m.TextInput.Value()
		m.TextInput, cmd = m.TextInput.Update(msg)
		cmds = append(cmds, cmd)
		m.growInput()
		if m.TextInput.Value() != before {
			// Typing narrows the list, so the highlight returns to the top
			// rather than pointing at whatever row it happened to be on.
			m.PaletteIndex = 0
		}
	}

	m.Spinner, cmd = m.Spinner.Update(msg)
	cmds = append(cmds, cmd)

	m.Viewport, cmd = m.Viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

// handleSlashCommand executes /commands typed in chat.
//
// Returns whether the input was a command, whether the app should quit, and
// any work to start — /compact calls the model, so it runs as a background
// task like any other rather than freezing the interface.
func (m *Model) handleSlashCommand(val string) (handled, quit bool, cmd tea.Cmd) {
	switch strings.ToLower(val) {
	case "/exit", "/quit", "exit", "quit", "q":
		return true, true, nil
	case "/clear", "clear":
		m.Messages = make([]ChatItem, 0)
		m.StreamBuffer = &strings.Builder{}
		return true, false, nil
	case "/help", "help", "?":
		m.say(ChatItem{Role: "system", Timestamp: time.Now(),
			Content: strings.Join(helpLines(), "\n")})
		return true, false, nil
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
		m.say(ChatItem{Role: "system", Content: strings.TrimRight(sb.String(), "\n"), Timestamp: time.Now()})
		return true, false, nil
	case "/context":
		m.say(ChatItem{Role: "assistant", Content: m.Agent.Context.FormatSystemContext(), Timestamp: time.Now()})
		return true, false, nil
	case "/settings", "/config":
		m.openSettings()
		return true, false, nil
	case "/compact":
		// Compaction calls the model, so it runs like any other task rather
		// than blocking the interface.
		m.Mode = ModeExecuting
		ch, ctx := m.wireAgentCallbacks()
		go func() {
			res, err := m.Agent.Compact(ctx)
			ch <- compactDoneMsg{result: res, err: err}
		}()
		m.say(ChatItem{Role: "system", Content: "Compacting the conversation…", Timestamp: time.Now()})
		return true, false, tea.Batch(m.listenForExecEventsCmd(), m.startThinking())

	case "/sessions":
		sessions, err := ai.ListSessions(m.appRoot())
		var sb strings.Builder
		switch {
		case err != nil:
			sb.WriteString("Could not read saved sessions: " + err.Error())
		case len(sessions) == 0:
			sb.WriteString("No saved sessions in this project yet.")
		default:
			now := time.Now()
			sb.WriteString("Sessions in this project (newest first):\n")
			for i, s := range sessions {
				if i >= 15 {
					sb.WriteString(fmt.Sprintf("  … and %d more\n", len(sessions)-i))
					break
				}
				marker := "  "
				if s.ID == m.Agent.Session.ID {
					marker = sAccent.Render(glyphDot) + " "
				}
				sb.WriteString(marker + s.Describe(now) + "\n")
			}
			sb.WriteString("\nResume one with:  nimbus ai --resume <id>")
		}
		m.say(ChatItem{Role: "system", Content: strings.TrimRight(sb.String(), "\n"), Timestamp: time.Now()})
		return true, false, nil

	case "/session":
		s := m.Agent.Session
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Session %s · %d turns remembered\n", s.ID, len(s.Turns)))
		if mem := s.ConversationSummary(); mem != "" {
			sb.WriteString(mem)
		}
		sb.WriteString(fmt.Sprintf("\nResume later with: nimbus ai --resume %s", s.ID))
		m.say(ChatItem{Role: "system", Content: sb.String(), Timestamp: time.Now()})
		return true, false, nil
	}
	return false, false, nil
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
		m.say(ChatItem{Role: "system", Content: "Cancelled. Add more detail to your request and try again.", Timestamp: time.Now()})
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
		m.say(ChatItem{Role: "system", Content: "Interrupted.", Timestamp: time.Now()})
		return
	}
	m.say(ChatItem{Role: "error", Content: err.Error(), Timestamp: time.Now(), IsError: true})
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
	m.say(ChatItem{Role: "phase", Content: strings.TrimRight(text, "…."), Timestamp: time.Now()})
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

// submitPromptCmd sends the user's message into the ongoing conversation.
//
// There is no planning phase in the default flow: the model reads the whole
// dialogue and decides whether to answer, investigate or change code. Callers
// that explicitly want a reviewable plan set PlanFirst.
func (m *Model) submitPromptCmd(prompt string) tea.Cmd {
	m.StreamBuffer = &strings.Builder{}
	m.ErrorMessage = ""
	m.ToolCalls = 0

	if m.PlanFirst {
		m.Mode = ModePlanning
		ch, ctx := m.wireAgentCallbacks()
		go func() {
			plan, err := m.Agent.GeneratePlan(ctx, prompt)
			ch <- planDoneMsg{plan: plan, err: err}
		}()
		return tea.Batch(m.listenForExecEventsCmd(), m.startThinking())
	}

	m.Mode = ModeExecuting
	ch, ctx := m.wireAgentCallbacks()
	go func() {
		res, err := m.Agent.Run(ctx, prompt)
		summary := ""
		if res != nil {
			summary = res.Text
		}
		ch <- execDoneMsg{summary: summary, err: err}
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
		OnUsage: func(turn *ai.TokenUsage, session ai.SessionUsage) {
			ch <- usageMsg{session: session}
		},
	}

	// A flagged command pauses the run and asks. This runs on the agent's
	// goroutine and blocks until the user answers, which is what makes the
	// approval meaningful: the command does not start until then.
	if m.Agent.Tools != nil {
		m.Agent.Tools.ApproveCommand = func(cmd, reason string) bool {
			reply := make(chan bool, 1)
			ch <- approvalRequestMsg{command: cmd, reason: reason, reply: reply}
			select {
			case allow := <-reply:
				return allow
			case <-ctx.Done(): // interrupted while the prompt was open
				return false
			}
		}
	}

	return ch, ctx
}

// recordThought writes a "Thought for 8s" line once per stretch of thinking,
// so the transcript keeps a trace of where the time went after the live
// indicator is gone.
func (m *Model) recordThought() {
	if m.thoughtLogged || m.segmentStart.IsZero() {
		return
	}
	m.thoughtLogged = true

	elapsed := time.Since(m.segmentStart)
	// Sub-second thinking is noise, not information.
	if elapsed < time.Second {
		return
	}
	m.say(ChatItem{
		Role:      "phase",
		Content:   "Thought",
		Elapsed:   elapsed,
		Timestamp: time.Now(),
	})
}

// flushNarration moves streamed assistant text into the transcript as a
// committed message, leaving the buffer empty for the next stretch.
func (m *Model) flushNarration() {
	if m.StreamBuffer == nil {
		return
	}
	text := strings.TrimSpace(m.StreamBuffer.String())
	m.StreamBuffer = &strings.Builder{}
	if text == "" {
		return
	}
	m.say(ChatItem{
		Role:      "assistant",
		Content:   text,
		Timestamp: time.Now(),
	})
}

// completeCommand replaces what has been typed with the highlighted command.
func (m *Model) completeCommand(matches []SlashCommand) {
	if len(matches) == 0 {
		return
	}
	if m.PaletteIndex >= len(matches) {
		m.PaletteIndex = len(matches) - 1
	}
	m.TextInput.SetValue(matches[m.PaletteIndex].Name)
	m.TextInput.CursorEnd()
	m.PaletteIndex = 0
	m.growInput()
}

// show displays a line without recording it. For output regenerated on every
// open — the welcome banner — which would otherwise accumulate one copy per
// reopen and, worse, count as a transcript before the real one is restored.
func (m *Model) show(item ChatItem) {
	if item.Timestamp.IsZero() {
		item.Timestamp = time.Now()
	}
	m.Messages = append(m.Messages, item)
}

// say shows a line and records it in the session transcript, so reopening the
// session shows the conversation as it happened — including the notices that
// never go to the model.
func (m *Model) say(item ChatItem) {
	if item.Timestamp.IsZero() {
		item.Timestamp = time.Now()
	}
	m.Messages = append(m.Messages, item)
	if m.Agent != nil && m.Agent.Session != nil {
		m.Agent.Session.AppendTranscript(toTranscriptEntry(item))
	}
}

// updateStoredDiff catches the stored transcript up with a tool line that
// gained its diff after being recorded.
func (m *Model) updateStoredDiff(item ChatItem) {
	if m.Agent == nil || m.Agent.Session == nil {
		return
	}
	entries := m.Agent.Session.Transcript
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Role == "tool" && entries[i].Diff == "" && entries[i].ToolName == item.ToolName {
			entries[i].Diff = item.Diff
			entries[i].Detail = item.Detail
			m.Agent.Session.Transcript = entries
			return
		}
	}
}

// dispatchQueued sends a message typed while the agent was working, now that
// the turn has finished. Returns nil when nothing is waiting.
func (m *Model) dispatchQueued() tea.Cmd {
	queued := strings.TrimSpace(m.QueuedPrompt)
	m.QueuedPrompt = ""
	if queued == "" {
		return nil
	}

	m.History = append(m.History, queued)
	m.HistoryIndex = len(m.History)
	m.say(ChatItem{Role: "user", Content: queued, Timestamp: time.Now()})
	m.OriginalPrompt = queued
	m.updateViewportContent()
	return m.submitPromptCmd(queued)
}

// growInput resizes the prompt box to the text it holds, between one line and
// inputMaxHeight. Without this the box stays one line tall and everything
// above the last line is invisible while typing.
func (m *Model) growInput() {
	lines := m.TextInput.LineCount()
	if lines < inputMinHeight {
		lines = inputMinHeight
	}
	if lines > inputMaxHeight {
		lines = inputMaxHeight
	}
	if lines != m.TextInput.Height() {
		m.TextInput.SetHeight(lines)
		m.resizeViewport()
	}
}

// AppRoot is the workspace the agent is operating in, used to resolve tool
// paths into clickable links.
func (m *Model) AppRoot() string {
	if m.Agent != nil && m.Agent.Context != nil {
		return m.Agent.Context.AppRoot
	}
	if m.Agent != nil && m.Agent.Tools != nil {
		return m.Agent.Tools.AppRoot
	}
	return ""
}

// pendingApproval is a command waiting on the user's yes/no.
type pendingApproval struct {
	Command string
	Reason  string
	reply   chan bool
	prev    Mode
}

// compactDoneMsg reports the outcome of a compaction.
type compactDoneMsg struct {
	result *ai.CompactResult
	err    error
}

// usageMsg carries the running token/cost total reported by the server.
type usageMsg struct{ session ai.SessionUsage }

// approvalRequestMsg reaches the TUI when the agent hits a flagged command.
// The agent's goroutine blocks on reply until a key is pressed.
type approvalRequestMsg struct {
	command string
	reason  string
	reply   chan bool
}

// answerApproval releases the waiting agent goroutine and restores the mode.
func (m *Model) answerApproval(allow bool) {
	if m.PendingApproval == nil {
		return
	}
	select {
	case m.PendingApproval.reply <- allow:
	default:
	}
	verdict := "Declined"
	if allow {
		verdict = "Approved"
	}
	m.say(ChatItem{
		Role:      "system",
		Content:   fmt.Sprintf("%s: %s", verdict, m.PendingApproval.Command),
		Timestamp: time.Now(),
	})
	m.Mode = m.PendingApproval.prev
	m.PendingApproval = nil
	m.updateViewportContent()
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
	m.say(ChatItem{Role: "system", Content: "Thanks — planning with your choices.", Timestamp: time.Now()})
	m.updateViewportContent()
	return m, m.submitPromptCmd(sb.String())
}

func (m *Model) updateViewportContent() {
	if m.Mode == ModePlanReview {
		m.Viewport.SetContent(renderPlanView(m))
		return
	}
	// Follow the tail only when the view is already at the tail.
	//
	// Scrolling up is a deliberate act — reading a diff or an error that just
	// went past — and this used to jump back to the bottom on every tool
	// result, which made the transcript impossible to read while the agent was
	// working. AtBottom has to be sampled before the content is replaced.
	follow := m.Viewport.AtBottom()
	m.Viewport.SetContent(renderChatHistory(m))
	if follow {
		m.Viewport.GotoBottom()
	}
}

// scrollToBottom returns to the live end of the transcript, whatever the
// reader had scrolled back to. Sending a message is a statement that they are
// done reading history.
func (m *Model) scrollToBottom() {
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
