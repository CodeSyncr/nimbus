package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CodeSyncr/nimbus/internal/ai"
)

func TestRenderMarkdown(t *testing.T) {
	input := `# Main Title
## Sub Title
### Section
This is **bold** text and ` + "`inline code`" + `.

- Bullet 1
- Bullet 2

> Important note

` + "```go\nfunc main() {\n\tprintln(\"hello\")\n}\n```"

	rendered := RenderMarkdown(input, 80)
	if !strings.Contains(rendered, "Main Title") {
		t.Errorf("expected Main Title in output, got: %s", rendered)
	}
	if !strings.Contains(rendered, "Sub Title") {
		t.Errorf("expected Sub Title in output, got: %s", rendered)
	}
	if !strings.Contains(rendered, "Section") {
		t.Errorf("expected Section in output, got: %s", rendered)
	}
	if !strings.Contains(rendered, "Bullet 1") {
		t.Errorf("expected Bullet 1 in output, got: %s", rendered)
	}
	if !strings.Contains(rendered, "Important note") {
		t.Errorf("expected Important note in output, got: %s", rendered)
	}
	if !strings.Contains(rendered, "func main()") {
		t.Errorf("expected code block in output, got: %s", rendered)
	}
}

func TestRenderColorizedDiff(t *testing.T) {
	diff := `--- a/main.go
+++ b/main.go
@@ -1,3 +1,4 @@
 package main
+import "fmt"
-func old() {}
`
	rendered := RenderColorizedDiff(diff)
	if !strings.Contains(rendered, "main.go") {
		t.Errorf("expected diff to contain main.go, got: %s", rendered)
	}
	if !strings.Contains(rendered, "+import \"fmt\"") {
		t.Errorf("expected diff to contain added line, got: %s", rendered)
	}
}

func TestTUIModelAndViews(t *testing.T) {
	projCtx := &ai.ProjectContext{
		AppRoot:       "/test/app",
		ProjectName:   "my-test-app",
		GitBranch:     "feature/new-ui",
		NimbusModules: []string{"router", "orm"},
	}
	session := ai.NewSession("optimal")
	agent := ai.NewAgent(nil, ai.NewToolExecutor("/test/app"), projCtx, session)

	model := NewModel(agent, "", false)
	model.Ready = true
	model.Width = 100
	model.Height = 30

	// Test Initial View
	viewOut := model.View()
	if !strings.Contains(viewOut, "Nimbus Code") && !strings.Contains(viewOut, "Nimbus") {
		t.Errorf("expected Nimbus in view output, got: %s", viewOut)
	}
	if !strings.Contains(viewOut, "my-test-app") {
		t.Errorf("expected workspace name in header, got: %s", viewOut)
	}
	if !strings.Contains(viewOut, "feature/new-ui") {
		t.Errorf("expected branch in header, got: %s", viewOut)
	}

	// Add messages
	model.Messages = append(model.Messages,
		ChatItem{
			Role:      "user",
			Content:   "Create a User model",
			Timestamp: time.Now(),
		},
		ChatItem{
			Role:      "tool",
			ToolName:  "read_file",
			ToolArgs:  map[string]any{"path": "models/user.go"},
			Timestamp: time.Now(),
		},
		ChatItem{
			Role:      "assistant",
			Content:   "Created model `models/user.go` successfully.",
			Timestamp: time.Now(),
			Diffs:     []string{"+type User struct {}"},
		},
	)

	chatView := renderChatHistory(&model)
	if !strings.Contains(chatView, "Create a User model") {
		t.Errorf("expected user prompt in chatView, got: %s", chatView)
	}
	if !strings.Contains(chatView, "Read") || !strings.Contains(chatView, "models/user.go") {
		t.Errorf("expected 'Read models/user.go' tool line in chatView, got: %s", chatView)
	}

	// Test Plan Review View
	model.Mode = ModePlanReview
	model.Agent.Session.Plan = &ai.PlanSummary{
		Summary: "Scaffold Authentication",
		Steps: []ai.PlanStep{
			{ID: 1, Action: "create_file", Target: "models/user.go", Description: "Create User schema", Risk: "low", Approved: true},
			{ID: 2, Action: "edit_file", Target: "routes/api.go", Description: "Register auth routes", Risk: "med", Approved: false},
		},
	}

	planOut := renderPlanView(&model)
	if !strings.Contains(planOut, "Scaffold Authentication") {
		t.Errorf("expected plan summary in planOut, got: %s", planOut)
	}
	if !strings.Contains(planOut, "models/user.go") {
		t.Errorf("expected step target in planOut, got: %s", planOut)
	}
	if !strings.Contains(planOut, "CREATE") {
		t.Errorf("expected CREATE action tag, got: %s", planOut)
	}
}

