package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

/*
Gemini image generation.

Google exposes two different shapes for making a picture, and which one applies
depends on the model:

  Imagen  (imagen-*)  POST …/models/<model>:predict
                      instances[].prompt + parameters.sampleCount
                      → predictions[].bytesBase64Encoded

  Gemini  (gemini-*)  POST …/models/<model>:generateContent with
                      responseModalities ["TEXT","IMAGE"]
                      → candidates[].content.parts[].inlineData.data

Both are implemented and picked by model name, so switching between them is a
config change rather than a code change.

The image model is independent of the text model: reasoning and pictures come
from different endpoints and are usually different models, so
ai.Image().Model(…) → AI_IMAGE_MODEL → the provider default, in that order.
*/

// Compile-time proof that the provider satisfies the interface. Without this,
// a missing method is only discovered at runtime, as "provider does not
// support image generation" — which is exactly how image generation came to be
// unimplemented while the builder advertised it.
var _ ImageProvider = (*geminiProvider)(nil)

// defaultGeminiImageModel is used when neither the caller nor the config names
// one. Google's image model names change with each generation; this is a
// starting point, not a constraint — set AI_IMAGE_MODEL to move.
const defaultGeminiImageModel = "imagen-3.0-generate-002"

// GenerateImage implements ImageProvider.
func (p *geminiProvider) GenerateImage(ctx context.Context, req *ImageRequest) (*ImageResponse, error) {
	if req == nil || strings.TrimSpace(req.Prompt) == "" {
		return nil, fmt.Errorf("ai: image prompt is required")
	}

	model := firstNonBlank(req.Model, p.imageModel, defaultGeminiImageModel)
	if strings.HasPrefix(strings.ToLower(model), "imagen") {
		return p.generateWithImagen(ctx, model, req)
	}
	return p.generateWithGeminiImage(ctx, model, req)
}

// generateWithImagen drives the Imagen predict endpoint.
func (p *geminiProvider) generateWithImagen(ctx context.Context, model string, req *ImageRequest) (*ImageResponse, error) {
	count := req.N
	if count <= 0 {
		count = 1
	}

	params := map[string]any{"sampleCount": count}
	if ratio := aspectRatioFor(req.Size); ratio != "" {
		params["aspectRatio"] = ratio
	}
	body := map[string]any{
		"instances":  []map[string]any{{"prompt": req.Prompt}},
		"parameters": params,
	}

	var out struct {
		Predictions []struct {
			BytesBase64Encoded string `json:"bytesBase64Encoded"`
			MimeType           string `json:"mimeType"`
		} `json:"predictions"`
	}
	if err := p.postImage(ctx, model, "predict", body, &out); err != nil {
		return nil, err
	}

	images := make([]ImageData, 0, len(out.Predictions))
	for _, pred := range out.Predictions {
		if pred.BytesBase64Encoded != "" {
			images = append(images, ImageData{B64JSON: pred.BytesBase64Encoded})
		}
	}
	if len(images) == 0 {
		return nil, fmt.Errorf("ai/gemini: %s returned no image data", model)
	}
	return &ImageResponse{Images: images, Model: model}, nil
}

// generateWithGeminiImage drives a Gemini model that returns inline images.
func (p *geminiProvider) generateWithGeminiImage(ctx context.Context, model string, req *ImageRequest) (*ImageResponse, error) {
	body := map[string]any{
		"contents": []map[string]any{{
			"role":  "user",
			"parts": []map[string]any{{"text": req.Prompt}},
		}},
		"generationConfig": map[string]any{
			// Without this the model answers with prose about the picture
			// instead of the picture.
			"responseModalities": []string{"TEXT", "IMAGE"},
		},
	}

	var out struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					InlineData *struct {
						MimeType string `json:"mimeType"`
						Data     string `json:"data"`
					} `json:"inlineData"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := p.postImage(ctx, model, "generateContent", body, &out); err != nil {
		return nil, err
	}

	var images []ImageData
	for _, cand := range out.Candidates {
		for _, part := range cand.Content.Parts {
			if part.InlineData != nil && part.InlineData.Data != "" {
				images = append(images, ImageData{B64JSON: part.InlineData.Data})
			}
		}
	}
	if len(images) == 0 {
		return nil, fmt.Errorf("ai/gemini: %s returned no image data (is it an image-capable model?)", model)
	}
	return &ImageResponse{Images: images, Model: model}, nil
}

// postImage sends one request to a model endpoint and decodes the result.
func (p *geminiProvider) postImage(ctx context.Context, model, action string, body, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/models/%s:%s", p.imageBaseURL(), model, action)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", p.apiKey)

	resp, err := geminiHTTPClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("ai/gemini: image request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ai/gemini: image generation failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("ai/gemini: could not decode the image response: %w", err)
	}
	return nil
}

// imageBaseURL is the API root, overridable so tests can point elsewhere.
func (p *geminiProvider) imageBaseURL() string {
	if p.baseURL != "" {
		return strings.TrimRight(p.baseURL, "/")
	}
	return "https://generativelanguage.googleapis.com/v1beta"
}

// aspectRatioFor maps a pixel size onto the ratio Imagen expects. Imagen picks
// resolution itself and takes a ratio instead, so "1024x1024" becomes "1:1".
func aspectRatioFor(size string) string {
	switch strings.TrimSpace(strings.ToLower(size)) {
	case "", "1024x1024", "512x512", "256x256":
		return "1:1"
	case "1792x1024", "1536x1024", "1344x768":
		return "16:9"
	case "1024x1792", "1024x1536", "768x1344":
		return "9:16"
	case "1152x896", "1216x832":
		return "4:3"
	case "896x1152", "832x1216":
		return "3:4"
	}
	// An explicit ratio passes through unchanged.
	if strings.Contains(size, ":") {
		return size
	}
	return ""
}

// firstNonBlank returns the first value that is not empty or whitespace.
func firstNonBlank(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
