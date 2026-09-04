package ai

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Session represents an AI session stored on disk.
type Session struct {
	ID           string            `json:"id"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
	InitialQuery string            `json:"initial_query"`
	Model        string            `json:"model"`
	Plan         *PlanSummary      `json:"plan,omitempty"`
	ApprovedPlan *PlanSummary      `json:"approved_plan,omitempty"`
	History      []Message         `json:"history"`
	AppliedSteps []int             `json:"applied_steps"`
	LoadedSkills map[string]string `json:"loaded_skills,omitempty"`
	Status       string            `json:"status"` // "planning" | "reviewing" | "executing" | "completed"
	// Findings is the exploration report produced for the current request.
	Findings string `json:"findings,omitempty"`
	// Turns is the conversation memory: one record per completed request,
	// carried into later prompts so follow-ups build on earlier work.
	Turns []TurnRecord `json:"turns,omitempty"`
	// Usage totals every model turn in this session, so a resumed session
	// keeps reporting what it has cost so far.
	Usage SessionUsage `json:"usage"`
	// ContextOverhead is the part of a request that Messages does not
	// account for — the system prompt, the tool schemas and the project
	// context — measured against what the server actually counted. See
	// RecordContextTokens.
	ContextOverhead int `json:"context_overhead,omitempty"`
	// Transcript is what the user saw, kept separately from Messages.
	//
	// Messages is the model's context: compaction rewrites it and pruning
	// drops from it, both of which are right for fitting a context window and
	// wrong for a record of the session. The transcript is append-only, so
	// reopening a session shows the conversation as it happened — including
	// the notices that never went to the model at all.
	Transcript []TranscriptEntry `json:"transcript,omitempty"`
	// Messages is the live conversation: user turns, assistant replies, tool
	// calls and tool results, in order. This is what the model actually sees,
	// and what makes "continue" mean something — it is saved with the session
	// and restored by --resume.
	Messages []Message `json:"messages,omitempty"`

	// limitTokens and compactPercent come from the settings, not from the
	// session, so they are deliberately not persisted: resuming a session
	// should honour the settings in force now, not the ones it was created
	// under. Zero means "use the default". See SetLimits.
	limitTokens    int
	compactPercent int
}

// TranscriptEntry is one line of the session as it was displayed.
type TranscriptEntry struct {
	Role      string            `json:"role"`
	Content   string            `json:"content,omitempty"`
	ToolName  string            `json:"tool_name,omitempty"`
	ToolArgs  map[string]string `json:"tool_args,omitempty"`
	Detail    string            `json:"detail,omitempty"`
	Diff      string            `json:"diff,omitempty"`
	IsError   bool              `json:"is_error,omitempty"`
	ElapsedMS int64             `json:"elapsed_ms,omitempty"`
	At        time.Time         `json:"at"`
}

// maxTranscriptEntries bounds the session file. It is generous: the transcript
// is plain text and a long session is exactly when the history matters most.
const maxTranscriptEntries = 4000

// AppendTranscript records a line of the session as the user saw it.
func (s *Session) AppendTranscript(e TranscriptEntry) {
	if e.At.IsZero() {
		e.At = time.Now()
	}
	s.Transcript = append(s.Transcript, e)
	if len(s.Transcript) > maxTranscriptEntries {
		s.Transcript = s.Transcript[len(s.Transcript)-maxTranscriptEntries:]
	}
}

// Conversation limits. A long session must not grow until it is refused for
// exceeding the model's context, so older turns are dropped once the history
// gets big — the oldest user message is always kept so the original request
// stays visible.
const (
	// maxSessionMessages is a backstop, not the working limit. Compaction
	// (agent_compact.go) is what normally keeps a conversation in budget, and
	// it summarises rather than deletes; this only catches a session that got
	// past it — compaction turned off, or a server that could not summarise.
	// It was 120, low enough that ordinary sessions were silently losing
	// history that compaction was about to preserve.
	maxSessionMessages = 400
	keptLeadMessages   = 2
	// pruneCeilingFactor is how far past the context window the conversation
	// may grow before pruning steps in regardless of message count. A handful
	// of large reads can exceed the window in far fewer than 400 messages.
	pruneCeilingFactor = 2
)

// AppendUser adds a user message to the conversation.
func (s *Session) AppendUser(text string) {
	s.Messages = append(s.Messages, Message{Role: "user", Content: text})
	s.pruneMessages()
}

// AppendAssistant adds the assistant's reply, keeping its content blocks
// (text and tool_use) intact so the model sees its own tool calls.
func (s *Session) AppendAssistant(content []ContentBlock) {
	if len(content) == 0 {
		return
	}
	s.Messages = append(s.Messages, Message{Role: "assistant", Content: content})
	s.pruneMessages()
}

// AppendToolResults adds the results of the assistant's tool calls. They are
// sent with the user role, which is how tool results are returned in this
// protocol.
func (s *Session) AppendToolResults(results []ContentBlock) {
	if len(results) == 0 {
		return
	}
	s.Messages = append(s.Messages, Message{Role: "user", Content: results})
	s.pruneMessages()
}

// pruneMessages trims the middle of an over-long conversation.
//
// The cut must not land between an assistant's tool_use and the tool_result
// that answers it, or the model receives a call with no reply, so the window
// starts at the first message that is not a tool result.
func (s *Session) pruneMessages() {
	if !s.needsPruning() {
		return
	}
	if len(s.Messages) <= keptLeadMessages+1 {
		return
	}

	lead := s.Messages[:keptLeadMessages]

	// Start from the message-count boundary, then keep giving ground until the
	// conversation is under the size ceiling too. Counting messages alone was
	// the old bug: 119 short messages and one 40KB read passed untouched.
	tailStart := len(s.Messages) - (maxSessionMessages - keptLeadMessages)
	if tailStart < keptLeadMessages {
		tailStart = keptLeadMessages
	}
	ceiling := s.pruneCeiling()
	for tailStart < len(s.Messages)-1 {
		// Never cut between an assistant's tool call and its result.
		for tailStart < len(s.Messages) && isToolResultMessage(s.Messages[tailStart]) {
			tailStart++
		}
		if EstimateTokens(s.Messages[tailStart:]) <= ceiling {
			break
		}
		tailStart++
	}
	for tailStart < len(s.Messages) && isToolResultMessage(s.Messages[tailStart]) {
		tailStart++
	}
	if tailStart >= len(s.Messages) {
		return
	}

	pruned := make([]Message, 0, len(s.Messages)-tailStart+keptLeadMessages+1)
	pruned = append(pruned, lead...)
	pruned = append(pruned, Message{
		Role:    "user",
		Content: "[earlier turns in this session were trimmed to fit the context window]",
	})
	pruned = append(pruned, s.Messages[tailStart:]...)
	s.Messages = pruned
}

// needsPruning reports whether the backstop has to act.
func (s *Session) needsPruning() bool {
	if len(s.Messages) > maxSessionMessages {
		return true
	}
	return EstimateTokens(s.Messages) > s.pruneCeiling()
}

// pruneCeiling is the size past which history is dropped outright.
func (s *Session) pruneCeiling() int {
	limit := s.limitTokens
	if limit <= 0 {
		limit = ContextLimit()
	}
	return limit * pruneCeilingFactor
}

// isToolResultMessage reports whether a message carries only tool results.
//
// A session loaded from disk holds []any of map[string]any rather than
// []ContentBlock, so both shapes are checked — otherwise the guard against
// cutting a tool call away from its result quietly stops working on exactly
// the sessions that have grown long enough to need it.
func isToolResultMessage(m Message) bool {
	switch blocks := m.Content.(type) {
	case []ContentBlock:
		if len(blocks) == 0 {
			return false
		}
		for _, b := range blocks {
			if b.Type != "tool_result" {
				return false
			}
		}
		return true
	case []any:
		if len(blocks) == 0 {
			return false
		}
		for _, raw := range blocks {
			m, ok := raw.(map[string]any)
			if !ok || m["type"] != "tool_result" {
				return false
			}
		}
		return true
	}
	return false
}

// SessionUsage accumulates token spend across a session.
type SessionUsage struct {
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	Requests     int     `json:"requests"`
	CostUSD      float64 `json:"cost_usd,omitempty"`
}

// Add folds one response's usage into the session total. Responses without
// usage still count as a request, so the request tally stays honest even
// against a server that does not report tokens.
func (u *SessionUsage) Add(t *TokenUsage) {
	u.Requests++
	if t == nil {
		return
	}
	u.InputTokens += t.InputTokens
	u.OutputTokens += t.OutputTokens
	u.CostUSD += t.CostUSD
}

// Summary renders the session's spend for humans, e.g.
// "18 requests · 42.1k tokens · $0.1240". Returns "" when nothing is known.
func (u SessionUsage) Summary() string {
	if u.Requests == 0 {
		return ""
	}
	parts := []string{fmt.Sprintf("%d request%s", u.Requests, plural(u.Requests))}
	if u.Reported() {
		parts = append(parts, FormatTokens(u.Total())+" tokens")
	}
	if u.CostUSD > 0 {
		parts = append(parts, fmt.Sprintf("$%.4f", u.CostUSD))
	}
	return strings.Join(parts, " · ")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// FormatTokens renders a token count compactly (1234 -> "1.2k").
func FormatTokens(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// Total returns all tokens seen in the session.
func (u SessionUsage) Total() int { return u.InputTokens + u.OutputTokens }

// Reported reports whether the server ever sent token counts.
func (u SessionUsage) Reported() bool { return u.Total() > 0 }

// TurnRecord summarises one completed request/response cycle.
type TurnRecord struct {
	At           time.Time `json:"at"`
	Prompt       string    `json:"prompt"`
	PlanSummary  string    `json:"plan_summary,omitempty"`
	Outcome      string    `json:"outcome,omitempty"`
	FilesChanged []string  `json:"files_changed,omitempty"`
}

// ClarificationQuestion represents an interactive decision required from the user.
type ClarificationQuestion struct {
	ID       string   `json:"id"`
	Question string   `json:"question"`
	Options  []string `json:"options,omitempty"`
	Default  string   `json:"default,omitempty"`
	Selected string   `json:"selected,omitempty"`
}

// PlanSummary represents the structured plan generated in Plan Mode.
type PlanSummary struct {
	Summary            string                  `json:"summary"`
	Overview           string                  `json:"overview,omitempty"`
	NeedsClarification bool                    `json:"needs_clarification,omitempty"`
	Questions          []ClarificationQuestion `json:"questions,omitempty"`
	Phases             []PlanPhase             `json:"phases,omitempty"`
	Steps              []PlanStep              `json:"steps"`
	Details            []string                `json:"details,omitempty"`
}

// PlanPhase groups architectural steps into logical stages.
type PlanPhase struct {
	Name        string   `json:"name"`        // e.g. "Phase 1: Frontend User Interface"
	Description string   `json:"description"` // e.g. "Implement responsive Todo app view"
	Files       []string `json:"files"`       // e.g. ["resources/views/todo.html"]
}

// PlanStep represents a single reviewable step in the execution plan.
type PlanStep struct {
	ID          int    `json:"id"`
	Phase       string `json:"phase,omitempty"` // e.g. "Phase 1: Frontend UI"
	Action      string `json:"action"`          // "create_file" | "edit_file" | "run_command" | "delete_file" | "clarification_needed"
	Target      string `json:"target"`
	Description string `json:"description"`
	Content     string `json:"content,omitempty"`
	Risk        string `json:"risk"` // "low" | "medium" | "high"
	Approved    bool   `json:"approved"`
	Status      string `json:"status,omitempty"` // "pending" | "running" | "applied" | "failed"
	Error       string `json:"error,omitempty"`
}

// NewSession creates an empty initialized Session.
func NewSession(model string) *Session {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)

	return &Session{
		ID:           id,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		Model:        model,
		History:      make([]Message, 0),
		AppliedSteps: make([]int, 0),
		LoadedSkills: make(map[string]string),
		Turns:        make([]TurnRecord, 0),
		Status:       "planning",
	}
}

const (
	maxMemoryTurns       = 8
	maxMemoryOutcomeChar = 700
)

// RecordTurn appends a conversation-memory entry for a finished request.
func (s *Session) RecordTurn(prompt, planSummary, outcome string, files []string) {
	if s == nil {
		return
	}
	outcome = strings.TrimSpace(outcome)
	if len(outcome) > maxMemoryOutcomeChar {
		outcome = outcome[:maxMemoryOutcomeChar] + "…"
	}
	s.Turns = append(s.Turns, TurnRecord{
		At:           time.Now(),
		Prompt:       strings.TrimSpace(prompt),
		PlanSummary:  strings.TrimSpace(planSummary),
		Outcome:      outcome,
		FilesChanged: files,
	})
}

// ConversationSummary renders recent turns for inclusion in prompts, so the
// model knows what was asked and done earlier in this session.
func (s *Session) ConversationSummary() string {
	if s == nil || len(s.Turns) == 0 {
		return ""
	}
	turns := s.Turns
	if len(turns) > maxMemoryTurns {
		turns = turns[len(turns)-maxMemoryTurns:]
	}
	var sb strings.Builder
	for i, t := range turns {
		sb.WriteString(fmt.Sprintf("%d. User asked: %s\n", i+1, t.Prompt))
		if t.PlanSummary != "" {
			sb.WriteString(fmt.Sprintf("   Plan: %s\n", t.PlanSummary))
		}
		if t.Outcome != "" {
			sb.WriteString(fmt.Sprintf("   Outcome: %s\n", t.Outcome))
		}
		if len(t.FilesChanged) > 0 {
			sb.WriteString(fmt.Sprintf("   Files changed: %s\n", strings.Join(t.FilesChanged, ", ")))
		}
	}
	return strings.TrimSpace(sb.String())
}

// sessionIDWidth is the column an id occupies in a listing. Ids are hex and
// fixed-length, so the header and the rows agree on one number rather than
// drifting apart the first time the id format changes.
const sessionIDWidth = 12

// SessionListHeader is the column header matching Describe.
func SessionListHeader() string {
	return fmt.Sprintf("%-*s  %-9s  %-60s  %s", sessionIDWidth, "ID", "UPDATED", "PROMPT", "WORK")
}

// Describe renders a session as one line for a picker or a listing: what was
// asked, how long ago, and how much work is in it.
//
// The prompt is what identifies a session to the person who ran it — an id is
// only useful once you already know which one you want.
func (s *Session) Describe(now time.Time) string {
	title := strings.TrimSpace(s.InitialQuery)
	if title == "" {
		title = "(no prompt)"
	}
	title = strings.Join(strings.Fields(title), " ")
	if len(title) > 60 {
		title = title[:57] + "…"
	}

	detail := fmt.Sprintf("%d turn", len(s.Turns))
	if len(s.Turns) != 1 {
		detail += "s"
	}
	if n := s.Usage.Total(); n > 0 {
		detail += ", " + FormatTokens(n) + " tokens"
	}

	return fmt.Sprintf("%-*s  %-9s  %-60s  %s",
		sessionIDWidth, s.ID, humaniseSince(now.Sub(s.UpdatedAt)), title, detail)
}

// humaniseSince renders an age the way it gets spoken.
func humaniseSince(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// LatestSession returns the most recently updated session, or nil when the
// project has none.
func LatestSession(appRoot string) (*Session, error) {
	sessions, err := ListSessions(appRoot)
	if err != nil || len(sessions) == 0 {
		return nil, err
	}
	return sessions[0], nil
}

// SaveSession persists the session JSON under .nimbus/ai-sessions/<id>.json.
func SaveSession(appRoot string, session *Session) error {
	session.UpdatedAt = time.Now()
	dir := filepath.Join(appRoot, ".nimbus", "ai-sessions")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}

	filePath := filepath.Join(dir, fmt.Sprintf("%s.json", session.ID))
	return os.WriteFile(filePath, data, 0644)
}

// LoadSession reads an existing session from disk.
func LoadSession(appRoot, sessionID string) (*Session, error) {
	filePath := filepath.Join(appRoot, ".nimbus", "ai-sessions", fmt.Sprintf("%s.json", sessionID))
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("session '%s' not found: %w", sessionID, err)
	}

	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("failed to decode session '%s': %w", sessionID, err)
	}
	if session.LoadedSkills == nil {
		session.LoadedSkills = make(map[string]string)
	}
	return &session, nil
}

// ListSessions returns a list of recent sessions sorted newest first.
func ListSessions(appRoot string) ([]*Session, error) {
	dir := filepath.Join(appRoot, ".nimbus", "ai-sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var sessions []*Session
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			var s Session
			if err := json.Unmarshal(data, &s); err == nil {
				sessions = append(sessions, &s)
			}
		}
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})

	return sessions, nil
}
