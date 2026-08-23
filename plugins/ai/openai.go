package ai

import (
	"context"
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

	return &GenerateResponse{
		Text:  resp.Choices[0].Message.Content,
		Usage: usage,
		Model: resp.Model,
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

		for {
			response, err := stream.Recv()
			if err == io.EOF {
				return
			}
			if err != nil {
				errCh <- fmt.Errorf("ai: openai stream recv: %w", err)
				return
			}
			if len(response.Choices) > 0 && response.Choices[0].Delta.Content != "" {
				chunks <- StreamChunk{Text: response.Choices[0].Delta.Content}
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
		msgs = append(msgs, openai.ChatCompletionMessage{
			Role:    role,
			Content: m.Content,
		})
	}
	return msgs
}
