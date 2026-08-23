package ai

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
		Status:       "planning",
	}
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
