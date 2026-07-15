package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// anthropicVersion is the required Anthropic API version header value.
const anthropicVersion = "2023-06-01"

// defaultAnthropicModel is used when no model is configured. Per Anthropic's
// guidance, default to the most capable Opus-tier model.
const defaultAnthropicModel = "claude-opus-4-8"

// defaultAnthropicMaxTokens is the fallback when a request sets no MaxTokens.
// Anthropic's Messages API requires max_tokens on every request.
const defaultAnthropicMaxTokens = 4096

var anthropicHTTPClient = &http.Client{Timeout: 120 * time.Second}

// anthropicBaseURL is the Messages API endpoint. It is a package var so tests
// can point the provider at a local server.
var anthropicBaseURL = "https://api.anthropic.com/v1/messages"

func newAnthropicProvider(cfg *Config) (Provider, error) {
	if cfg.AnthropicKey == "" {
		return nil, fmt.Errorf("ai: ANTHROPIC_API_KEY is required for Anthropic provider")
	}
	model := cfg.Model
	if model == "" {
		model = defaultAnthropicModel
	}
	return &anthropicProvider{apiKey: cfg.AnthropicKey, model: model}, nil
}

type anthropicProvider struct {
	apiKey string
	model  string
}

func (p *anthropicProvider) Name() string { return "anthropic" }

// ── Wire types (Messages API) ────────────────────────────────────

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Tools     []anthropicTool    `json:"tools,omitempty"`
	Stop      []string           `json:"stop_sequences,omitempty"`
	Stream    bool               `json:"stream,omitempty"`
	// NOTE: temperature/top_p/top_k are deliberately NOT forwarded. They are
	// rejected with a 400 on Opus 4.8/4.7 and Sonnet 5; steer behavior through
	// prompting instead.
}

type anthropicMessage struct {
	Role    string             `json:"role"`
	Content []anthropicContent `json:"content"`
}

type anthropicContent struct {
	Type   string                `json:"type"`
	Text   string                `json:"text,omitempty"`
	Source *anthropicImageSource `json:"source,omitempty"`
}

