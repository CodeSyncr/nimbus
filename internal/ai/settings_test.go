package ai

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// writeSettings puts a raw settings file in place for a scope.
func writeSettings(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A project file overrides the user file, and a local file overrides both.
func TestSettingsLayerInPrecedenceOrder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	app := t.TempDir()

	writeSettings(t, filepath.Join(home, ".nimbus", "settings.json"),
		`{"model":"deep","verify_builds":false,"show_thinking":false}`)
	writeSettings(t, filepath.Join(app, ".nimbus", "settings.json"),
		`{"model":"balanced","auto_compact":false}`)
	writeSettings(t, filepath.Join(app, ".nimbus", "settings.local.json"),
		`{"model":"fast"}`)

	s := LoadSettings(app)

	if s.Model != "fast" {
		t.Errorf("model = %q, want the local file's value", s.Model)
	}
	if s.AutoCompact {
		t.Error("the project file's auto_compact=false was lost")
	}
	// The one that matters most: a project file that says nothing about
	// verify_builds must not reset the user's answer to the zero value.
	if s.VerifyBuilds {
		t.Error("the user's verify_builds=false was overwritten by a file that never mentioned it")
	}
	if s.ShowThinking {
		t.Error("the user's show_thinking=false was overwritten")
	}
	// Untouched by every layer.
	if s.CompactThresholdPercent != DefaultSettings().CompactThresholdPercent {
		t.Errorf("compact threshold = %d, want the default", s.CompactThresholdPercent)
	}
}

// The environment is the escape hatch, so it beats every file.
func TestEnvironmentOverridesEveryFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	app := t.TempDir()
	writeSettings(t, filepath.Join(app, ".nimbus", "settings.json"), `{"context_limit":64000}`)

	if got := LoadSettings(app).ContextLimit; got != 64000 {
		t.Fatalf("context limit = %d, want the file's 64000", got)
	}

	t.Setenv(contextLimitEnv, "200000")
	if got := LoadSettings(app).ContextLimit; got != 200000 {
		t.Errorf("context limit = %d, want the environment's 200000", got)
	}
	if src := SettingSource(app, "context_limit"); src != ScopeEnv {
		t.Errorf("source = %q, want env", src)
	}
}

// A write must touch one key in one file, leaving the rest — including keys
// this build has never heard of — exactly as they were.
func TestSaveSettingPreservesTheRestOfTheFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	app := t.TempDir()

	path := filepath.Join(app, ".nimbus", "settings.json")
	writeSettings(t, path, `{"model":"deep","mcp_servers":{"aws":{"command":"uvx"}},"future_key":42}`)

	if err := SaveSetting(app, ScopeProject, "auto_compact", false); err != nil {
		t.Fatalf("SaveSetting: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("the file is no longer valid JSON: %v\n%s", err, data)
	}
	for _, key := range []string{"model", "mcp_servers", "future_key", "auto_compact"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("%q was lost by the write", key)
		}
	}
	if string(raw["auto_compact"]) != "false" {
		t.Errorf("auto_compact = %s, want false", raw["auto_compact"])
	}
	if string(raw["future_key"]) != "42" {
		t.Errorf("an unknown key was rewritten: %s", raw["future_key"])
	}
}

// Writing to the user scope while the project overrides the same key must not
// look like it took effect.
func TestSettingSourceNamesTheDecidingLayer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	app := t.TempDir()

	if src := SettingSource(app, "model"); src != ScopeDefault {
		t.Errorf("untouched setting reports %q, want default", src)
	}

	if err := SaveSetting(app, ScopeUser, "model", "deep"); err != nil {
		t.Fatal(err)
	}
	if src := SettingSource(app, "model"); src != ScopeUser {
		t.Errorf("source = %q, want user", src)
	}

	if err := SaveSetting(app, ScopeProject, "model", "fast"); err != nil {
		t.Fatal(err)
	}
	if src := SettingSource(app, "model"); src != ScopeProject {
		t.Errorf("source = %q, want project", src)
	}
	if got := LoadSettings(app).Model; got != "fast" {
		t.Errorf("model = %q, want the project's value", got)
	}
}

