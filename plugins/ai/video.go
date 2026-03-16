/*
|--------------------------------------------------------------------------
| AI SDK — Video Generation
|--------------------------------------------------------------------------
|
| Fluent API for AI video generation. Supports text-to-video and
| image-to-video workflows (e.g. Kling, Runway, Sora).
|
| Usage:
|
|   video, err := ai.Video().
|       Model("kling-2.5").
|       Prompt("a sunset over the ocean").
|       Duration(5).
|       Generate(ctx)
|
|   // Image-to-video:
|   video, err := ai.Video().
|       Model("kling-2.5").
|       Prompt("camera slowly pans right").
|       FromImage(imageURL).
|       Generate(ctx)
|
*/

package ai

import (
	"context"
	"fmt"
)

// ---------------------------------------------------------------------------
// Video types
// ---------------------------------------------------------------------------

// VideoRequest configures a video generation call.
type VideoRequest struct {
	Prompt   string `json:"prompt"`
	Model    string `json:"model,omitempty"`
	ImageURL string `json:"image_url,omitempty"` // for image-to-video
	Duration int    `json:"duration,omitempty"`  // seconds
	FPS      int    `json:"fps,omitempty"`
	Size     string `json:"size,omitempty"` // e.g. "1920x1080"
}

// VideoResponse wraps the generated video.
type VideoResponse struct {
	URL      string `json:"url"`
	Model    string `json:"model"`
	Duration int    `json:"duration"`
}

// VideoProvider is the capability interface for video generation.
type VideoProvider interface {
	GenerateVideo(ctx context.Context, req *VideoRequest) (*VideoResponse, error)
}

// ---------------------------------------------------------------------------
// VideoBuilder — fluent API
// ---------------------------------------------------------------------------

// VideoBuilder provides a fluent API for video generation.
type VideoBuilder struct {
	req    VideoRequest
	client *Client
}

// Video starts building a video generation request.
func Video() *VideoBuilder {
	return &VideoBuilder{}
}

// Model sets the video generation model.
func (b *VideoBuilder) Model(model string) *VideoBuilder {
	b.req.Model = model
	return b
}

// Prompt sets the video description.
func (b *VideoBuilder) Prompt(prompt string) *VideoBuilder {
	b.req.Prompt = prompt
	return b
}

// FromImage sets a source image for image-to-video generation.
func (b *VideoBuilder) FromImage(url string) *VideoBuilder {
	b.req.ImageURL = url
	return b
}

// Duration sets the video duration in seconds.
func (b *VideoBuilder) Duration(seconds int) *VideoBuilder {
	b.req.Duration = seconds
	return b
}

// FPS sets the frames per second.
func (b *VideoBuilder) FPS(fps int) *VideoBuilder {
	b.req.FPS = fps
	return b
}

// Size sets the output resolution.
func (b *VideoBuilder) Size(size string) *VideoBuilder {
	b.req.Size = size
	return b
}

// WithClient overrides the default client.
func (b *VideoBuilder) WithClient(c *Client) *VideoBuilder {
	b.client = c
	return b
}

// Generate produces the video.
func (b *VideoBuilder) Generate(ctx context.Context) (*VideoResponse, error) {
	client := b.client
	if client == nil {
		client = GetClient()
	}

	vp, ok := client.provider.(VideoProvider)
	if !ok {
		return nil, fmt.Errorf("ai: provider %q does not support video generation", client.config.Provider)
	}
	return vp.GenerateVideo(ctx, &b.req)
}
