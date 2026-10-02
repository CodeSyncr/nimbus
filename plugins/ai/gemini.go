package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

var geminiHTTPClient = &http.Client{Timeout: 120 * time.Second}

// geminiClientFor returns a client honouring the configured timeout. A
// fixed 120s ceiling silently truncated long streamed answers regardless
// of AI_TIMEOUT.
func geminiClientFor(cfg *Config) *http.Client {
	if cfg != nil && cfg.Timeout > 0 {
		return &http.Client{Timeout: time.Duration(cfg.Timeout) * time.Second}
	}
	return geminiHTTPClient
}

func newGeminiProvider(cfg *Config) (Provider, error) {
	if cfg.GeminiKey == "" {
		return nil, fmt.Errorf("ai: GEMINI_API_KEY is required for Gemini provider")
	}
	model := cfg.Model
	if model == "" {
		model = "gemini-2.0-flash"
	}
	return &geminiProvider{
		apiKey:     cfg.GeminiKey,
		model:      model,
		imageModel: cfg.ImageModel,
		http:       geminiClientFor(cfg),
	}, nil
}

type geminiProvider struct {
	apiKey string
	model  string
	// http carries the configured timeout; nil falls back to the package client.
	http *http.Client
	// imageModel is the default for image generation, independent of the text
	// model: pictures come from a different endpoint and usually a different
	// model. See gemini_image.go.
	imageModel string
	// baseURL overrides the API root (tests point it at a local server).
	baseURL string
}

// client returns the provider's HTTP client, falling back to the package
// default for providers built before the timeout was configurable.
func (p *geminiProvider) client() *http.Client {
	if p.http != nil {
		return p.http
	}
	return geminiHTTPClient
}

func (p *geminiProvider) Name() string { return "gemini" }

type geminiRequest struct {
	Contents          []geminiContent        `json:"contents"`
	GenerationConfig  geminiGenerationConfig `json:"generationConfig,omitempty"`
	SystemInstruction *geminiContent         `json:"system_instruction,omitempty"`
	Tools             []geminiTool           `json:"tools,omitempty"`
	ToolConfig        *geminiToolConfig      `json:"toolConfig,omitempty"`
}

type geminiToolConfig struct {
	FunctionCallingConfig struct {
		Mode                 string   `json:"mode"` // AUTO, ANY, NONE
		AllowedFunctionNames []string `json:"allowedFunctionNames,omitempty"`
	} `json:"functionCallingConfig"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text             string                  `json:"text,omitempty"`
	Thought          bool                    `json:"thought,omitempty"`
	ThoughtSignature string                  `json:"thoughtSignature,omitempty"`
	InlineData       *geminiInlineData       `json:"inlineData,omitempty"`
	FunctionCall     *geminiFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *geminiFunctionResponse `json:"functionResponse,omitempty"`
}

type geminiInlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type geminiFunctionCall struct {
	ID   string          `json:"id,omitempty"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

type geminiFunctionResponse struct {
	ID       string         `json:"id,omitempty"`
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type geminiTool struct {
	FunctionDeclarations []geminiFunctionDecl `json:"functionDeclarations"`
}

type geminiFunctionDecl struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type geminiGenerationConfig struct {
	MaxOutputTokens  int                   `json:"maxOutputTokens,omitempty"`
	Temperature      float32               `json:"temperature,omitempty"`
	ThinkingConfig   *geminiThinkingConfig `json:"thinkingConfig,omitempty"`
	ResponseMimeType string                `json:"responseMimeType,omitempty"`
	ResponseSchema   json.RawMessage       `json:"responseJsonSchema,omitempty"`
}

type geminiThinkingConfig struct {
	ThinkingBudget  int  `json:"thinkingBudget"`
	IncludeThoughts bool `json:"includeThoughts"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []geminiPart `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount        int `json:"promptTokenCount"`
		CandidatesTokenCount    int `json:"candidatesTokenCount"`
		TotalTokenCount         int `json:"totalTokenCount"`
		CachedContentTokenCount int `json:"cachedContentTokenCount"`
		ThoughtsTokenCount      int `json:"thoughtsTokenCount"`
	} `json:"usageMetadata"`
	ModelVersion string `json:"modelVersion"`
}

// usage converts Gemini's counts. Thinking is billed as output, so it is
// part of CompletionTokens (Gemini reports it separately).
func (r *geminiResponse) usage() *Usage {
	m := r.UsageMetadata
	if m.TotalTokenCount == 0 {
		return nil
	}
	return &Usage{
		PromptTokens:     m.PromptTokenCount,
		CompletionTokens: m.CandidatesTokenCount + m.ThoughtsTokenCount,
		TotalTokens:      m.TotalTokenCount,
		CacheReadTokens:  m.CachedContentTokenCount,
		ReasoningTokens:  m.ThoughtsTokenCount,
	}
}

// geminiTurn accumulates the parts of one model turn (all of a response,
// or every chunk of a stream).
type geminiTurn struct {
	text, thinking strings.Builder
	calls          []ToolCall
	reasoning      []ReasoningBlock
	finish         string
}

// add folds parts in and returns the visible text they carried.
func (t *geminiTurn) add(parts []geminiPart) string {
	var visible strings.Builder
	for _, part := range parts {
		switch {
		case part.FunctionCall != nil:
			id := part.FunctionCall.ID
			if id == "" {
				id = fmt.Sprintf("gemini_call_%d", len(t.calls)+1)
			}
			args := part.FunctionCall.Args
			if len(args) == 0 {
				args = json.RawMessage("{}")
			}
			t.calls = append(t.calls, ToolCall{ID: id, Name: part.FunctionCall.Name, Args: args, Signature: part.ThoughtSignature})
		case part.Thought:
			t.thinking.WriteString(part.Text)
			t.reasoning = append(t.reasoning, ReasoningBlock{Text: part.Text, Signature: part.ThoughtSignature})
		case part.Text != "":
			t.text.WriteString(part.Text)
			visible.WriteString(part.Text)
		}
	}
	return visible.String()
}

func (t *geminiTurn) response(model string, u *Usage) *GenerateResponse {
	finish := t.finish
	if len(t.calls) > 0 {
		finish = "tool_calls"
	}
	return &GenerateResponse{
		Text:            t.text.String(),
		ToolCalls:       t.calls,
		Reasoning:       t.thinking.String(),
		ReasoningBlocks: t.reasoning,
		Model:           model,
		FinishReason:    strings.ToLower(finish),
		Usage:           u,
	}
}

func (p *geminiProvider) modelFor(req *GenerateRequest) string {
	if req.Model != "" {
		return req.Model
	}
	return p.model
}

func (p *geminiProvider) post(ctx context.Context, url string, req *GenerateRequest) (*http.Response, error) {
	jsonBody, err := json.Marshal(p.buildRequest(req))
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", p.apiKey)
	resp, err := p.client().Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, newAPIError("gemini", resp)
	}
	return resp, nil
}

