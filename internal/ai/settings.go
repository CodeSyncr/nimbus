package ai

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

/*
Settings.

Everything configurable about `nimbus ai` used to live in flags and
environment variables: fine for a one-off run, useless for a preference. There
was nowhere to say "always verify builds in this project" or "never auto
compact" and have it stick, and no way to see what the current answers were.

This is the store behind /settings. Three files layer on top of each other,
each overriding the one before:

	~/.nimbus/settings.json              the user, across every project
	<app>/.nimbus/settings.json          the project, usually committed
	<app>/.nimbus/settings.local.json    this checkout, usually ignored

Environment variables override all three. They are the escape hatch for CI and
for one-off runs, and a run that sets NIMBUS_CONTEXT_LIMIT should not have to
edit a file to do it.

Layering is done over raw JSON rather than over decoded structs. A decoded
struct cannot tell "absent" from "false", so a project file would silently
reset every bool the user had set; merging the raw keys means a file overrides
exactly what it mentions. It also preserves keys this version does not know
about, so a settings file written by a newer build survives a round-trip
through an older one.
*/

// SettingsScope is one layer of the settings stack.
type SettingsScope string

const (
	// ScopeUser applies to every project, from ~/.nimbus/settings.json.
	ScopeUser SettingsScope = "user"
	// ScopeProject is committed with the repo.
	ScopeProject SettingsScope = "project"
	// ScopeLocal is this checkout only, and belongs in .gitignore.
	ScopeLocal SettingsScope = "local"
	// ScopeEnv is an environment variable. It cannot be written.
	ScopeEnv SettingsScope = "env"
	// ScopeDefault means nothing has set it.
	ScopeDefault SettingsScope = "default"
)

// scopeOrder is the precedence, weakest first.
var scopeOrder = []SettingsScope{ScopeUser, ScopeProject, ScopeLocal}

// Settings is the resolved configuration for a run.
//
// Every field is also described in the registry below, which is what /settings
// renders; a field added here without an entry there is invisible to the user.
type Settings struct {
	// Model is the default model name, or "optimal" to let the server pick.
	Model string `json:"model"`
	// AutoCompact summarises the conversation automatically as it fills.
	AutoCompact bool `json:"auto_compact"`
	// ContextLimit is the assumed context window in tokens.
	ContextLimit int `json:"context_limit"`
	// CompactThresholdPercent is how full the window gets before compaction.
	CompactThresholdPercent int `json:"compact_threshold_percent"`
	// PermissionMode decides how much runs without being confirmed.
	PermissionMode string `json:"permission_mode"`
	// VerifyBuilds runs the project's build after a turn changes code, and
	// hands failures back to the model to repair.
	VerifyBuilds bool `json:"verify_builds"`
	// ExpandDiffs shows full diffs in the transcript rather than a summary.
	ExpandDiffs bool `json:"expand_diffs"`
	// ShowThinking shows the "Thought for Xs" line between turns.
	ShowThinking bool `json:"show_thinking"`
	// StreamOutput prints the reply as it arrives rather than when complete.
	StreamOutput bool `json:"stream_output"`
	// MaxCommandOutput caps the bytes of a command's output fed back.
	MaxCommandOutput int `json:"max_command_output"`
	// PlanFirst routes every request through plan → approve → execute.
	PlanFirst bool `json:"plan_first"`

	// MCPServers is carried through untouched for the MCP client to read.
	// It is declared here so the raw-JSON round trip keeps it, and is not in
	// the registry: a server list is edited as JSON, not toggled in a list.
	MCPServers map[string]json.RawMessage `json:"mcp_servers,omitempty"`
}

// DefaultSettings is the configuration before any file or variable is read.
func DefaultSettings() Settings {
	return Settings{
		Model:                   "optimal",
		AutoCompact:             true,
		ContextLimit:            defaultContextLimit,
		CompactThresholdPercent: 80,
		PermissionMode:          string(PermissionAuto),
		VerifyBuilds:            true,
		ExpandDiffs:             false,
		ShowThinking:            true,
		StreamOutput:            true,
		MaxCommandOutput:        maxCommandOutput,
		PlanFirst:               false,
	}
}

// SettingKind is how a value is presented and edited.
type SettingKind int

const (
	// SettingBool toggles between true and false.
	SettingBool SettingKind = iota
	// SettingChoice cycles through a fixed list.
	SettingChoice
	// SettingInt steps between a minimum and a maximum.
	SettingInt
)

// SettingDef describes one setting for the /settings screen.
//
// The screen is generated from this list rather than written out row by row,
// so a setting added here appears in the list, in the search, and in the
// round-trip to disk without touching the view — the same reason the slash
// commands live in one registry.
type SettingDef struct {
	Key     string // the JSON key, and the name used by search
	Label   string // shown in the list
	Help    string // one line, shown for the highlighted row
	Kind    SettingKind
	Choices []string // SettingChoice
	Min     int      // SettingInt
	Max     int      // SettingInt
	Step    int      // SettingInt
	Env     string   // the variable that overrides it, if any

	boolField func(*Settings) *bool
	intField  func(*Settings) *int
	strField  func(*Settings) *string
}