type anthropicImageSource struct {
	Type      string `json:"type"`       // "base64"
	MediaType string `json:"media_type"` // e.g. image/png
	Data      string `json:"data"`
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anthropicResponse struct {
	Content []struct {
		Type  string          `json:"type"`
		Text  string          `json:"text"`
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	} `json:"content"`
	Model      string `json:"model"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// ── Generate ─────────────────────────────────────────────────────

func (p *anthropicProvider) Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
	body := p.buildRequest(req, false)
	respBody, err := p.do(ctx, body)
	if err != nil {
		return nil, err
	}

	var ar anthropicResponse
	if err := json.Unmarshal(respBody, &ar); err != nil {
		return nil, fmt.Errorf("anthropic: failed to parse response: %w", err)
	}

	var text strings.Builder
	var toolCalls []ToolCall
	for _, block := range ar.Content {
		switch block.Type {
		case "text":
			text.WriteString(block.Text)
		case "tool_use":
			toolCalls = append(toolCalls, ToolCall{
				ID:   block.ID,
				Name: block.Name,
				Args: block.Input,
			})
		}
	}

	return &GenerateResponse{
		Text:         text.String(),
		ToolCalls:    toolCalls,
		Model:        ar.Model,
		FinishReason: ar.StopReason,
		Usage: &Usage{
			PromptTokens:     ar.Usage.InputTokens,
			CompletionTokens: ar.Usage.OutputTokens,
			TotalTokens:      ar.Usage.InputTokens + ar.Usage.OutputTokens,
		},
	}, nil
}

// do performs a non-streaming Messages API call and returns the raw body.
func (p *anthropicProvider) do(ctx context.Context, body *anthropicRequest) ([]byte, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", anthropicBaseURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	p.setHeaders(httpReq)

	resp, err := anthropicHTTPClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("anthropic error (%d): %s", resp.StatusCode, string(respBody))
	}
	return respBody, nil
}

// ── Stream ───────────────────────────────────────────────────────

// anthropicStreamEvent is a superset of the SSE event shapes we consume; unused
// fields for a given event type stay at their zero value.
type anthropicStreamEvent struct {
	Type  string `json:"type"`
	Delta struct {
		Type       string `json:"type"`
		Text       string `json:"text"`
		StopReason string `json:"stop_reason"`
	} `json:"delta"`
	Message struct {
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	} `json:"message"`
	Usage struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func (p *anthropicProvider) Stream(ctx context.Context, req *GenerateRequest) (*StreamResponse, error) {
	body := p.buildRequest(req, true)
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", anthropicBaseURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	p.setHeaders(httpReq)

	resp, err := anthropicHTTPClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic stream error (%d): %s", resp.StatusCode, string(respBody))
	}

	chunks := make(chan StreamChunk, 32)
	errCh := make(chan error, 1)

	go func() {
		defer resp.Body.Close()
		defer close(chunks)
		defer close(errCh)

		usage := &Usage{}
		scanner := bufio.NewScanner(resp.Body)
		// Allow long SSE data lines (default 64KiB can be exceeded by large blocks).
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "" {
				continue
			}

			var ev anthropicStreamEvent
			if err := json.Unmarshal([]byte(data), &ev); err != nil {
				errCh <- fmt.Errorf("anthropic: SSE parse error: %w", err)
				return
			}

			switch ev.Type {
			case "message_start":
				usage.PromptTokens = ev.Message.Usage.InputTokens
			case "content_block_delta":
				if ev.Delta.Type == "text_delta" && ev.Delta.Text != "" {
					select {
					case chunks <- StreamChunk{Text: ev.Delta.Text}:
					case <-ctx.Done():
						errCh <- ctx.Err()
						return
					}
				}
			case "message_delta":
				if ev.Usage.OutputTokens > 0 {
					usage.CompletionTokens = ev.Usage.OutputTokens
				}
			case "message_stop":
				usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
				select {
				case chunks <- StreamChunk{Usage: usage, Done: true}:
				case <-ctx.Done():
					errCh <- ctx.Err()
					return
				}
			}
		}

		if err := scanner.Err(); err != nil {
			errCh <- err
		}
	}()

	return &StreamResponse{Chunks: chunks, Err: errCh}, nil
}

// ── Shared request building ──────────────────────────────────────

func (p *anthropicProvider) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)
}

func (p *anthropicProvider) buildRequest(req *GenerateRequest, stream bool) *anthropicRequest {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultAnthropicMaxTokens
	}

	ar := &anthropicRequest{
		Model:     p.model,
		MaxTokens: maxTokens,
		Stop:      req.Stop,
		Stream:    stream,
	}

	for _, tool := range req.Tools {
		schema := tool.Parameters
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object"}`)
		}
		ar.Tools = append(ar.Tools, anthropicTool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: schema,
		})
	}

	// The Messages API takes the system prompt as a top-level field and only
	// user/assistant turns in messages. Hoist any system-role messages up and
	// map everything else to user/assistant.
	var systemParts []string
	if req.System != "" {
		systemParts = append(systemParts, req.System)
	}
	for _, msg := range req.Messages {
		if msg.Role == RoleSystem {
			if msg.Content != "" {
				systemParts = append(systemParts, msg.Content)
			}
			continue
		}
		role := "user"
		if msg.Role == RoleAssistant {
			role = "assistant"
		}
		content := []anthropicContent{}
		if msg.Content != "" {
			content = append(content, anthropicContent{Type: "text", Text: msg.Content})
		}
		content = append(content, anthropicImageBlocks(msg.Images)...)
		if len(content) == 0 {
			continue
		}
		ar.Messages = append(ar.Messages, anthropicMessage{Role: role, Content: content})
	}
	if len(systemParts) > 0 {
		ar.System = strings.Join(systemParts, "\n\n")
	}

	return ar
}

// anthropicImageBlocks reads local image paths and encodes them as base64
// image content blocks. Unreadable paths are skipped (matching the Gemini
// provider's best-effort behavior).
func anthropicImageBlocks(paths []string) []anthropicContent {
	var blocks []anthropicContent
	for _, imgPath := range paths {
		cleanPath := strings.TrimPrefix(imgPath, "/")
		data, err := os.ReadFile(cleanPath)
		if err != nil {
			continue
		}
		mediaType := "image/jpeg"
		switch strings.ToLower(filepath.Ext(cleanPath)) {
		case ".png":
			mediaType = "image/png"
		case ".gif":
			mediaType = "image/gif"
		case ".webp":
			mediaType = "image/webp"
		}
		blocks = append(blocks, anthropicContent{
			Type: "image",
			Source: &anthropicImageSource{
				Type:      "base64",
				MediaType: mediaType,
				Data:      base64.StdEncoding.EncodeToString(data),
			},
		})
	}
	return blocks
}
