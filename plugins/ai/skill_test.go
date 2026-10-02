package ai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"
)

var skillFS = fstest.MapFS{
	"skills/refunds/SKILL.md":  {Data: []byte("---\nname: refunds\ndescription: >\n  Issue or refuse refunds under the\n  30-day policy.\n---\n# Refunds\n\nCheck the order date first. See policy.md.\n")},
	"skills/refunds/policy.md": {Data: []byte("Refunds are allowed within 30 days.")},
	"skills/chargebacks.md":    {Data: []byte("---\ndescription: Respond to a card chargeback.\n---\nGather the receipt and delivery proof.")},
	"skills/README.md":         {Data: []byte("not a skill")},
	"skills/notes/todo.txt":    {Data: []byte("a folder without SKILL.md is ignored")},
}

func TestLoadSkillsFromFS(t *testing.T) {
	set, err := LoadSkills(skillFS, "skills")
	if err != nil {
		t.Fatal(err)
	}
	if set.Len() != 2 {
		t.Fatalf("loaded %d skills, want 2: %+v", set.Len(), set.Skills())
	}
	refunds, ok := set.Get("refunds")
	if !ok || refunds.Description != "Issue or refuse refunds under the 30-day policy." {
		t.Fatalf("refunds = %+v", refunds)
	}
	if !strings.HasPrefix(refunds.Instructions(), "# Refunds") {
		t.Fatalf("instructions = %q", refunds.Instructions())
	}
	if files := refunds.Files(); len(files) != 1 || files[0] != "policy.md" {
		t.Fatalf("files = %v", files)
	}
	if got, err := refunds.ReadFile("policy.md"); err != nil || !strings.Contains(got, "30 days") {
		t.Fatalf("ReadFile = %q, %v", got, err)
	}
	if _, err := refunds.ReadFile("../chargebacks.md"); err == nil {
		t.Fatal("a skill file path escaped the skill's folder")
	}
	// Single-file skills take their name from the file.
	if cb, ok := set.Get("chargebacks"); !ok || cb.Instructions() != "Gather the receipt and delivery proof." {
		t.Fatalf("chargebacks = %+v", cb)
	}
}

func TestSkillValidation(t *testing.T) {
	cases := map[string]Skill{
		"bad name":       NewSkill("Refund Policy", "d", "x"),
		"no description": NewSkill("refunds", "", "x"),
		"no body":        NewSkill("refunds", "d", "   "),
	}
	for name, sk := range cases {
		if _, err := NewSkillSet(sk); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if _, err := NewSkillSet(NewSkill("a", "d", "x"), NewSkill("a", "d", "y")); err == nil {
		t.Error("duplicate names should be rejected")
	}
	if _, _, _, err := ParseSkill("no frontmatter here"); err == nil {
		t.Error("a file without frontmatter should be rejected")
	}
	bad := fstest.MapFS{"s/x/SKILL.md": {Data: []byte("---\nname: Not Valid\ndescription: d\n---\nbody")}}
	if _, err := LoadSkills(bad, "s"); err == nil {
		t.Error("LoadSkills should reject an invalid skill name")
	}
}

func TestAgentListsSkillsAndLoadsOneOnDemand(t *testing.T) {
	fake := NewFake(
		FakeToolCall(SkillToolName, `{"name":"refunds"}`),
		FakeToolCall(SkillToolName, `{"name":"refunds","file":"policy.md"}`),
		FakeText("You can get a refund: the order is 12 days old."),
	)
	agent := NewAgent("You are a support agent.").
		WithClient(fake.Client()).
		WithSkills(MustLoadSkills(skillFS, "skills"))

	resp, err := agent.Prompt(context.Background(), "Can I get my money back for order 42?")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.Text, "refund") {
		t.Fatalf("answer = %q", resp.Text)
	}

	reqs := fake.Requests()
	first := reqs[0]
	if !strings.HasPrefix(first.System, "You are a support agent.") ||
		!strings.Contains(first.System, "- refunds: Issue or refuse refunds") ||
		!strings.Contains(first.System, "- chargebacks: Respond to a card chargeback.") {
		t.Fatalf("system prompt does not list the skills:\n%s", first.System)
	}
	if strings.Contains(first.System, "Check the order date first") {
		t.Fatal("full skill instructions should not be in the prompt until loaded")
	}
	hasTool := false
	for _, tool := range first.Tools {
		if tool.Name == SkillToolName {
			hasTool = true
			var schema map[string]any
			_ = json.Unmarshal(tool.Parameters, &schema)
			if req, _ := schema["required"].([]any); len(req) != 1 || req[0] != "name" {
				t.Fatalf("load_skill schema = %s", tool.Parameters)
			}
		}
	}
	if !hasTool {
		t.Fatal("agent with skills should offer the load_skill tool")
	}

	// Step 2 sees the skill's instructions; step 3 sees the file.
	lastTool := func(r GenerateRequest) string {
		for i := len(r.Messages) - 1; i >= 0; i-- {
			if r.Messages[i].Role == RoleTool {
				return r.Messages[i].Content
			}
		}
		return ""
	}
	if got := lastTool(reqs[1]); !strings.Contains(got, "Check the order date first") || !strings.Contains(got, "policy.md") {
		t.Fatalf("skill load returned %q", got)
	}
	if got := lastTool(reqs[2]); !strings.Contains(got, "within 30 days") {
		t.Fatalf("skill file load returned %q", got)
	}
}