// SettingDefs is the catalogue, in the order the screen shows it.
var SettingDefs = []SettingDef{
	{
		Key: "model", Label: "Model", Kind: SettingChoice,
		Help:     "Which model answers. 'optimal' lets the server choose per request.",
		Choices:  []string{"optimal", "fast", "balanced", "deep"},
		strField: func(s *Settings) *string { return &s.Model },
	},
	{
		Key: "auto_compact", Label: "Auto-compact", Kind: SettingBool,
		Help:      "Summarise earlier turns automatically as the window fills, instead of failing the turn.",
		boolField: func(s *Settings) *bool { return &s.AutoCompact },
	},
	{
		Key: "context_limit", Label: "Context limit", Kind: SettingInt,
		Help: "Assumed context window, in tokens. Too high fails requests; too low compacts early.",
		Min:  16000, Max: 1000000, Step: 16000, Env: contextLimitEnv,
		intField: func(s *Settings) *int { return &s.ContextLimit },
	},
	{
		Key: "compact_threshold_percent", Label: "Compact at", Kind: SettingInt,
		Help: "How full the window gets before compaction runs, as a percentage.",
		Min:  50, Max: 95, Step: 5,
		intField: func(s *Settings) *int { return &s.CompactThresholdPercent },
	},
	{
		Key: "permission_mode", Label: "Permission mode", Kind: SettingChoice,
		Help:     "auto asks about consequential actions · ask confirms every change · allow runs anything not refused.",
		Choices:  []string{string(PermissionAuto), string(PermissionAsk), string(PermissionAllow)},
		strField: func(s *Settings) *string { return &s.PermissionMode },
	},
	{
		Key: "verify_builds", Label: "Verify builds", Kind: SettingBool,
		Help:      "Build the project after a turn changes code, and hand failures back to be repaired.",
		boolField: func(s *Settings) *bool { return &s.VerifyBuilds },
	},
	{
		Key: "plan_first", Label: "Plan before executing", Kind: SettingBool,
		Help:      "Route every request through plan → approve → execute, rather than answering directly.",
		boolField: func(s *Settings) *bool { return &s.PlanFirst },
	},
	{
		Key: "expand_diffs", Label: "Expand diffs", Kind: SettingBool,
		Help:      "Show full diffs in the transcript instead of a change summary. Ctrl+O toggles it per session.",
		boolField: func(s *Settings) *bool { return &s.ExpandDiffs },
	},
	{
		Key: "show_thinking", Label: "Show thinking", Kind: SettingBool,
		Help:      "Report how long each stretch of thinking took.",
		boolField: func(s *Settings) *bool { return &s.ShowThinking },
	},
	{
		Key: "stream_output", Label: "Stream output", Kind: SettingBool,
		Help:      "Print the reply as it arrives rather than when it is complete.",
		boolField: func(s *Settings) *bool { return &s.StreamOutput },
	},
	{
		Key: "max_command_output", Label: "Max command output", Kind: SettingInt,
		Help: "Bytes of a command's output fed back to the model before it is truncated.",
		Min:  4096, Max: 262144, Step: 4096,
		intField: func(s *Settings) *int { return &s.MaxCommandOutput },
	},
}

// FindSetting returns the definition for a key.
func FindSetting(key string) (SettingDef, bool) {
	for _, d := range SettingDefs {
		if d.Key == key {
			return d, true
		}
	}
	return SettingDef{}, false
}

// Value reads this setting out of a resolved Settings.
func (d SettingDef) Value(s *Settings) any {
	switch {
	case d.boolField != nil:
		return *d.boolField(s)
	case d.intField != nil:
		return *d.intField(s)
	case d.strField != nil:
		return *d.strField(s)
	}
	return nil
}

// Display renders the value the way the screen shows it.
func (d SettingDef) Display(s *Settings) string {
	switch v := d.Value(s).(type) {
	case bool:
		return strconv.FormatBool(v)
	case int:
		if d.Kind == SettingInt && d.Max >= 100000 {
			return FormatTokens(v)
		}
		return strconv.Itoa(v)
	case string:
		return v
	}
	return ""
}

// Next advances the value: bools flip, choices cycle, numbers step. Negative
// delta goes the other way, which is what ←/→ do on a number.
//
// Cycling rather than opening an editor is deliberate — every setting here has
// few enough sensible values that a list is faster than typing one, and there
// is no invalid state to validate.
func (d SettingDef) Next(s *Settings, delta int) {
	switch {
	case d.boolField != nil:
		p := d.boolField(s)
		*p = !*p

	case d.strField != nil:
		p := d.strField(s)
		if len(d.Choices) == 0 {
			return
		}
		idx := 0
		for i, c := range d.Choices {
			if c == *p {
				idx = i
				break
			}
		}
		idx = (idx + delta + len(d.Choices)) % len(d.Choices)
		*p = d.Choices[idx]

	case d.intField != nil:
		p := d.intField(s)
		step := d.Step
		if step <= 0 {
			step = 1
		}
		v := *p + step*delta
		// Wrapping keeps a single key usable: Enter on a maximum returns to
		// the minimum instead of doing nothing at all.
		if v > d.Max {
			v = d.Min
		}
		if v < d.Min {
			v = d.Max
		}
		*p = v
	}
}

