/*
|--------------------------------------------------------------------------
| AI SDK — Fake provider for tests
|--------------------------------------------------------------------------
|
| Test agents, tools and prompts without calling a model. The fake answers
| from a script and records every request, so a test can assert what was
| sent (system prompt, tools, messages) as well as what came back.
|
|   fake := ai.NewFake(
|       ai.FakeToolCall("lookup_order", `{"id":"42"}`),
|       ai.FakeText("Your order ships tomorrow."),
|   )
|   defer fake.Install()()          // becomes the global client
|
|   resp, _ := ai.NewAgent("…").WithTools("lookup_order").Prompt(ctx, "Where is order 42?")
|   fake.Requests()[1].Messages     // includes the tool result
|
*/

package ai

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// ErrFakeExhausted is returned when a fake has answered every scripted
// response.
var ErrFakeExhausted = errors.New("ai: fake provider has no more responses")

// FakeProvider is a Provider that answers from a script.
type FakeProvider struct {
	mu        sync.Mutex
	responses []*GenerateResponse
	requests  []GenerateRequest
	// StreamTools makes Stream report tool calls, like the OpenAI provider.
	// Without it the fake behaves like providers that do not.
	StreamTools bool
	// Repeat answers with the last response once the script runs out,
	// instead of failing with ErrFakeExhausted.
	Repeat bool
}

// NewFake returns a fake that answers with the given responses in order.
func NewFake(responses ...*GenerateResponse) *FakeProvider {
	return &FakeProvider{responses: responses}
}

// FakeText is a scripted text answer.
func FakeText(text string) *GenerateResponse {
	return &GenerateResponse{Text: text, Model: "fake", FinishReason: "stop",
		Usage: &Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2}}
}

// FakeToolCall is a scripted request to call one tool with JSON arguments.
func FakeToolCall(name, argsJSON string) *GenerateResponse {
	return FakeToolCalls(ToolCall{Name: name, Args: []byte(argsJSON)})
}

// FakeToolCalls is a scripted request to call several tools.
func FakeToolCalls(calls ...ToolCall) *GenerateResponse {
	for i := range calls {
		if calls[i].ID == "" {
			calls[i].ID = fmt.Sprintf("call_%d", i+1)
		}
	}
	return &GenerateResponse{ToolCalls: calls, Model: "fake", FinishReason: "tool_calls"}
}

// Name implements Provider.
func (f *FakeProvider) Name() string { return "fake" }

// Generate implements Provider.
func (f *FakeProvider) Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *req
	cp.Messages = append([]Message(nil), req.Messages...)
	cp.Tools = append([]ToolSpec(nil), req.Tools...)
	f.requests = append(f.requests, cp)
	if len(f.responses) == 0 {
		return nil, ErrFakeExhausted
	}
	resp := f.responses[0]
	if len(f.responses) > 1 || !f.Repeat {
		f.responses = f.responses[1:]
	}
	out := *resp
	out.ToolCalls = append([]ToolCall(nil), resp.ToolCalls...)
	return &out, nil
}

// Stream implements Provider: the scripted text arrives word by word.
func (f *FakeProvider) Stream(ctx context.Context, req *GenerateRequest) (*StreamResponse, error) {
	resp, err := f.Generate(ctx, req)
	if err != nil {
		return nil, err
	}
	chunks := make(chan StreamChunk, 64)
	errc := make(chan error, 1)
	go func() {
		defer close(chunks)
		for _, word := range strings.SplitAfter(resp.Text, " ") {
			if word == "" {
				continue
			}
			select {
			case chunks <- StreamChunk{Text: word}:
			case <-ctx.Done():
				errc <- ctx.Err()
				return
			}
		}
		if f.StreamTools && len(resp.ToolCalls) > 0 {
			chunks <- StreamChunk{ToolCalls: resp.ToolCalls}
		}
		chunks <- StreamChunk{Usage: resp.Usage, Done: true}
		errc <- nil
	}()
	return &StreamResponse{Chunks: chunks, Err: errc}, nil
}

// StreamsToolCalls implements ToolCallStreamer.
func (f *FakeProvider) StreamsToolCalls() bool { return f.StreamTools }

// Requests returns every request the fake received, in order.
func (f *FakeProvider) Requests() []GenerateRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]GenerateRequest(nil), f.requests...)
}

// Remaining returns how many scripted responses are left.
func (f *FakeProvider) Remaining() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.responses)
}

// Client returns a Client backed by the fake.
func (f *FakeProvider) Client() *Client {
	return &Client{provider: f, config: &Config{Provider: "fake", Model: "fake", MaxTokens: 1024}}
}

// Install makes the fake the global client (what NewAgent and the package
// helpers use) and returns a function that restores the previous one.
func (f *FakeProvider) Install() (restore func()) {
	clientMu.Lock()
	prev := globalClient
	globalClient = f.Client()
	clientMu.Unlock()
	return func() {
		clientMu.Lock()
		globalClient = prev
		clientMu.Unlock()
	}
}

var (
	_ Provider         = (*FakeProvider)(nil)
	_ ToolCallStreamer = (*FakeProvider)(nil)
)

// Embed implements EmbeddingProvider with deterministic bag-of-words
// vectors: texts that share words are close, so vector search behaves
// sensibly in tests without a model.
func (f *FakeProvider) Embed(_ context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error) {
	const dims = 64
	out := &EmbeddingResponse{Model: "fake-embedding"}
	for _, text := range req.Input {
		v := make([]float32, dims)
		for _, w := range strings.Fields(strings.ToLower(text)) {
			h := uint32(2166136261)
			for i := 0; i < len(w); i++ {
				h = (h ^ uint32(w[i])) * 16777619
			}
			v[h%dims]++
		}
		out.Embeddings = append(out.Embeddings, v)
	}
	return out, nil
}

var _ EmbeddingProvider = (*FakeProvider)(nil)