func (p *geminiProvider) Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
	model := p.modelFor(req)
	resp, err := p.post(ctx, p.imageBaseURL()+"/models/"+model+":generateContent", req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var gr geminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&gr); err != nil {
		return nil, fmt.Errorf("gemini: failed to parse response: %w", err)
	}
	if len(gr.Candidates) == 0 {
		return nil, fmt.Errorf("gemini: empty response (no candidates)")
	}
	var turn geminiTurn
	turn.add(gr.Candidates[0].Content.Parts)
	turn.finish = gr.Candidates[0].FinishReason
	if gr.ModelVersion != "" {
		model = gr.ModelVersion
	}
	return turn.response(model, gr.usage()), nil
}

func (p *geminiProvider) Stream(ctx context.Context, req *GenerateRequest) (*StreamResponse, error) {
	resp, err := p.post(ctx, p.imageBaseURL()+"/models/"+p.modelFor(req)+":streamGenerateContent?alt=sse", req)
	if err != nil {
		return nil, err
	}

	chunks := make(chan StreamChunk, 32)
	errCh := make(chan error, 1)

	go func() {
		defer resp.Body.Close()
		defer close(chunks)
		defer close(errCh)

		var (
			turn  geminiTurn
			usage *Usage
		)
		send := func(c StreamChunk) bool {
			select {
			case chunks <- c:
				return true
			case <-ctx.Done():
				errCh <- ctx.Err()
				return false
			}
		}
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "" {
				continue
			}
			var chunk geminiResponse
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				errCh <- fmt.Errorf("gemini: SSE parse error: %w", err)
				return
			}
			if u := chunk.usage(); u != nil {
				usage = u
			}
			if len(chunk.Candidates) == 0 {
				continue
			}
			before := len(turn.calls)
			if text := turn.add(chunk.Candidates[0].Content.Parts); text != "" {
				if !send(StreamChunk{Text: text}) {
					return
				}
			}
			if len(turn.calls) > before {
				if !send(StreamChunk{ToolCalls: turn.calls[before:]}) {
					return
				}
			}
		}
		if err := scanner.Err(); err != nil {
			errCh <- err
			return
		}
		send(StreamChunk{Usage: usage, Done: true})
	}()

	return &StreamResponse{Chunks: chunks, Err: errCh}, nil
}

// StreamsToolCalls implements ToolCallStreamer: function calls arrive as
// parts of the stream.
func (p *geminiProvider) StreamsToolCalls() bool { return true }

