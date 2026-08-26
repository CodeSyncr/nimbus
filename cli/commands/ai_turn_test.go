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

// TestAICommand_TurnProtocol drives the full explore → plan → execute loop
// against a mock /api/v1/ai/turn server that streams SSE, checking that tool
// results flow back to the "model" intact and that files land on disk.
func TestAICommand_TurnProtocol(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer

	var mu sync.Mutex
	calls := map[string]int{}
	var sawFileContent, sawFindingsInPlan, sawPlanInExecute bool

	sse := func(w http.ResponseWriter, events ...map[string]any) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, ev := range events {
			b, _ := json.Marshal(ev)
			fmt.Fprintf(w, "data: %s\n\n", b)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}
	textEv := func(s string) map[string]any { return map[string]any{"type": "text", "text": s} }
	toolEv := func(id, name string, input map[string]any) map[string]any {
		return map[string]any{"type": "tool_use", "id": id, "name": name, "input": input}
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ai/turn" {
			http.NotFound(w, r)
			return
		}
		var req struct {
			Mode     string           `json:"mode"`
			Messages []map[string]any `json:"messages"`
			Tools    []map[string]any `json:"tools"`
			Plan     map[string]any   `json:"plan"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		raw, _ := json.Marshal(req.Messages)
		mu.Lock()
		calls[req.Mode]++
		n := calls[req.Mode]
		mu.Unlock()

		switch req.Mode {
		case "explore":
			if n == 1 {
				sse(w, toolEv("t1", "read_file", map[string]any{"path": "main.go"}))
				return
			}
			if strings.Contains(string(raw), "func main() {}") {
				mu.Lock()
				sawFileContent = true
				mu.Unlock()
			}
			sse(w, textEv("FINDINGS: main.go is empty; add greet.go"))
		case "plan":
			if strings.Contains(string(raw), "FINDINGS: main.go is empty") {
				mu.Lock()
				sawFindingsInPlan = true
				mu.Unlock()
			}
			sse(w, textEv(`{"summary":"Add greet","steps":[{"id":1,"action":"create_file","target":"greet.go","description":"Greet","risk":"low"}]}`))
		case "execute":
			if req.Plan != nil && req.Plan["summary"] == "Add greet" {
				mu.Lock()
				sawPlanInExecute = true
				mu.Unlock()
			}
			if n == 1 {
				sse(w, textEv("Writing greet.go"), toolEv("t2", "write_file", map[string]any{"path": "greet.go", "content": "package main\n\nfunc Greet() string { return \"hi\" }\n"}))
				return
			}
			sse(w, textEv("Done: created greet.go"))
		default:
			http.Error(w, "bad mode "+req.Mode, http.StatusBadRequest)
		}
	}))
	defer server.Close()

	t.Setenv("HOME", tmpDir)
	t.Setenv(auth.ConfigDirEnv, filepath.Join(tmpDir, ".nimbus"))
	t.Setenv("NIMBUS_CLOUD_URL", server.URL)
	_ = auth.SaveCredentials(&auth.Credentials{
		AccessToken: "mock-token", Email: "pro@nimbusgo.in", Plan: "pro", HasSub: true,
		ServerURL: server.URL, ExpiresAt: time.Now().Add(24 * time.Hour),
	})

	ctx := &cli.Context{AppRoot: tmpDir, Args: []string{"add", "a", "greet", "function"}, Stdout: &stdout, Stderr: &stderr}
	if err := (&commands.AICommand{}).Run(ctx); err != nil {
		t.Fatalf("Run failed: %v\n%s", err, stdout.String())
	}

	data, err := os.ReadFile(filepath.Join(tmpDir, "greet.go"))
	if err != nil || !bytes.Contains(data, []byte("func Greet")) {
		t.Fatalf("greet.go not written (err=%v)\n%s", err, stdout.String())
	}
	if !sawFileContent {
		t.Errorf("read_file output did not reach the model in full")
	}
	if !sawFindingsInPlan {
		t.Errorf("plan turn did not receive exploration findings")
	}
	if !sawPlanInExecute {
		t.Errorf("execute turn did not receive the approved plan")
	}
	if !strings.Contains(stdout.String(), "Done: created greet.go") {
		t.Errorf("final summary not printed:\n%s", stdout.String())
	}
	// go.mod is absent, so verification is skipped and execute is called exactly twice.
	if calls["execute"] != 2 {
		t.Errorf("expected 2 execute turns, got %d", calls["execute"])
	}
}
