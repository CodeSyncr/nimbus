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

var geminiHTTPClient = &http.Client{Timeout: 120 * time.Second}

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
	}, nil
}

type geminiProvider struct {
	apiKey string
	model  string
	// imageModel is the default for image generation, independent of the text
	// model: pictures come from a different endpoint and usually a different
	// model. See gemini_image.go.
	imageModel string
	// baseURL overrides the API root (tests point it at a local server).
	baseURL string
}

func (p *geminiProvider) Name() string { return "gemini" }

type geminiRequest struct {
	Contents          []geminiContent        `json:"contents"`
	GenerationConfig  geminiGenerationConfig `json:"generationConfig,omitempty"`
	SystemInstruction *geminiContent         `json:"system_instruction,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text       string            `json:"text,omitempty"`
	InlineData *geminiInlineData `json:"inlineData,omitempty"`
}

type geminiInlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type geminiGenerationConfig struct {
	MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
	Temperature     float32 `json:"temperature,omitempty"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}

func (p *geminiProvider) Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
	apiURL := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent", p.model)

	body := p.buildRequest(req)
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", p.apiKey)

	resp, err := geminiHTTPClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("gemini error (%d): %s", resp.StatusCode, string(respBody))
	}

	var geminiResp geminiResponse
	if err := json.Unmarshal(respBody, &geminiResp); err != nil {
		return nil, fmt.Errorf("gemini: failed to parse response: %w", err)
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("gemini: empty response (no candidates)")
	}

	return &GenerateResponse{
		Text:  geminiResp.Candidates[0].Content.Parts[0].Text,
		Model: p.model,
		Usage: &Usage{
			PromptTokens:     geminiResp.UsageMetadata.PromptTokenCount,
			CompletionTokens: geminiResp.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      geminiResp.UsageMetadata.TotalTokenCount,
		},
	}, nil
}

func (p *geminiProvider) Stream(ctx context.Context, req *GenerateRequest) (*StreamResponse, error) {
	apiURL := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:streamGenerateContent?alt=sse", p.model)

	body := p.buildRequest(req)
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", p.apiKey)

	resp, err := geminiHTTPClient.Do(httpReq)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gemini stream error (%d): %s", resp.StatusCode, string(respBody))
	}

	chunks := make(chan StreamChunk, 32)
	errCh := make(chan error, 1)

	go func() {
		defer resp.Body.Close()
		defer close(chunks)
		defer close(errCh)

		var totalUsage *Usage
		scanner := bufio.NewScanner(resp.Body)
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

			if len(chunk.Candidates) > 0 && len(chunk.Candidates[0].Content.Parts) > 0 {
				text := chunk.Candidates[0].Content.Parts[0].Text
				done := chunk.Candidates[0].FinishReason == "STOP"

				if chunk.UsageMetadata.TotalTokenCount > 0 {
					totalUsage = &Usage{
						PromptTokens:     chunk.UsageMetadata.PromptTokenCount,
						CompletionTokens: chunk.UsageMetadata.CandidatesTokenCount,
						TotalTokens:      chunk.UsageMetadata.TotalTokenCount,
					}
				}

				select {
				case chunks <- StreamChunk{Text: text, Usage: totalUsage, Done: done}:
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

func (p *geminiProvider) buildRequest(req *GenerateRequest) *geminiRequest {
	gr := &geminiRequest{
		GenerationConfig: geminiGenerationConfig{
			MaxOutputTokens: req.MaxTokens,
			Temperature:     req.Temperature,
		},
	}

	if req.System != "" {
		gr.SystemInstruction = &geminiContent{
			Parts: []geminiPart{{Text: req.System}},
		}
	}

	for _, msg := range req.Messages {
		role := "user"
		if msg.Role == RoleAssistant {
			role = "model"
		}
		parts := []geminiPart{{Text: msg.Content}}
		for _, imgPath := range msg.Images {
			cleanPath := strings.TrimPrefix(imgPath, "/")
			data, err := os.ReadFile(cleanPath)
			if err != nil {
				continue
			}
			mimeType := "image/jpeg"
			ext := strings.ToLower(filepath.Ext(cleanPath))
			switch ext {
			case ".png":
				mimeType = "image/png"
			case ".gif":
				mimeType = "image/gif"
			case ".webp":
				mimeType = "image/webp"
			}
			base64Data := base64.StdEncoding.EncodeToString(data)
			parts = append(parts, geminiPart{
				InlineData: &geminiInlineData{
					MimeType: mimeType,
					Data:     base64Data,
				},
			})
		}
		gr.Contents = append(gr.Contents, geminiContent{
			Role:  role,
			Parts: parts,
		})
	}

	return gr
}
