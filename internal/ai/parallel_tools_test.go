package ai

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// toolAgent builds an agent over a workspace of numbered files.
func toolAgent(t *testing.T, files int) (*Agent, string) {
	t.Helper()
	dir := t.TempDir()
	for i := 0; i < files; i++ {
		body := fmt.Sprintf("package main\n// contents of file %d\n", i)
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%d.go", i)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	agent := NewAgent(&mockAIClient{}, NewToolExecutor(dir), &ProjectContext{AppRoot: dir}, NewSession("optimal"))
	agent.Verifier = nil
	return agent, dir
}

func readCall(id, path string) ContentBlock {
	return ContentBlock{Type: "tool_use", ID: id, Name: "read_file", Input: map[string]any{"path": path}}
}

// Only calls that observe the workspace may overlap. A write followed by a
// read of the same path has to see what the write did.
func TestOnlyReadOnlyToolsAreParallelSafe(t *testing.T) {
	for _, name := range []string{"read_file", "read", "list_dir", "find_files", "glob", "grep", "search", "fetch_url"} {
		if !isParallelSafe(name) {
			t.Errorf("%s observes the workspace and should run in parallel", name)
		}
	}
	for _, name := range []string{"write_file", "edit_file", "delete_file", "bash", "load_skill", "query_skill", "generate_image"} {
		if isParallelSafe(name) {
			t.Errorf("%s changes something and must stay sequential", name)
		}
	}
}

// The risk of running calls together is a result landing against the wrong
// call. Every result must match the file its own call named.
func TestParallelResultsStayWithTheirCalls(t *testing.T) {
	agent, _ := toolAgent(t, 8)

	var calls []ContentBlock
	for i := 0; i < 8; i++ {
		calls = append(calls, readCall(fmt.Sprintf("t%d", i), fmt.Sprintf("f%d.go", i)))
	}

	results := agent.runToolCalls(context.Background(), calls)
	if len(results) != len(calls) {
		t.Fatalf("got %d results for %d calls", len(results), len(calls))
	}
	for i, res := range results {
		if res.ToolUseID != calls[i].ID {
			t.Fatalf("result %d answers %s, want %s", i, res.ToolUseID, calls[i].ID)
		}
		if res.IsError {
			t.Fatalf("result %d failed: %s", i, res.Content)
		}
		if want := fmt.Sprintf("contents of file %d", i); !strings.Contains(res.Content, want) {
			t.Errorf("result %d carries the wrong file:\n%s", i, res.Content)
		}
	}
}

// A batch that mixes reads with a write keeps the model's ordering, and the
// write still happens.
func TestMixedBatchKeepsOrderAndStillWrites(t *testing.T) {
	agent, dir := toolAgent(t, 3)

	calls := []ContentBlock{
		readCall("a", "f0.go"),
		readCall("b", "f1.go"),
		{Type: "tool_use", ID: "w", Name: "write_file", Input: map[string]any{
			"path": "new.go", "content": "package main\n"}},
		readCall("c", "f2.go"),
	}

	var seen []string
	agent.Callbacks.OnToolResult = func(name string, _ map[string]any, _ string, _ error) {
		seen = append(seen, name)
	}

	results := agent.runToolCalls(context.Background(), calls)

	var gotIDs []string
	for _, r := range results {
		gotIDs = append(gotIDs, r.ToolUseID)
	}
	if strings.Join(gotIDs, ",") != "a,b,w,c" {
		t.Errorf("results out of order: %v", gotIDs)
	}
	if strings.Join(seen, ",") != "read_file,read_file,write_file,read_file" {
		t.Errorf("callbacks out of order: %v", seen)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.go")); err != nil {
		t.Errorf("the write in the middle of the batch did not happen: %v", err)
	}
}

// The executor's read cache and injection taint are written from every tool
// call. Running reads together must not corrupt them.
func TestConcurrentReadsDoNotRaceTheExecutor(t *testing.T) {
	agent, _ := toolAgent(t, 6)

	var wg sync.WaitGroup
	for round := 0; round < 4; round++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var calls []ContentBlock
			for i := 0; i < 6; i++ {
				calls = append(calls, readCall(fmt.Sprintf("r%d", i), fmt.Sprintf("f%d.go", i)))
			}
			agent.runToolCalls(context.Background(), calls)
		}()
	}
	wg.Wait()

	// Whatever the interleaving, the cache must still answer sensibly.
	if _, _, err := agent.Tools.ExecuteTool(context.Background(), "read_file",
		map[string]any{"path": "f0.go"}); err != nil {
		t.Errorf("the executor is broken after concurrent use: %v", err)
	}
}

// A failing call reports as an error against its own id, without disturbing
// the calls beside it.
func TestOneFailureInAGroupDoesNotAffectTheOthers(t *testing.T) {
	agent, _ := toolAgent(t, 3)

	calls := []ContentBlock{
		readCall("ok1", "f0.go"),
		readCall("bad", "does-not-exist.go"),
		readCall("ok2", "f1.go"),
	}
	results := agent.runToolCalls(context.Background(), calls)

	if len(results) != 3 {
		t.Fatalf("got %d results", len(results))
	}
	if results[0].IsError || results[2].IsError {
		t.Error("a neighbouring failure broke the successful reads")
	}
	if !results[1].IsError {
		t.Error("reading a missing file was not reported as an error")
	}
	if results[1].ToolUseID != "bad" {
		t.Errorf("the error landed on %s", results[1].ToolUseID)
	}
}