// ---------------------------------------------------------------------------
// Files
// ---------------------------------------------------------------------------

// settingsFileName is the per-scope file, relative to its directory.
func settingsPath(appRoot string, scope SettingsScope) (string, error) {
	switch scope {
	case ScopeUser:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot locate the home directory: %w", err)
		}
		return filepath.Join(home, ".nimbus", "settings.json"), nil
	case ScopeProject:
		if appRoot == "" {
			return "", fmt.Errorf("no project directory for project settings")
		}
		return filepath.Join(appRoot, ".nimbus", "settings.json"), nil
	case ScopeLocal:
		if appRoot == "" {
			return "", fmt.Errorf("no project directory for local settings")
		}
		return filepath.Join(appRoot, ".nimbus", "settings.local.json"), nil
	}
	return "", fmt.Errorf("%q is not a writable settings scope", scope)
}

// readLayer loads one file as raw keys. A missing file is not an error: most
// projects have none, and that is the normal case rather than a problem.
func readLayer(path string) map[string]json.RawMessage {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	return raw
}

// LoadSettings resolves the settings for a project.
//
// Nothing here fails: a malformed or unreadable file is skipped rather than
// stopping the CLI from starting, because the alternative is a typo in a
// settings file making the tool unusable.
func LoadSettings(appRoot string) Settings {
	merged := map[string]json.RawMessage{}
	for _, scope := range scopeOrder {
		path, err := settingsPath(appRoot, scope)
		if err != nil {
			continue
		}
		for k, v := range readLayer(path) {
			merged[k] = v
		}
	}

	settings := DefaultSettings()
	if len(merged) > 0 {
		if data, err := json.Marshal(merged); err == nil {
			// Unmarshalling over a populated struct leaves absent keys at
			// their default, which is exactly the layering we want.
			_ = json.Unmarshal(data, &settings)
		}
	}
	applyEnvOverrides(&settings)
	return settings
}

// applyEnvOverrides lets the environment win over every file.
func applyEnvOverrides(s *Settings) {
	if v := strings.TrimSpace(os.Getenv(contextLimitEnv)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			s.ContextLimit = n
		}
	}
}

// SettingSource reports which layer decides a key's value, so the screen can
// explain why a setting is not what the user thought they set.
//
// This is the question a flat list cannot answer: "I set that to false, why is
// it true?" is almost always a project file overriding a user file.
func SettingSource(appRoot, key string) SettingsScope {
	if def, ok := FindSetting(key); ok && def.Env != "" {
		if strings.TrimSpace(os.Getenv(def.Env)) != "" {
			return ScopeEnv
		}
	}
	source := ScopeDefault
	for _, scope := range scopeOrder {
		path, err := settingsPath(appRoot, scope)
		if err != nil {
			continue
		}
		if _, ok := readLayer(path)[key]; ok {
			source = scope
		}
	}
	return source
}

// SaveSetting writes one key to one scope, leaving the rest of the file alone.
//
// Rewriting the whole file from a resolved Settings would be wrong twice over:
// it would bake every inherited value into the layer as if it had been set
// there, and it would drop any key this build does not know about.
func SaveSetting(appRoot string, scope SettingsScope, key string, value any) error {
	path, err := settingsPath(appRoot, scope)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("cannot create %s: %w", filepath.Dir(path), err)
	}

	raw := readLayer(path)
	if raw == nil {
		raw = map[string]json.RawMessage{}
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("cannot encode %s: %w", key, err)
	}
	raw[key] = encoded

	data, err := marshalSorted(raw)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	return nil
}

// marshalSorted renders the file with stable key order, so a settings file
// under version control produces a readable diff rather than a reshuffle.
func marshalSorted(raw map[string]json.RawMessage) ([]byte, error) {
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	sb.WriteString("{\n")
	for i, k := range keys {
		key, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		sb.WriteString("  ")
		sb.Write(key)
		sb.WriteString(": ")
		sb.Write(raw[k])
		if i < len(keys)-1 {
			sb.WriteString(",")
		}
		sb.WriteString("\n")
	}
	sb.WriteString("}\n")
	return []byte(sb.String()), nil
}

// SettingsFileList reports where each scope's file lives and whether it
// exists, for the footer of the settings screen.
func SettingsFileList(appRoot string) []string {
	var out []string
	for _, scope := range scopeOrder {
		path, err := settingsPath(appRoot, scope)
		if err != nil {
			continue
		}
		mark := "—"
		if _, err := os.Stat(path); err == nil {
			mark = "✓"
		}
		out = append(out, fmt.Sprintf("%s %s  %s", mark, scope, path))
	}
	return out
}