func (p *geminiProvider) buildRequest(req *GenerateRequest) *geminiRequest {
	gr := &geminiRequest{
		GenerationConfig: geminiGenerationConfig{
			MaxOutputTokens: req.MaxTokens,
			Temperature:     req.Temperature,
		},
	}
	if req.Reasoning != nil {
		gr.GenerationConfig.ThinkingConfig = &geminiThinkingConfig{
			ThinkingBudget:  req.Reasoning.budget(),
			IncludeThoughts: true,
		}
	}
	if len(req.Schema) > 0 {
		gr.GenerationConfig.ResponseMimeType = "application/json"
		gr.GenerationConfig.ResponseSchema = req.Schema
	}
	if len(req.Tools) > 0 {
		decls := make([]geminiFunctionDecl, 0, len(req.Tools))
		for _, t := range req.Tools {
			decls = append(decls, geminiFunctionDecl{Name: t.Name, Description: t.Description, Parameters: geminiSchema(t.Parameters)})
		}
		gr.Tools = []geminiTool{{FunctionDeclarations: decls}}
		if c := req.ToolChoice; c != "" && c != ToolChoiceAuto {
			tc := &geminiToolConfig{}
			switch c {
			case ToolChoiceNone:
				tc.FunctionCallingConfig.Mode = "NONE"
			case ToolChoiceRequired:
				tc.FunctionCallingConfig.Mode = "ANY"
			default:
				tc.FunctionCallingConfig.Mode = "ANY"
				tc.FunctionCallingConfig.AllowedFunctionNames = []string{c}
			}
			gr.ToolConfig = tc
		}
	}

	system := []string{}
	if req.System != "" {
		system = append(system, req.System)
	}
	// Gemini's function responses are matched by name: remember the name
	// behind each call id.
	callNames := map[string]string{}
	for _, msg := range req.Messages {
		var role string
		var parts []geminiPart
		switch msg.Role {
		case RoleSystem:
			if msg.Content != "" {
				system = append(system, msg.Content)
			}
			continue
		case RoleAssistant:
			role = "model"
			for _, rb := range msg.Reasoning {
				if rb.Signature != "" {
					parts = append(parts, geminiPart{Text: rb.Text, Thought: true, ThoughtSignature: rb.Signature})
				}
			}
			if msg.Content != "" {
				parts = append(parts, geminiPart{Text: msg.Content})
			}
			for _, tc := range msg.ToolCalls {
				callNames[tc.ID] = tc.Name
				args := tc.Args
				if len(strings.TrimSpace(string(args))) == 0 {
					args = json.RawMessage("{}")
				}
				parts = append(parts, geminiPart{
					FunctionCall:     &geminiFunctionCall{Name: tc.Name, Args: args},
					ThoughtSignature: tc.Signature,
				})
			}
		case RoleTool:
			role = "user"
			parts = append(parts, geminiPart{FunctionResponse: &geminiFunctionResponse{
				Name:     callNames[msg.ToolCallID],
				Response: geminiToolResult(msg.Content),
			}})
		default:
			role = "user"
			if msg.Content != "" {
				parts = append(parts, geminiPart{Text: msg.Content})
			}
			for _, att := range loadAttachments(append(append([]string(nil), msg.Images...), msg.Files...)) {
				parts = append(parts, geminiPart{InlineData: &geminiInlineData{MimeType: att.MediaType, Data: att.Base64()}})
			}
		}
		if len(parts) == 0 {
			continue
		}
		// Consecutive turns of the same role (several tool results) merge.
		if n := len(gr.Contents); n > 0 && gr.Contents[n-1].Role == role {
			gr.Contents[n-1].Parts = append(gr.Contents[n-1].Parts, parts...)
			continue
		}
		gr.Contents = append(gr.Contents, geminiContent{Role: role, Parts: parts})
	}
	if len(system) > 0 {
		gr.SystemInstruction = &geminiContent{Parts: []geminiPart{{Text: strings.Join(system, "\n\n")}}}
	}
	return gr
}

// geminiToolResult wraps a tool's output in the object Gemini expects.
func geminiToolResult(content string) map[string]any {
	var v any
	if err := json.Unmarshal([]byte(content), &v); err == nil {
		if m, ok := v.(map[string]any); ok {
			return m
		}
		return map[string]any{"result": v}
	}
	if strings.HasPrefix(content, "Error:") {
		return map[string]any{"error": strings.TrimSpace(strings.TrimPrefix(content, "Error:"))}
	}
	return map[string]any{"result": content}
}

// geminiSchema drops JSON Schema keywords Gemini's function declarations
// reject ($schema, additionalProperties, $defs…), recursively.
func geminiSchema(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw
	}
	var clean func(any) any
	clean = func(x any) any {
		switch t := x.(type) {
		case map[string]any:
			out := map[string]any{}
			for k, val := range t {
				switch k {
				case "$schema", "additionalProperties", "$defs", "definitions", "$id", "$ref", "examples":
					continue
				}
				out[k] = clean(val)
			}
			return out
		case []any:
			for i := range t {
				t[i] = clean(t[i])
			}
			return t
		}
		return x
	}
	b, err := json.Marshal(clean(v))
	if err != nil {
		return raw
	}
	return b
}