func TestAgentUnknownSkillIsReportedToTheModel(t *testing.T) {
	fake := NewFake(FakeToolCall(SkillToolName, `{"name":"nope"}`), FakeText("ok"))
	_, err := NewAgent("x").WithClient(fake.Client()).
		WithSkill(NewSkill("refunds", "Refunds.", "Body.")).
		Prompt(context.Background(), "hi")
	if err != nil {
		t.Fatal(err)
	}
	msgs := fake.Requests()[1].Messages
	got := msgs[len(msgs)-1].Content
	if !strings.Contains(got, `no skill named "nope"`) || !strings.Contains(got, "refunds") {
		t.Fatalf("tool result = %q", got)
	}
}

func TestAgentWithInvalidSkillsFailsOnUse(t *testing.T) {
	fake := NewFake(FakeText("never"))
	agent := NewAgent("x").WithClient(fake.Client()).WithSkill(NewSkill("Bad Name", "d", "b"))
	if _, err := agent.Prompt(context.Background(), "hi"); err == nil {
		t.Fatal("an agent with an invalid skill should refuse to run")
	}
	if len(fake.Requests()) != 0 {
		t.Fatal("no request should reach the model")
	}
}

func TestAgentWithoutSkillsHasNoSkillTool(t *testing.T) {
	fake := NewFake(FakeText("hi"))
	if _, err := NewAgent("plain").WithClient(fake.Client()).Prompt(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	req := fake.Requests()[0]
	if req.System != "plain" || len(req.Tools) != 0 {
		t.Fatalf("system=%q tools=%v", req.System, req.Tools)
	}
}

// ── Agent.Stream ────────────────────────────────────────────────────

type echoInput struct {
	Q string `json:"q"`
}

func echoTool(t *testing.T) *Tool {
	t.Helper()
	tool, err := NewTool("echo").Desc("echo").Handler(func(_ context.Context, in echoInput) (string, error) {
		return "echo:" + in.Q, nil
	}).Build()
	if err != nil {
		t.Fatal(err)
	}
	return tool
}

func TestAgentStreamDoesNotAskTwiceAndSavesTheAnswer(t *testing.T) {
	for _, streamTools := range []bool{false, true} {
		fake := NewFake(FakeToolCall("echo", `{"q":"x"}`), FakeText("The final answer."))
		fake.StreamTools = streamTools
		mem := MemoryStore()
		agent := NewAgent("x").WithClient(fake.Client()).WithToolObjects(echoTool(t)).
			WithMemory(mem, "conv")

		st, err := agent.Stream(context.Background(), "go", WithImages([]string{"data:image/png;base64,AA=="}))
		if err != nil {
			t.Fatal(err)
		}
		resp, err := st.Collect(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if resp.Text != "The final answer." {
			t.Fatalf("streamTools=%v: streamed %q", streamTools, resp.Text)
		}
		if n := len(fake.Requests()); n != 2 {
			t.Fatalf("streamTools=%v: %d model calls, want 2 (tool step + answer)", streamTools, n)
		}
		if imgs := fake.Requests()[0].Messages[0].Images; len(imgs) != 1 {
			t.Fatalf("streamTools=%v: images were dropped: %v", streamTools, imgs)
		}
		history, _ := mem.Load(context.Background(), "conv")
		last := history[len(history)-1]
		if last.Role != RoleAssistant || last.Content != "The final answer." {
			t.Fatalf("streamTools=%v: memory ends with %+v", streamTools, last)
		}
	}
}

func TestAgentStreamWithoutToolsStreamsWordByWord(t *testing.T) {
	fake := NewFake(FakeText("one two three"))
	st, err := NewAgent("x").WithClient(fake.Client()).Stream(context.Background(), "hi")
	if err != nil {
		t.Fatal(err)
	}
	var chunks int
	for c := range st.Chunks {
		if c.Text != "" {
			chunks++
		}
	}
	if err := <-st.Err; err != nil {
		t.Fatal(err)
	}
	if chunks != 3 {
		t.Fatalf("got %d text chunks, want 3 (streamed, not one blob)", chunks)
	}
}

func TestAgentStreamReportsErrors(t *testing.T) {
	fake := NewFake() // no responses
	st, err := NewAgent("x").WithClient(fake.Client()).Stream(context.Background(), "hi")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Collect(context.Background()); err == nil {
		t.Fatal("a failing model call should surface on Err")
	}
}

func TestAgentPromptSendsImagesOnce(t *testing.T) {
	fake := NewFake(FakeToolCall("echo", `{"q":"x"}`), FakeText("done"))
	_, err := NewAgent("x").WithClient(fake.Client()).WithToolObjects(echoTool(t)).
		Prompt(context.Background(), "look", WithImages([]string{"data:image/png;base64,AA=="}))
	if err != nil {
		t.Fatal(err)
	}
	for i, req := range fake.Requests() {
		if imgs := req.Messages[0].Images; len(imgs) != 1 {
			t.Fatalf("step %d sent %d copies of the image", i, len(imgs))
		}
	}
}

// TestDocsTestingExample mirrors the Testing example on the AI SDK docs page.
func TestDocsTestingExample(t *testing.T) {
	type orderIn struct {
		OrderID string `description:"The order ID"`
	}
	RegisterTool(Tool{Name: "lookup_order_docs", Description: "Look up an order",
		Run: func(_ context.Context, in orderIn) (map[string]string, error) {
			return map[string]string{"id": in.OrderID, "status": "delivered"}, nil
		}})
	support := func() *Agent {
		return NewAgent("You are a helpful customer support agent.").
			WithSkills(MustLoadSkills(skillFS, "skills")).
			WithTools("lookup_order_docs").
			MaxSteps(8)
	}

	fake := NewFake(
		FakeToolCall("load_skill", `{"name":"refunds"}`),
		FakeToolCall("lookup_order_docs", `{"OrderID":"42"}`),
		FakeText("Refunded. The money arrives in 3–5 business days."),
	)
	defer fake.Install()()

	resp, err := support().Prompt(context.Background(), "Refund order 42 please")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.Text, "Refunded") {
		t.Fatalf("reply = %q", resp.Text)
	}
	if !strings.Contains(fake.Requests()[0].System, "- refunds:") {
		t.Fatal("the refunds skill is not offered")
	}
	last := fake.Requests()[2].Messages
	if got := last[len(last)-1].Content; !strings.Contains(got, "delivered") {
		t.Fatalf("tool result = %q", got)
	}
	if fake.Remaining() != 0 {
		t.Fatal("script not fully used")
	}
}
