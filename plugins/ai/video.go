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
	Audio    *bool  `json:"audio,omitempty"`
	Prompt   string `json:"prompt"`
	Model    string `json:"model,omitempty"`
	ImageURL string `json:"image_url,omitempty"` // for image-to-video
	Duration int    `json:"duration,omitempty"`  // seconds
	FPS      int    `json:"fps,omitempty"`
	Size     string `json:"size,omitempty"` // e.g. "1920x1080"
}

// VideoResponse wraps the generated video.
type VideoResponse struct {
	TaskID   string  `json:"task_id,omitempty"`
	Credits  float64 `json:"credits,omitempty"`
	URL      string  `json:"url"`
	Model    string  `json:"model"`
	Duration int     `json:"duration"`
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
	base := b.client
	if base == nil {
		base = GetClient()
	}
	client, err := base.videoFor()
	if err != nil {
		return nil, err
	}

	vp, ok := client.provider.(VideoProvider)
	if !ok {
		return nil, fmt.Errorf("ai: provider %q does not support video generation. Set AI_VIDEO_PROVIDER to one that does", client.config.Provider)
	}
	req := b.req
	req.Model = firstNonBlank(req.Model, client.config.VideoModel)
	return vp.GenerateVideo(ctx, &req)
}

// VideoTask is an asynchronous generation. Persist ID before polling; do not
// start another task merely because polling was interrupted.
type VideoTask struct {
	ID               string   `json:"id"`
	Status           string   `json:"status"`
	Output           []string `json:"output,omitempty"`
	Failure          string   `json:"failure,omitempty"`
	FailureCode      string   `json:"failure_code,omitempty"`
	Progress         float64  `json:"progress,omitempty"`
	EstimatedCredits float64  `json:"estimated_credits,omitempty"`
}

// AsyncVideoProvider supports task creation, polling and cancellation.
type AsyncVideoProvider interface {
	StartVideo(context.Context, *VideoRequest) (*VideoTask, error)
	GetVideoTask(context.Context, string) (*VideoTask, error)
	CancelVideoTask(context.Context, string) error
}

func (b *VideoBuilder) Audio(enabled bool) *VideoBuilder { b.req.Audio = &enabled; return b }
func (b *VideoBuilder) Start(ctx context.Context) (*VideoTask, error) {
	base := b.client
	if base == nil {
		base = GetClient()
	}
	client, err := base.videoFor()
	if err != nil {
		return nil, err
	}
	p, ok := client.provider.(AsyncVideoProvider)
	if !ok {
		return nil, fmt.Errorf("ai: provider %q does not support asynchronous video", client.config.Provider)
	}
	req := b.req
	req.Model = firstNonBlank(req.Model, client.config.VideoModel)
	return p.StartVideo(ctx, &req)
}
func (c *Client) GetVideoTask(ctx context.Context, id string) (*VideoTask, error) {
	client, err := c.videoFor()
	if err != nil {
		return nil, err
	}
	p, ok := client.provider.(AsyncVideoProvider)
	if !ok {
		return nil, fmt.Errorf("ai: provider does not support asynchronous video")
	}
	return p.GetVideoTask(ctx, id)
}
func (c *Client) CancelVideoTask(ctx context.Context, id string) error {
	client, err := c.videoFor()
	if err != nil {
		return err
	}
	p, ok := client.provider.(AsyncVideoProvider)
	if !ok {
		return fmt.Errorf("ai: provider does not support asynchronous video")
	}
	return p.CancelVideoTask(ctx, id)
}
