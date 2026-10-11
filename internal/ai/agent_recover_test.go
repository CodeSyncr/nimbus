package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stepClient answers each agent turn from a list of steps, in order. A step
// sees the request it was sent, so a test can act mid-turn the way a user does.
type stepClient struct {
	mockAIClient
	steps    []func(req *TurnRequest) (*MessageResponse, error)
	requests []*TurnRequest
}

func (s *stepClient) Turn(_ context.Context, req *TurnRequest, _ StreamHandler) (*MessageResponse, error) {
	cp := *req
	cp.Messages = append([]Message{}, req.Messages...)
	s.requests = append(s.requests, &cp)
	if len(s.requests) > len(s.steps) {
		return text("Done."), nil
	}
	return s.steps[len(s.requests)-1](&cp)
}

func reply(r *MessageResponse) func(*TurnRequest) (*MessageResponse, error) {
	return func(*TurnRequest) (*MessageResponse, error) { return r, nil }
}

// sentText is everything a request carried, as one text.
func sentText(req *TurnRequest) string {
	b, _ := json.Marshal(req.Messages)
	return string(b)
}

// A write cut off at the output limit is not run on what arrived of it. The
// model is told, in the conversation, and the turn carries on.
func TestACutOffToolCallIsAnsweredNotRun(t *testing.T) {
	cut := toolUse("t1", "write_file", map[string]any{"path": "app/models/post.go"})
	cut.Content[0].Incomplete = true
	cut.StopReason = "max_tokens"
	client := &stepClient{steps: []func(*TurnRequest) (*MessageResponse, error){
		reply(cut),
		reply(toolUse("t2", "write_file", map[string]any{"path": "app/models/post.go", "content": "package models\n\ntype Post struct{}\n"})),
		reply(text("Added the Post model.")),
	}}
	agent, dir := runAgent(t, client)

	res, err := agent.Run(context.Background(), "add a Post model")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(client.requests) != 3 {
		t.Fatalf("%d turns, want 3", len(client.requests))
	}
	told := sentText(client.requests[1])
	if !strings.Contains(told, "this call was not run") || !strings.Contains(told, "edit_file") {
		t.Errorf("the model was not told its call was cut off:\n%s", told)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "app/models/post.go"))
	if string(got) != "package models\n\ntype Post struct{}\n" {
		t.Errorf("the file on disk is not the whole one: %q", got)
	}
	if !strings.Contains(res.Text, "Added the Post model") {
		t.Errorf("answer = %q", res.Text)
	}
}

// A server that predates the marker sends a cut-off call with no arguments at
// all, and says only that the reply stopped at the limit. That is the same
// thing and is treated the same; a call that simply takes no arguments is not.
func TestACallWithNoArgumentsIsCutOffOnlyWhenTheReplyWas(t *testing.T) {
	agent, _ := runAgent(t, &stepClient{})
	empty := ContentBlock{Type: "tool_use", ID: "t1", Name: "write_file", Input: map[string]any{}}

	for _, stop := range []string{"max_tokens", "length"} {
		agent.lastStop = stop
		if !agent.cutOff(empty) {
			t.Errorf("stop %q: a call with no arguments should count as cut off", stop)
		}
	}
	for _, stop := range []string{"tool_use", "end_turn", "stop", ""} {
		agent.lastStop = stop
		if agent.cutOff(empty) {
			t.Errorf("stop %q: a call with no arguments is just that", stop)
		}
	}
	agent.lastStop = "max_tokens"
	if agent.cutOff(ContentBlock{Type: "tool_use", Name: "read_file", Input: map[string]any{"path": "main.go"}}) {
		t.Error("a whole call in a reply that later hit the limit must still run")
	}
}

// An empty reply used to be reported as "Done.". It is asked for again, and
// the turn ends with the model's answer.
func TestAnEmptyReplyIsAskedForAgain(t *testing.T) {
	client := &stepClient{steps: []func(*TurnRequest) (*MessageResponse, error){
		reply(toolUse("t1", "read_file", map[string]any{"path": "main.go"})),
		reply(&MessageResponse{Role: "assistant"}),
		func(*TurnRequest) (*MessageResponse, error) { return nil, errors.Join(ErrEmptyReply) },
		reply(text("main.go holds an empty main function.")),
	}}
	agent, _ := runAgent(t, client)

	res, err := agent.Run(context.Background(), "what is in main.go?")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(client.requests) != 4 {
		t.Fatalf("%d turns, want 4", len(client.requests))
	}
	if !strings.Contains(lastUserText(client.requests[2]), "your last reply was empty") {
		t.Errorf("the model was not told: %q", lastUserText(client.requests[2]))
	}
	if !strings.Contains(res.Text, "empty main function") {
		t.Errorf("answer = %q", res.Text)
	}
}