// Every setting in the registry must round-trip: change it, save it, read it
// back. A setting that cannot be persisted is a switch that does nothing.
func TestEverySettingRoundTrips(t *testing.T) {
	for _, def := range SettingDefs {
		t.Run(def.Key, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			// The context-limit row is overridden by the environment in the
			// parent process only; clear it so the round trip is honest.
			t.Setenv(contextLimitEnv, "")
			app := t.TempDir()

			before := LoadSettings(app)
			next := before
			def.Next(&next, 1)

			want := def.Value(&next)
			if want == def.Value(&before) {
				t.Fatalf("Next did not change %s", def.Key)
			}
			if err := SaveSetting(app, ScopeUser, def.Key, want); err != nil {
				t.Fatalf("SaveSetting: %v", err)
			}
			reloaded := LoadSettings(app)
			if got := def.Value(&reloaded); got != want {
				t.Errorf("%s round-tripped to %v, want %v", def.Key, got, want)
			}
		})
	}
}

// Numbers wrap at their bounds so a single key stays usable, and choices cycle.
func TestNextWrapsAtTheBounds(t *testing.T) {
	limit, ok := FindSetting("compact_threshold_percent")
	if !ok {
		t.Fatal("compact_threshold_percent is not in the registry")
	}
	s := DefaultSettings()
	s.CompactThresholdPercent = limit.Max
	limit.Next(&s, 1)
	if s.CompactThresholdPercent != limit.Min {
		t.Errorf("stepping past the maximum gave %d, want %d", s.CompactThresholdPercent, limit.Min)
	}
	limit.Next(&s, -1)
	if s.CompactThresholdPercent != limit.Max {
		t.Errorf("stepping below the minimum gave %d, want %d", s.CompactThresholdPercent, limit.Max)
	}

	mode, _ := FindSetting("permission_mode")
	s.PermissionMode = string(PermissionAllow)
	mode.Next(&s, 1)
	if s.PermissionMode != string(PermissionAuto) {
		t.Errorf("choices did not cycle: %q", s.PermissionMode)
	}
}

// A settings file full of nonsense must not stop the CLI from starting.
func TestBrokenSettingsFileFallsBackToDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(contextLimitEnv, "")
	app := t.TempDir()
	writeSettings(t, filepath.Join(app, ".nimbus", "settings.json"), "{ this is not json")

	if got := LoadSettings(app); !reflect.DeepEqual(got, DefaultSettings()) {
		t.Errorf("a broken file changed the settings: %+v", got)
	}
}

// The settings the screen shows have to reach the code that acts on them.
func TestApplySettingsReachesTheAgent(t *testing.T) {
	dir := t.TempDir()
	agent := NewAgent(&mockAIClient{}, NewToolExecutor(dir), &ProjectContext{AppRoot: dir}, NewSession("optimal"))

	s := DefaultSettings()
	s.ContextLimit = 40000
	s.CompactThresholdPercent = 60
	s.PermissionMode = string(PermissionAsk)
	s.MaxCommandOutput = 4096
	s.VerifyBuilds = false
	agent.ApplySettings(s)

	usage := agent.Session.ContextUsage()
	if usage.Limit != 40000 {
		t.Errorf("context limit = %d, want 40000", usage.Limit)
	}
	if usage.Threshold() != 60 {
		t.Errorf("threshold = %d, want 60", usage.Threshold())
	}
	if agent.Tools.MaxCommandOutput != 4096 {
		t.Errorf("command output cap = %d, want 4096", agent.Tools.MaxCommandOutput)
	}
	if agent.Verifier != nil {
		t.Error("verify_builds=false left the verifier installed")
	}

	// Turning it back on restores it, rather than leaving the run unverified
	// for the rest of the session.
	s.VerifyBuilds = true
	agent.ApplySettings(s)
	if agent.Verifier == nil {
		t.Error("verify_builds=true did not restore the verifier")
	}
}

// Auto-compact off means off, even when the window is full.
func TestAutoCompactOffSuppressesCompaction(t *testing.T) {
	dir := t.TempDir()
	client := &scriptedTurnClient{}
	agent := NewAgent(client, NewToolExecutor(dir), &ProjectContext{AppRoot: dir}, NewSession("optimal"))

	s := DefaultSettings()
	s.AutoCompact = false
	s.ContextLimit = 1000 // anything at all is over the threshold
	agent.ApplySettings(s)
	agent.Session.Messages = longConversation(30)

	if !agent.Session.ContextUsage().NeedsCompaction() {
		t.Fatal("the fixture is not over the threshold")
	}
	agent.maybeCompact(context.Background())

	if client.calls[TurnModeChat] != 0 {
		t.Error("compaction ran with auto_compact off")
	}
}
