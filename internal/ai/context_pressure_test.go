package ai

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// pressureClient records how full the window was at each agent turn, so a test
// can assert on the shape of the curve and not just its endpoint.
type pressureClient struct {
	scriptedTurnClient
	session *Session
	peak    int
}

func (p *pressureClient) Turn(ctx context.Context, req *TurnRequest, onDelta StreamHandler) (*MessageResponse, error) {
	if p.session != nil && req.Mode == TurnModeAgent {
		if pct := p.session.ContextUsage().Percent(); pct > p.peak {
			p.peak = pct
		}
	}
	return p.scriptedTurnClient.Turn(ctx, req, onDelta)
}

// A single turn can run dozens of rounds, and tool results are where its
// context goes. Compaction used to be checked only before the loop, so one
// request filled the window and stayed there until the *next* message — the
// gauge pinned at 100% and then dropped to nearly nothing in one step.
func TestLongTurnCompactsMidFlight(t *testing.T) {
	t.Setenv(contextLimitEnv, "12000")

	dir := t.TempDir()
	const rounds = 16
	queue := make([]*MessageResponse, 0, rounds)
	for i := 0; i < rounds; i++ {
		name := fmt.Sprintf("file%d.go", i)
		body := strings.Repeat("// a line of source that costs real tokens\n", 90)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
		queue = append(queue, toolUse(strconv.Itoa(i), "read_file", map[string]any{"path": name}))
	}

	client := &pressureClient{
		scriptedTurnClient: scriptedTurnClient{
			responses: map[TurnMode][]*MessageResponse{
				TurnModeAgent: queue,
				TurnModeChat:  {text("GOAL — read the sources\nLEARNED — file0..file15 are stubs")},
			},
		},
	}
	session := NewSession("optimal")
	client.session = session

	agent := NewAgent(client, NewToolExecutor(dir), &ProjectContext{AppRoot: dir}, session)
	agent.Verifier = nil

	if _, err := agent.Run(context.Background(), "read every file in the project"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// The session began empty, so the pre-loop check cannot have fired: any
	// summarising turn here happened inside the loop.
	if client.calls[TurnModeChat] == 0 {
		t.Error("the turn ran to the end of the window without ever compacting")
	}
	if client.peak >= 100 {
		t.Errorf("the window pinned at %d%% during the turn", client.peak)
	}
}

// The gauge has to account for what the request carries besides the messages —
// the system prompt, the tool schemas, the project context. None of it is
// visible to EstimateTokens, and it is thousands of tokens.
func TestContextUsageIncludesServerMeasuredOverhead(t *testing.T) {
	s := NewSession("optimal")
	s.Messages = longConversation(10)

	estimate := EstimateTokens(s.Messages)
	if got := s.ContextUsage().Tokens; got != estimate {
		t.Fatalf("before calibration tokens = %d, want the bare estimate %d", got, estimate)
	}

	s.RecordContextTokens(estimate + 5000)
	if got := s.ContextUsage().Tokens; got != estimate+5000 {
		t.Errorf("after calibration tokens = %d, want %d", got, estimate+5000)
	}

	// Compaction shrinks the messages; the overhead does not go with them, so
	// the reading has to stay honest before the next turn measures again.
	s.Messages = longConversation(2)
	want := EstimateTokens(s.Messages) + 5000
	if got := s.ContextUsage().Tokens; got != want {
		t.Errorf("after shrinking tokens = %d, want %d", got, want)
	}
}

// A server that reports fewer tokens than the estimate must not push the
// overhead negative and start under-reporting.
func TestRecordContextTokensIgnoresImplausibleCounts(t *testing.T) {
	s := NewSession("optimal")
	s.Messages = longConversation(10)

	s.RecordContextTokens(0)
	s.RecordContextTokens(1)
	if s.ContextOverhead != 0 {
		t.Errorf("overhead = %d, want 0", s.ContextOverhead)
	}
}

// One read used to be able to put 160KB — roughly 40k tokens — into the
// conversation, so three of them exhausted a 128k window.
func TestReadFileIsBoundedAndReportsWhatItReturned(t *testing.T) {
	dir := t.TempDir()
	line := strings.Repeat("x", 200) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), []byte(strings.Repeat(line, 4000)), 0644); err != nil {
		t.Fatal(err)
	}

	out, err := NewToolExecutor(dir).ReadFileRange("big.txt", 0, 0)
	if err != nil {
		t.Fatalf("ReadFileRange: %v", err)
	}
	if len(out) > maxReadBytes+1024 {
		t.Errorf("read returned %d bytes, cap is %d", len(out), maxReadBytes)
	}

	header, body, _ := strings.Cut(out, "\n\n")
	var claimed int
	if _, err := fmt.Sscanf(header[strings.Index(header, "lines 1-")+len("lines 1-"):], "%d", &claimed); err != nil {
		t.Fatalf("no line count in header %q", header)
	}
	if got := len(strings.Split(body, "\n")); got != claimed {
		t.Errorf("header claims %d lines, body has %d", claimed, got)
	}
}

// A single line longer than the cap (minified source) still has to yield
// something, and must not blow the budget doing it.
func TestReadFileBoundsASingleEnormousLine(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bundle.js"), []byte(strings.Repeat("a", 500_000)), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := NewToolExecutor(dir).ReadFileRange("bundle.js", 0, 0)
	if err != nil {
		t.Fatalf("ReadFileRange: %v", err)
	}
	if len(out) > maxReadBytes+1024 {
		t.Errorf("read returned %d bytes, cap is %d", len(out), maxReadBytes)
	}
}

// Compaction that keeps twelve verbatim messages can keep several large reads
// with them, leaving the result still over the threshold: it then reclaims
// nothing and is attempted again on every following turn.
func TestShrinkToolResultsSpareTheLiveEnd(t *testing.T) {
	// Results large enough to matter: a tail of small ones has nothing to give.
	messages := []Message{{Role: "user", Content: "read the sources"}}
	for i := 0; i < 6; i++ {
		id := fmt.Sprintf("t%d", i)
		messages = append(messages, Message{Role: "assistant", Content: []ContentBlock{
			{Type: "tool_use", ID: id, Name: "read_file", Input: map[string]any{"path": fmt.Sprintf("f%d.go", i)}},
		}})
		messages = append(messages, Message{Role: "user", Content: []ContentBlock{
			{Type: "tool_result", ToolUseID: id, Content: strings.Repeat("source line\n", 2000)},
		}})
	}
	before := EstimateTokens(messages)

	got := shrinkToolResults(messages, keptIntactMessages, maxRetainedResultChars)
	if len(got) != len(messages) {
		t.Fatalf("message count changed: %d -> %d", len(messages), len(got))
	}
	if EstimateTokens(got) >= before {
		t.Error("shrinking the tail reclaimed nothing")
	}

	for i := len(got) - keptIntactMessages; i < len(got); i++ {
		if EstimateTokens(got[i:i+1]) != EstimateTokens(messages[i:i+1]) {
			t.Errorf("message %d is in the live end and was truncated anyway", i)
		}
	}
	// The originals must not have been rewritten underneath the caller.
	if EstimateTokens(messages) != before {
		t.Error("shrinkToolResults mutated its input")
	}
}
