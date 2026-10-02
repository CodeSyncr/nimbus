package ai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

// openAIProvider implements Provider using the OpenAI API.
type openAIProvider struct {
	client     *openai.Client
	httpClient *http.Client
	model      string
	maxTokens  int
	imageModel string
}

// maxDroppedAttempts is how often a request the server hung up on is sent,
// in all. Other retryable failures keep three.
const maxDroppedAttempts = 5

// retryAfterDrop decides whether a failed attempt goes again, and clears
// the connection pool when the server hung up: the next attempt otherwise
// reuses the dead connection and fails at once, burning a try.
func (p *openAIProvider) retryAfterDrop(ctx context.Context, err error, attempt int, transient bool) bool {
	if ctx.Err() != nil {
		return false
	}
	if droppedConnection(err) && attempt < maxDroppedAttempts {
		if p.httpClient != nil {
			p.httpClient.CloseIdleConnections()
		}
		time.Sleep(time.Duration(attempt) * 700 * time.Millisecond)
		return true
	}
	if transient && attempt < 3 {
		time.Sleep(time.Duration(attempt) * 600 * time.Millisecond)
		return true
	}
	return false
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
	// Its own pool, so clearing it after a dropped connection touches no
	// other client in the process.
	httpClient := &http.Client{
		Timeout:   time.Duration(timeoutSec) * time.Second,
		Transport: http.DefaultTransport.(*http.Transport).Clone(),
	}
	openaiConfig.HTTPClient = httpClient
	client := openai.NewClientWithConfig(openaiConfig)
	model := cfg.Model
	if model == "" {
		model = openai.GPT4o
	}
	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 8192
	}
	return &openAIProvider{client: client, httpClient: httpClient, model: model, maxTokens: maxTokens, imageModel: cfg.ImageModel}, nil
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

	for attempt := 1; attempt <= maxDroppedAttempts; attempt++ {
		resp, err = p.client.CreateChatCompletion(ctx, openAIChatRequest(req, model, messages, maxTokens, false))
		if err == nil && len(resp.Choices) > 0 {
			break
		}
		if err != nil {
			errMsg := strings.ToLower(err.Error())
			transient := strings.Contains(errMsg, "502") || strings.Contains(errMsg, "503") || strings.Contains(errMsg, "504") || strings.Contains(errMsg, "429") || strings.Contains(errMsg, "bad gateway") || strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "deadline")
			if p.retryAfterDrop(ctx, err, attempt, transient) {
				continue
			}
			return nil, fmt.Errorf("ai: openai: %w", err)
		}
		// If err == nil but no choices, retry (three tries in all)
		if attempt < 3 {
			time.Sleep(time.Duration(attempt) * 800 * time.Millisecond)
			continue
		}
		break
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("ai: openai: no choices in response")
	}

	usage := &Usage{
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		TotalTokens:      resp.Usage.TotalTokens,
	}
	if d := resp.Usage.PromptTokensDetails; d != nil {
		usage.CacheReadTokens = d.CachedTokens
	}
	if d := resp.Usage.CompletionTokensDetails; d != nil {
		usage.ReasoningTokens = d.ReasoningTokens
	}

	choice := resp.Choices[0]
	return &GenerateResponse{
		Reasoning:    choice.Message.ReasoningContent, // DeepSeek-style compatible APIs
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
		for attempt := 1; attempt <= maxDroppedAttempts; attempt++ {
			stream, err = p.client.CreateChatCompletionStream(ctx, openAIChatRequest(req, model, messages, maxTokens, true))
			if err == nil {
				break
			}
			errMsg := strings.ToLower(err.Error())
			transient := strings.Contains(errMsg, "502") || strings.Contains(errMsg, "503") || strings.Contains(errMsg, "504") || strings.Contains(errMsg, "429") || strings.Contains(errMsg, "bad gateway")
			if p.retryAfterDrop(ctx, err, attempt, transient) {
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
		content := m.Content
		images := m.Images
		if len(m.Files) > 0 {
			// Chat Completions takes no documents through this client:
			// PDFs and text go in as their text, images as image parts.
			content = contentWithDocuments(m)
			images = imageRefs(m)
		}
		msg := openai.ChatCompletionMessage{
			Role:    role,
			Content: content,
		}
		// A user turn with pictures goes as parts: the text, then each
		// image, which is how vision models on OpenAI-compatible APIs
		// receive them.
		if len(images) > 0 && (role == RoleUser || role == openai.ChatMessageRoleUser) {
			var parts []openai.ChatMessagePart
			if strings.TrimSpace(content) != "" {
				parts = append(parts, openai.ChatMessagePart{Type: openai.ChatMessagePartTypeText, Text: content})
			}
			for _, ref := range images {
				if u := openAIImageURL(ref); u != "" {
					parts = append(parts, openai.ChatMessagePart{Type: openai.ChatMessagePartTypeImageURL, ImageURL: &openai.ChatMessageImageURL{URL: u, Detail: openai.ImageURLDetailAuto}})
				}
			}
			if len(parts) > 0 {
				msg.Content = ""
				msg.MultiContent = parts
			}
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

// openAIImageURL turns an image reference into what the image_url part
// takes: data URIs and http(s) URLs pass through, a readable file becomes a
// data URI. Anything else answers "".
func openAIImageURL(ref string) string {
	ref = strings.TrimSpace(ref)
	if strings.HasPrefix(ref, "data:image/") || strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "http://") {
		return ref
	}
	data, err := os.ReadFile(strings.TrimPrefix(ref, "/"))
	if err != nil {
		if data, err = os.ReadFile(ref); err != nil {
			return ""
		}
	}
	mediaType := http.DetectContentType(data)
	if !strings.HasPrefix(mediaType, "image/") {
		return ""
	}
	return "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// GenerateImage draws through the OpenAI images endpoint, which
// OpenAI-compatible gateways (Agnes, OpenRouter-style routers) also serve.
// The model is the caller's, then AI_IMAGE_MODEL, then dall-e-3. Pictures
// come back as base64 so nothing depends on a URL that expires.
func (p *openAIProvider) GenerateImage(ctx context.Context, req *ImageRequest) (*ImageResponse, error) {
	if req == nil || strings.TrimSpace(req.Prompt) == "" {
		return nil, fmt.Errorf("ai: image prompt is required")
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = strings.TrimSpace(p.imageModel)
	}
	if model == "" {
		model = openai.CreateImageModelDallE3
	}
	n := req.N
	if n <= 0 {
		n = 1
	}
	size := strings.TrimSpace(req.Size)
	if size == "" {
		size = openai.CreateImageSize1024x1024
	}
	res, err := p.client.CreateImage(ctx, openai.ImageRequest{
		Prompt:         req.Prompt,
		Model:          model,
		N:              n,
		Size:           size,
		Style:          req.Style,
		ResponseFormat: openai.CreateImageResponseFormatB64JSON,
	})
	if err != nil {
		return nil, err
	}
	out := &ImageResponse{Model: model}
	for _, d := range res.Data {
		if d.B64JSON == "" && d.URL == "" {
			continue
		}
		out.Images = append(out.Images, ImageData{URL: d.URL, B64JSON: d.B64JSON})
	}
	if len(out.Images) == 0 {
		return nil, fmt.Errorf("ai: %s returned no image", model)
	}
	return out, nil
}

// droppedConnection is a request the server hung up on before answering:
// "unexpected EOF", a reset, a closed idle connection. Nothing came back,
// so nothing reached the user, and sending the request again is safe.
// Seen from DeepSeek under load: the connection held for 15-30 seconds,
// then closed without a response.
func droppedConnection(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, s := range []string{"unexpected eof", "connection reset", "broken pipe", "server closed idle connection", "http2: server sent goaway", "timeout awaiting response headers"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return strings.HasSuffix(msg, ": eof")
}

// StreamsToolCalls implements ToolCallStreamer: the OpenAI stream (and the
// OpenAI-compatible ones built on this provider) reports tool calls.
func (p *openAIProvider) StreamsToolCalls() bool { return true }

// openAIChatRequest builds a chat completion request. Reasoning models
// take max_completion_tokens (thinking counts against it) and
// reasoning_effort, and reject temperature.
func openAIChatRequest(req *GenerateRequest, model string, messages []openai.ChatCompletionMessage, maxTokens int, stream bool) openai.ChatCompletionRequest {
	r := openai.ChatCompletionRequest{
		Model:       model,
		Messages:    messages,
		MaxTokens:   maxTokens,
		Temperature: req.Temperature,
		Tools:       toOpenAITools(req.Tools),
		Stop:        req.Stop,
		Stream:      stream,
	}
	switch c := req.ToolChoice; {
	case len(r.Tools) == 0, c == "":
	case c == ToolChoiceAuto || c == ToolChoiceNone || c == ToolChoiceRequired:
		r.ToolChoice = c
	default:
		r.ToolChoice = openai.ToolChoice{Type: openai.ToolTypeFunction, Function: openai.ToolFunction{Name: c}}
	}
	if req.Reasoning != nil {
		r.ReasoningEffort = req.Reasoning.effort()
		r.MaxCompletionTokens = maxTokens
		r.MaxTokens = 0
		r.Temperature = 0
	}
	return r
}
