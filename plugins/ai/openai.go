package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

// openAIProvider implements Provider using the OpenAI API.
type openAIProvider struct {
	client    *openai.Client
	model     string
	maxTokens int
}

func (p *openAIProvider) Name() string { return "openai" }

func newOpenAIProvider(cfg *Config) (*openAIProvider, error) {
	if cfg.OpenAIKey == "" {
		return nil, fmt.Errorf("ai: OPENAI_API_KEY is required for OpenAI provider")
	}
	openaiConfig := openai.DefaultConfig(cfg.OpenAIKey)
	if cfg.OpenAIBaseURL != "" {
		openaiConfig.BaseURL = cfg.OpenAIBaseURL
	}
	timeoutSec := 600
	if cfg.Timeout > 0 {
		timeoutSec = cfg.Timeout
	}
	openaiConfig.HTTPClient = &http.Client{
		Timeout: time.Duration(timeoutSec) * time.Second,
	}
	client := openai.NewClientWithConfig(openaiConfig)
	model := cfg.Model
	if model == "" {
		model = openai.GPT4o
	}
	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 8192
	}
	return &openAIProvider{client: client, model: model, maxTokens: maxTokens}, nil
}

func (p *openAIProvider) Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
	messages := p.toOpenAIMessages(req)
	model := req.Model
	if model == "" {
		model = p.model
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = p.maxTokens
	}

	var resp openai.ChatCompletionResponse
	var err error

	for attempt := 1; attempt <= 3; attempt++ {
		resp, err = p.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model:       model,
			Messages:    messages,
			MaxTokens:   maxTokens,
			Temperature: req.Temperature,
			Tools:       toOpenAITools(req.Tools),
			Stop:        req.Stop,
		})
		if err == nil && len(resp.Choices) > 0 {
			break
		}
		if err != nil {
			errMsg := strings.ToLower(err.Error())
			if attempt < 3 && (strings.Contains(errMsg, "502") || strings.Contains(errMsg, "503") || strings.Contains(errMsg, "504") || strings.Contains(errMsg, "429") || strings.Contains(errMsg, "bad gateway") || strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "deadline")) {
				time.Sleep(time.Duration(attempt) * 800 * time.Millisecond)
				continue
			}
			return nil, fmt.Errorf("ai: openai: %w", err)
		}
		// If err == nil but no choices, retry
		if attempt < 3 {
			time.Sleep(time.Duration(attempt) * 800 * time.Millisecond)
			continue
		}
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("ai: openai: no choices in response")
	}

	usage := &Usage{
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		TotalTokens:      resp.Usage.TotalTokens,
	}

	choice := resp.Choices[0]
	return &GenerateResponse{
		Text:         choice.Message.Content,
		ToolCalls:    fromOpenAIToolCalls(choice.Message.ToolCalls),
		Usage:        usage,
		Model:        resp.Model,
		FinishReason: string(choice.FinishReason),
	}, nil
}

