package ai

import (
	"context"
	"fmt"
	"io"

	openai "github.com/sashabaranov/go-openai"
)

// openAIProvider implements Provider using the OpenAI API.
type openAIProvider struct {
	client *openai.Client
	model  string
}

func (p *openAIProvider) Name() string { return "openai" }

func newOpenAIProvider(cfg *Config) (*openAIProvider, error) {
	if cfg.OpenAIKey == "" {
		return nil, fmt.Errorf("ai: OPENAI_API_KEY is required for OpenAI provider")
	}
	client := openai.NewClient(cfg.OpenAIKey)
	model := cfg.Model
	if model == "" {
		model = openai.GPT4o
	}
	return &openAIProvider{client: client, model: model}, nil
}

func (p *openAIProvider) Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
	messages := p.toOpenAIMessages(req)
	model := req.Model
	if model == "" {
		model = p.model
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}

	resp, err := p.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:       model,
		Messages:    messages,
		MaxTokens:   maxTokens,
		Temperature: req.Temperature,
	})
	if err != nil {
		return nil, fmt.Errorf("ai: openai: %w", err)
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

		stream, err := p.client.CreateChatCompletionStream(ctx, openai.ChatCompletionRequest{
			Model:       model,
			Messages:    messages,
			MaxTokens:   maxTokens,
			Temperature: req.Temperature,
			Stream:      true,
		})
		if err != nil {
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