// Bounded, and honest when it gives up: not "Done.".
func TestEmptyRepliesEndTheTurnSayingSo(t *testing.T) {
	blank := reply(&MessageResponse{Role: "assistant"})
	client := &stepClient{steps: []func(*TurnRequest) (*MessageResponse, error){blank, blank, blank, blank, blank}}
	agent, _ := runAgent(t, client)

	res, err := agent.Run(context.Background(), "what is in main.go?")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(client.requests) != maxEmptyReplies+1 {
		t.Errorf("%d turns, want %d", len(client.requests), maxEmptyReplies+1)
	}
	if res.Text == "Done." || !strings.Contains(res.Text, "empty reply") {
		t.Errorf("answer = %q", res.Text)
	}
}

// Something typed while the agent works is shown to the model before its next
// step, after the tools of the current one have answered.
func TestWhatIsTypedMidTurnReachesTheNextStep(t *testing.T) {
	var agent *Agent
	var heard []string
	client := &stepClient{}
	client.steps = []func(*TurnRequest) (*MessageResponse, error){
		func(*TurnRequest) (*MessageResponse, error) {
			agent.Steer("name it Article, not Post")
			return toolUse("t1", "read_file", map[string]any{"path": "main.go"}), nil
		},
		reply(text("Named it Article.")),
	}
	agent, _ = runAgent(t, client)
	agent.Callbacks.OnSteered = func(s string) { heard = append(heard, s) }

	res, err := agent.Run(context.Background(), "add a Post model")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	second := client.requests[1]
	if got := lastUserText(second); !strings.Contains(got, "name it Article, not Post") || !strings.Contains(got, "while you were working") {
		t.Errorf("the next step was not shown it: %q", got)
	}
	// after the tool result, never between a call and its result
	msgs := second.Messages
	if blocks, ok := msgs[len(msgs)-2].Content.([]ContentBlock); !ok || blocks[0].Type != "tool_result" {
		t.Errorf("it should follow the tool results: %+v", msgs[len(msgs)-2])
	}
	if len(heard) != 1 || len(agent.TakeSteer()) != 0 {
		t.Errorf("heard=%v left=%v", heard, agent.TakeSteer())
	}
	if !strings.Contains(res.Text, "Article") {
		t.Errorf("answer = %q", res.Text)
	}
}

// Said while the answer was being written: the model has not seen it, so the
// turn does not end on that answer.
func TestWhatIsTypedDuringTheAnswerIsNotLost(t *testing.T) {
	var agent *Agent
	client := &stepClient{}
	client.steps = []func(*TurnRequest) (*MessageResponse, error){
		func(*TurnRequest) (*MessageResponse, error) {
			agent.Steer("and add a test for it")
			return text("Added the model."), nil
		},
		reply(text("Added the model and a test.")),
	}
	agent, _ = runAgent(t, client)

	res, err := agent.Run(context.Background(), "add a Post model")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(client.requests) != 2 || !strings.Contains(res.Text, "and a test") {
		t.Fatalf("turns=%d answer=%q", len(client.requests), res.Text)
	}
	if !strings.Contains(sentText(client.requests[1]), "Added the model.") {
		t.Error("the first answer was dropped from the conversation")
	}
}

