package commands_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CodeSyncr/nimbus/cli"
	"github.com/CodeSyncr/nimbus/cli/auth"
	"github.com/CodeSyncr/nimbus/cli/commands"
)

// agentServer is a mock Nimbus Cloud that speaks the conversational "agent"
// turn mode and records what each turn was shown.
type agentServer struct {
	mu         sync.Mutex
	turns      int
	transcript []string // the messages payload of every turn, in order
	modes      []string
}

func (s *agentServer) snapshot() ([]string, []string, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.transcript...), append([]string(nil), s.modes...), s.turns
}

func newAgentServer(t *testing.T, reply func(turn int, messages string) []map[string]any) (*httptest.Server, *agentServer) {
	t.Helper()
	state := &agentServer{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ai/turn" {
			http.NotFound(w, r)
			return
		}
		var req struct {
			Mode     string           `json:"mode"`
			Messages []map[string]any `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		raw, _ := json.Marshal(req.Messages)

		state.mu.Lock()
		state.turns++
		turn := state.turns
		state.transcript = append(state.transcript, string(raw))
		state.modes = append(state.modes, req.Mode)
		state.mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		for _, ev := range reply(turn, string(raw)) {
			b, _ := json.Marshal(ev)
			fmt.Fprintf(w, "data: %s\n\n", b)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	return srv, state
}

func textEvent(s string) map[string]any { return map[string]any{"type": "text", "text": s} }

func toolEvent(id, name string, input map[string]any) map[string]any {
	return map[string]any{"type": "tool_use", "id": id, "name": name, "input": input}
}

func authAgainst(t *testing.T, dir, serverURL string) {
	t.Helper()
	t.Setenv("HOME", dir)
	t.Setenv(auth.ConfigDirEnv, filepath.Join(dir, ".nimbus"))
	t.Setenv("NIMBUS_CLOUD_URL", serverURL)
	_ = auth.SaveCredentials(&auth.Credentials{
		AccessToken: "mock-token", Email: "pro@nimbusgo.in", Plan: "pro", HasSub: true,
		ServerURL: serverURL, ExpiresAt: time.Now().Add(24 * time.Hour),
	})
}

// A change request runs in one conversation: no explore/plan/execute modes, no
// plan JSON, no approval gate — just tools and an answer.
func TestAIAgentModeMakesChangesWithoutAPlanGate(t *testing.T) {
	tmpDir := t.TempDir()
	var stdout, stderr bytes.Buffer

	srv, state := newAgentServer(t, func(turn int, messages string) []map[string]any {
		if turn == 1 {
			return []map[string]any{
				textEvent("Adding the greeter."),
				toolEvent("t1", "write_file", map[string]any{
					"path": "greet.go", "content": "package main\n\nfunc Greet() string { return \"hi\" }\n",
				}),
			}
		}
		return []map[string]any{textEvent("Created greet.go with a Greet function.")}
	})
	defer srv.Close()
	authAgainst(t, tmpDir, srv.URL)

	ctx := &cli.Context{AppRoot: tmpDir, Args: []string{"add", "a", "greet", "function"}, Stdout: &stdout, Stderr: &stderr}
	if err := (&commands.AICommand{}).Run(ctx); err != nil {
		t.Fatalf("Run failed: %v\n%s", err, stdout.String())
	}

	data, err := os.ReadFile(filepath.Join(tmpDir, "greet.go"))
	if err != nil || !bytes.Contains(data, []byte("func Greet")) {
		t.Fatalf("greet.go not written (err=%v)\n%s", err, stdout.String())
	}

	transcript, modes, turns := state.snapshot()
	for _, m := range modes {
		if m != "agent" {
			t.Errorf("turn used mode %q; the staged pipeline should not run", m)
		}
	}
	if turns != 2 {
		t.Errorf("expected 2 turns (act, then summarise), got %d", turns)
	}
	if !strings.Contains(transcript[1], "tool_result") {
		t.Error("the write_file result was not fed back into the conversation")
	}
	if !strings.Contains(stdout.String(), "Created greet.go") {
		t.Errorf("final answer not printed:\n%s", stdout.String())
	}
}

// A question is answered directly: no files touched, no plan, one turn.
func TestAIAgentModeAnswersQuestionsWithoutTouchingFiles(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer

	srv, state := newAgentServer(t, func(turn int, messages string) []map[string]any {
		return []map[string]any{textEvent("It is a small Go program with a single main package.")}
	})
	defer srv.Close()
	authAgainst(t, tmpDir, srv.URL)

	ctx := &cli.Context{AppRoot: tmpDir, Args: []string{"what", "does", "this", "project", "do?"}, Stdout: &stdout, Stderr: &stderr}
	if err := (&commands.AICommand{}).Run(ctx); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if _, _, turns := state.snapshot(); turns != 1 {
		t.Errorf("a question took %d turns, want 1", turns)
	}
	if !strings.Contains(stdout.String(), "single main package") {
		t.Errorf("answer not printed:\n%s", stdout.String())
	}
	entries, _ := os.ReadDir(tmpDir)
	for _, e := range entries {
		if e.Name() != "main.go" && e.Name() != ".nimbus" {
			t.Errorf("a question created %q", e.Name())
		}
	}
}

// The workspace scan reaches the server, so the model can tell whether a
// request relates to the project in front of it.
func TestAIAgentModeSendsWorkspaceContext(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/shop\n\ngo 1.24\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("# Shop\n"), 0644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer

	var sawContext bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ProjectContext map[string]any `json:"project_context"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.ProjectContext != nil {
			blob, _ := json.Marshal(req.ProjectContext)
			if strings.Contains(string(blob), "example.com/shop") && strings.Contains(string(blob), "README.md") {
				sawContext = true
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"text\",\"text\":\"Answered.\"}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	authAgainst(t, tmpDir, srv.URL)

	ctx := &cli.Context{AppRoot: tmpDir, Args: []string{"what", "is", "this"}, Stdout: &stdout, Stderr: &stderr}
	if err := (&commands.AICommand{}).Run(ctx); err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !sawContext {
		t.Error("the workspace scan (module name, root files) never reached the server")
	}
}

// The agent asks Nimbus Cloud for a picture and the bytes land in the project.
// The CLI holds no provider keys, so this whole path has to work through the
// cloud endpoint.
func TestAIAgentGeneratesImagesThroughTheCloud(t *testing.T) {
	tmpDir := t.TempDir()
	var stdout, stderr bytes.Buffer

	var imagePrompt string
	var sawImageTool bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/ai/image":
			var req struct {
				Prompt string `json:"prompt"`
				Size   string `json:"size"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			imagePrompt = req.Prompt
			// "hero" base64-encoded, standing in for image bytes.
			fmt.Fprint(w, `{"success":true,"model":"imagen-3.0-generate-002","images":[{"b64_json":"aGVybw=="}]}`)

		case "/api/v1/ai/turn":
			var req struct {
				Tools []map[string]any `json:"tools"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			for _, tool := range req.Tools {
				if tool["name"] == "generate_image" {
					sawImageTool = true
				}
			}
			w.Header().Set("Content-Type", "text/event-stream")
			if !sawImageTool {
				fmt.Fprint(w, "data: {\"type\":\"text\",\"text\":\"no image tool offered\"}\n\n")
				fmt.Fprint(w, "data: [DONE]\n\n")
				return
			}
			if imagePrompt == "" {
				b, _ := json.Marshal(toolEvent("t1", "generate_image", map[string]any{
					"prompt": "a neon skyline reflected in wet asphalt",
					"path":   "public/images/hero.png",
					"size":   "1792x1024",
				}))
				fmt.Fprintf(w, "data: %s\n\n", b)
				fmt.Fprint(w, "data: [DONE]\n\n")
				return
			}
			fmt.Fprint(w, "data: {\"type\":\"text\",\"text\":\"Added public/images/hero.png to the page.\"}\n\n")
			fmt.Fprint(w, "data: [DONE]\n\n")

		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	authAgainst(t, tmpDir, srv.URL)

	ctx := &cli.Context{AppRoot: tmpDir, Args: []string{"add", "a", "hero", "image"}, Stdout: &stdout, Stderr: &stderr}
	if err := (&commands.AICommand{}).Run(ctx); err != nil {
		t.Fatalf("Run failed: %v\n%s", err, stdout.String())
	}

	if !sawImageTool {
		t.Fatal("generate_image was never offered to the model")
	}
	data, err := os.ReadFile(filepath.Join(tmpDir, "public/images/hero.png"))
	if err != nil {
		t.Fatalf("the image was not written: %v\n%s", err, stdout.String())
	}
	if string(data) != "hero" {
		t.Errorf("decoded bytes = %q, want the decoded base64 payload", data)
	}
	if imagePrompt != "a neon skyline reflected in wet asphalt" {
		t.Errorf("prompt did not reach the cloud endpoint: %q", imagePrompt)
	}
}
