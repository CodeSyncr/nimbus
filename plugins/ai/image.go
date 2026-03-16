/*
|--------------------------------------------------------------------------
| AI SDK — Image Generation
|--------------------------------------------------------------------------
|
| Fluent API for generating images through AI providers.
|
| Usage:
|
|   img, err := ai.Image().
|       Model("dall-e-3").
|       Prompt("cyberpunk city at night").
|       Size("1024x1024").
|       Generate(ctx)
|
*/

package ai

import (
	"context"
	"fmt"
)

// ---------------------------------------------------------------------------
// ImageBuilder — fluent image generation
// ---------------------------------------------------------------------------

// ImageBuilder provides a fluent API for configuring and generating images.
type ImageBuilder struct {
	req    ImageRequest
	client *Client
}

// Image starts building an image generation request.
func Image() *ImageBuilder {
	return &ImageBuilder{
		req: ImageRequest{N: 1},
	}
}

// Model sets the image generation model.
func (b *ImageBuilder) Model(model string) *ImageBuilder {
	b.req.Model = model
	return b
}

// Prompt sets the image description prompt.
func (b *ImageBuilder) Prompt(prompt string) *ImageBuilder {
	b.req.Prompt = prompt
	return b
}

// Size sets the output image size (e.g. "1024x1024", "512x512").
func (b *ImageBuilder) Size(size string) *ImageBuilder {
	b.req.Size = size
	return b
}

// Style sets the image style (e.g. "natural", "vivid").
func (b *ImageBuilder) Style(style string) *ImageBuilder {
	b.req.Style = style
	return b
}

// Count sets the number of images to generate.
func (b *ImageBuilder) Count(n int) *ImageBuilder {
	b.req.N = n
	return b
}

// WithClient overrides the default client.
func (b *ImageBuilder) WithClient(c *Client) *ImageBuilder {
	b.client = c
	return b
}

// Generate produces the image(s).
func (b *ImageBuilder) Generate(ctx context.Context) (*ImageResponse, error) {
	client := b.client
	if client == nil {
		client = GetClient()
	}

	ip, ok := client.provider.(ImageProvider)
	if !ok {
		return nil, fmt.Errorf("ai: provider %q does not support image generation", client.config.Provider)
	}
	return ip.GenerateImage(ctx, &b.req)
}