func (p *openAIProvider) Stream(ctx context.Context, req *GenerateRequest) (*StreamResponse, error) {
	chunks := make(chan StreamChunk, 32)
	errCh := make(chan error, 1)

	go func() {
		defer close(chunks)
		defer close(errCh)

		messages := p.toOpenAIMessages(req)
		model := req.Model
		if model == "" {
			model = p.model
		}
		maxTokens := req.MaxTokens
		if maxTokens <= 0 {
			maxTokens = 1024
		}

		var stream *openai.ChatCompletionStream
		var err error
		for attempt := 1; attempt <= 3; attempt++ {
			stream, err = p.client.CreateChatCompletionStream(ctx, openai.ChatCompletionRequest{
				Model:       model,
				Messages:    messages,
				MaxTokens:   maxTokens,
				Temperature: req.Temperature,
				Tools:       toOpenAITools(req.Tools),
				Stop:        req.Stop,
				Stream:      true,
			})
			if err == nil {
				break
			}
			errMsg := strings.ToLower(err.Error())
			if attempt < 3 && (strings.Contains(errMsg, "502") || strings.Contains(errMsg, "503") || strings.Contains(errMsg, "504") || strings.Contains(errMsg, "429") || strings.Contains(errMsg, "bad gateway")) {
				time.Sleep(time.Duration(attempt) * 600 * time.Millisecond)
				continue
			}
			errCh <- fmt.Errorf("ai: openai stream: %w", err)
			return
		}
		defer stream.Close()

		// Tool-call arguments arrive as fragments spread over many deltas,
		// keyed by index. Accumulate them and emit the complete calls once
		// the stream ends so consumers never see partial JSON.
		pending := map[int]*ToolCall{}
		pendingArgs := map[int]*strings.Builder{}
		var order []int

		flushToolCalls := func() {
			if len(order) == 0 {
				return
			}
			var calls []ToolCall
			for _, idx := range order {
				tc := pending[idx]
				tc.Args = json.RawMessage(pendingArgs[idx].String())
				calls = append(calls, *tc)
			}
			chunks <- StreamChunk{ToolCalls: calls}
		}

		for {
			response, err := stream.Recv()
			if err == io.EOF {
				flushToolCalls()
				return
			}
			if err != nil {
				errCh <- fmt.Errorf("ai: openai stream recv: %w", err)
				return
			}
			if len(response.Choices) == 0 {
				continue
			}
			delta := response.Choices[0].Delta
			if delta.Content != "" {
				chunks <- StreamChunk{Text: delta.Content}
			}
			for _, tc := range delta.ToolCalls {
				idx := 0
				if tc.Index != nil {
					idx = *tc.Index
				}
				if _, ok := pending[idx]; !ok {
					pending[idx] = &ToolCall{ID: tc.ID, Name: tc.Function.Name}
					pendingArgs[idx] = &strings.Builder{}
					order = append(order, idx)
				}
				if tc.ID != "" {
					pending[idx].ID = tc.ID
				}
				if tc.Function.Name != "" {
					pending[idx].Name = tc.Function.Name
				}
				pendingArgs[idx].WriteString(tc.Function.Arguments)
			}
		}
	}()

	return &StreamResponse{Chunks: chunks, Err: errCh}, nil
}

func (p *openAIProvider) toOpenAIMessages(req *GenerateRequest) []openai.ChatCompletionMessage {
	var msgs []openai.ChatCompletionMessage
	if req.System != "" {
		msgs = append(msgs, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: req.System,
		})
	}
	for _, m := range req.Messages {
		role := m.Role
		if role == "" {
			role = openai.ChatMessageRoleUser
		}
		msg := openai.ChatCompletionMessage{
			Role:    role,
			Content: m.Content,
		}
		switch role {
		case RoleAssistant:
			for _, tc := range m.ToolCalls {
				args := string(tc.Args)
				if strings.TrimSpace(args) == "" {
					args = "{}"
				}
				msg.ToolCalls = append(msg.ToolCalls, openai.ToolCall{
					ID:   tc.ID,
					Type: openai.ToolTypeFunction,
					Function: openai.FunctionCall{
						Name:      tc.Name,
						Arguments: args,
					},
				})
			}
		case RoleTool:
			msg.Role = openai.ChatMessageRoleTool
			msg.ToolCallID = m.ToolCallID
		}
		msgs = append(msgs, msg)
	}
	return msgs
}

// toOpenAITools converts provider-neutral ToolSpecs into OpenAI function
// tool definitions. Returns nil when there are no tools so the field is
// omitted from the request entirely.
func toOpenAITools(specs []ToolSpec) []openai.Tool {
	if len(specs) == 0 {
		return nil
	}
	tools := make([]openai.Tool, 0, len(specs))
	for _, s := range specs {
		params := s.Parameters
		if len(params) == 0 {
			params = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		tools = append(tools, openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        s.Name,
				Description: s.Description,
				Parameters:  params,
			},
		})
	}
	return tools
}

func fromOpenAIToolCalls(calls []openai.ToolCall) []ToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]ToolCall, 0, len(calls))
	for _, tc := range calls {
		args := tc.Function.Arguments
		if strings.TrimSpace(args) == "" {
			args = "{}"
		}
		out = append(out, ToolCall{
			ID:   tc.ID,
			Name: tc.Function.Name,
			Args: json.RawMessage(args),
		})
	}
	return out
}
