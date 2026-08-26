package tui

import (
	"strings"
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