// A file change is announced by its shape, not pasted in full: a run of edits
// has to stay readable in the transcript.
func TestToolLineCollapsesDiffsByDefault(t *testing.T) {
	var diff strings.Builder
	diff.WriteString("--- a/main.go\n+++ b/main.go\n")
	for i := 0; i < 40; i++ {
		diff.WriteString(fmt.Sprintf("+line %d\n", i))
	}
	item := ChatItem{
		Role: "tool", ToolName: "edit_file", Content: "main.go",
		ToolArgs: map[string]any{"path": "main.go"}, Detail: "edited", Diff: diff.String(),
	}

	collapsed := renderToolLine(item, 100, "", false)
	if strings.Count(collapsed, "\n") > collapsedDiffLines+4 {
		t.Errorf("collapsed diff is %d lines; it should stay compact:\n%s", strings.Count(collapsed, "\n"), collapsed)
	}
	if !strings.Contains(collapsed, "+40") {
		t.Errorf("collapsed view should state the change shape:\n%s", collapsed)
	}
	if !strings.Contains(collapsed, "ctrl+o") {
		t.Errorf("collapsed view should say how to expand:\n%s", collapsed)
	}

	expanded := renderToolLine(item, 100, "", true)
	if strings.Count(expanded, "\n") <= strings.Count(collapsed, "\n") {
		t.Error("ctrl+o should reveal more of the diff")
	}
}

// Links are only emitted for terminals that render them, and only for paths
// that exist — a dead link is worse than plain text.
func TestFilePathsLinkOnlyWhenUseful(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "real.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("NIMBUS_HYPERLINKS", "1")
	linkOnce = sync.Once{}
	if got := linkFile(dir, "real.go", "real.go"); !strings.Contains(got, "\x1b]8;;file://") {
		t.Errorf("existing file was not linked: %q", got)
	}
	if got := linkFile(dir, "ghost.go", "ghost.go"); got != "ghost.go" {
		t.Errorf("missing file should not be linked: %q", got)
	}

	t.Setenv("NIMBUS_HYPERLINKS", "0")
	linkOnce = sync.Once{}
	if got := linkFile(dir, "real.go", "real.go"); got != "real.go" {
		t.Errorf("links disabled but got escapes: %q", got)
	}
	linkOnce = sync.Once{}
}

// The transcript should read in the order things happened: what the model was
// thinking, then the actions that thinking led to — not all the narration
// dumped at the end of the run.
func TestNarrationIsCommittedBeforeTheToolsItExplains(t *testing.T) {
	m := Model{StreamBuffer: &strings.Builder{}, Messages: []ChatItem{}}
	m.segmentStart = time.Now().Add(-3 * time.Second)

	// The model narrates, then calls a tool.
	m.StreamBuffer.WriteString("I'll add the greeter to main.go.")
	m.recordThought()
	m.flushNarration()

	if len(m.Messages) != 2 {
		t.Fatalf("expected a thought line and the narration, got %d items", len(m.Messages))
	}
	if m.Messages[0].Role != "phase" || m.Messages[0].Content != "Thought" {
		t.Errorf("first item should be the thought record, got %+v", m.Messages[0])
	}
	if m.Messages[0].Elapsed < 2*time.Second {
		t.Errorf("thought duration not measured: %v", m.Messages[0].Elapsed)
	}
	if m.Messages[1].Role != "assistant" || !strings.Contains(m.Messages[1].Content, "greeter") {
		t.Errorf("narration was not committed: %+v", m.Messages[1])
	}
	if m.StreamBuffer.Len() != 0 {
		t.Error("buffer should be empty after flushing")
	}

	// A second flush with nothing buffered must not add an empty message.
	m.flushNarration()
	if len(m.Messages) != 2 {
		t.Errorf("empty flush added a message: %d", len(m.Messages))
	}
}

// One stretch of thinking produces one record, however many tools follow.
func TestThoughtRecordedOncePerStretch(t *testing.T) {
	m := Model{StreamBuffer: &strings.Builder{}}
	m.segmentStart = time.Now().Add(-2 * time.Second)

	m.recordThought()
	m.recordThought()
	m.recordThought()

	if len(m.Messages) != 1 {
		t.Errorf("expected 1 thought line, got %d", len(m.Messages))
	}
}

// Instant replies are not worth a "Thought for 0s" line.
func TestSubSecondThinkingIsNotRecorded(t *testing.T) {
	m := Model{StreamBuffer: &strings.Builder{}}
	m.segmentStart = time.Now()

	m.recordThought()
	if len(m.Messages) != 0 {
		t.Errorf("sub-second thinking should be silent, got %+v", m.Messages)
	}
}

// The rendered line reads as a record, not a label.
func TestThoughtLineRendersReadably(t *testing.T) {
	m := Model{
		Width: 100, Height: 40, Ready: true,
		StreamBuffer: &strings.Builder{},
		Messages: []ChatItem{
			{Role: "phase", Content: "Thought", Elapsed: 8 * time.Second},
		},
	}
	out := renderChatHistory(&m)
	if !strings.Contains(out, "Thought for 8s") {
		t.Errorf("expected 'Thought for 8s' in:\n%s", out)
	}
}