// The conversation is sent unaltered once the server has been seen to cache,
// and with older output elided until then.
func TestHistoryIsSentStableOnceTheServerCaches(t *testing.T) {
	long := strings.Repeat("line of file content\n", 400)
	var msgs []Message
	msgs = append(msgs, Message{Role: "user", Content: "read everything"})
	for i := 0; i < 12; i++ {
		id := string(rune('a' + i))
		msgs = append(msgs,
			Message{Role: "assistant", Content: []ContentBlock{{Type: "tool_use", ID: id, Name: "write_file", Input: map[string]any{"path": id + ".go", "content": long}}}},
			Message{Role: "user", Content: []ContentBlock{{Type: "tool_result", ToolUseID: id, Content: long}}})
	}
	wire := func(m []Message, stable bool) string { b, _ := json.Marshal(wireMessages(m, stable)); return string(b) }

	if elided := wire(msgs, false); !strings.Contains(elided, "earlier output elided") {
		t.Fatal("without a cache, older output should still be elided")
	}
	stable := wire(msgs, true)
	if strings.Contains(stable, "elided") || strings.Contains(stable, "content omitted") {
		t.Error("the stable form altered earlier messages")
	}
	// what was sent before is a prefix of what is sent next, byte for byte
	earlier := wire(msgs[:len(msgs)-6], true)
	if !strings.HasPrefix(stable, strings.TrimSuffix(earlier, "]")) {
		t.Error("an earlier request is not a prefix of a later one")
	}
	if earlierElided := wire(msgs[:len(msgs)-6], false); strings.HasPrefix(wire(msgs, false), strings.TrimSuffix(earlierElided, "]")) {
		t.Error("the elided form was expected to rewrite earlier messages (the test is not testing anything)")
	}
	// the cap is per result and lands the same way every time
	huge := []Message{{Role: "user", Content: []ContentBlock{{Type: "tool_result", ToolUseID: "x", Content: strings.Repeat("x", maxToolResultChars+500)}}}}
	if got := wireMessages(huge, true)[0].Content.([]ContentBlock)[0].Content; len(got) > maxToolResultChars+40 {
		t.Errorf("a huge result was sent whole (%d characters)", len(got))
	}
	if len(huge[0].Content.([]ContentBlock)[0].Content) != maxToolResultChars+500 {
		t.Error("the session's own copy was cut")
	}

	// and the agent asks for it on the evidence of a cache read
	withUsage := func(r *MessageResponse, read int) *MessageResponse {
		r.Usage = &TokenUsage{InputTokens: 9000, OutputTokens: 20, CacheReadTokens: read}
		return r
	}
	client := &stepClient{steps: []func(*TurnRequest) (*MessageResponse, error){
		reply(withUsage(toolUse("t1", "read_file", map[string]any{"path": "main.go"}), 0)),
		reply(withUsage(toolUse("t2", "read_file", map[string]any{"path": "main.go"}), 8000)),
		reply(withUsage(text("Read it."), 8500)),
	}}
	agent, _ := runAgent(t, client)
	if _, err := agent.Run(context.Background(), "read main.go"); err != nil {
		t.Fatal(err)
	}
	if client.requests[0].StableHistory || client.requests[1].StableHistory || !client.requests[2].StableHistory {
		t.Errorf("stable: %v %v %v, want false false true", client.requests[0].StableHistory, client.requests[1].StableHistory, client.requests[2].StableHistory)
	}
	if agent.Session.Usage.CacheReadTokens != 16500 {
		t.Errorf("cache reads counted: %d", agent.Session.Usage.CacheReadTokens)
	}
}

// What goes into the instructions is captured when the session starts and is
// not rewritten by the agent's own edits; what rides beside the conversation
// is kept current.
func TestATurnDoesNotRewriteTheSessionsSnapshot(t *testing.T) {
	client := &stepClient{steps: []func(*TurnRequest) (*MessageResponse, error){
		reply(toolUse("t1", "write_file", map[string]any{"path": "notes.txt", "content": "hello"})),
		reply(text("Wrote it.")),
	}}
	agent, _ := runAgent(t, client)
	agent.Context.DirectoryTree = "the tree as it was at the start"
	agent.Context.Instructions = "the instructions as they were at the start"

	if _, err := agent.Run(context.Background(), "write notes.txt"); err != nil {
		t.Fatal(err)
	}
	if agent.Context.DirectoryTree != "the tree as it was at the start" || agent.Context.Instructions != "the instructions as they were at the start" {
		t.Error("a turn rewrote what the instructions are built from")
	}
}

// The stream as a current server sends it: words as they are written, a
// marked cut-off call, then why it stopped and what it cost.
func TestParsingAStreamedTurn(t *testing.T) {
	stream := strings.Join([]string{
		`: keepalive`,
		`data: {"type":"text_delta","text":"Writing "}`,
		`data: {"type":"text_delta","text":"the model."}`,
		`data: {"type":"tool_use","id":"t1","name":"write_file","input":{"path":"post.go"},"incomplete":true}`,
		`data: {"type":"done","stop_reason":"max_tokens","usage":{"input_tokens":9000,"output_tokens":8192,"cache_read_tokens":8000}}`,
		`data: [DONE]`, ``,
	}, "\n\n")
	var seen []string
	resp, err := parseMessageResponse(&http.Response{
		Header: http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:   io.NopCloser(strings.NewReader(stream)),
	}, func(d string) { seen = append(seen, d) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(seen, "|") != "Writing |the model." || resp.TextContent() != "Writing the model." {
		t.Errorf("deltas=%v text=%q", seen, resp.TextContent())
	}
	calls := resp.ToolUseBlocks()
	if len(calls) != 1 || !calls[0].Incomplete || calls[0].Input["path"] != "post.go" {
		t.Errorf("calls = %+v", calls)
	}
	if resp.StopReason != "max_tokens" || resp.Usage == nil || resp.Usage.InputTokens != 9000 || resp.Usage.CacheReadTokens != 8000 {
		t.Errorf("stop=%q usage=%+v", resp.StopReason, resp.Usage)
	}

	_, err = parseMessageResponse(&http.Response{
		Header: http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:   io.NopCloser(strings.NewReader("data: {\"type\":\"error\",\"error\":\"AI provider returned an empty response\"}\n\n")),
	}, nil)
	if !errors.Is(err, ErrEmptyReply) {
		t.Errorf("an empty turn should be known as one: %v", err)
	}
}
