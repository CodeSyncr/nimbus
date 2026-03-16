/*
|--------------------------------------------------------------------------
| AI SDK — Video Production Pipeline
|--------------------------------------------------------------------------
|
| End-to-end cinematic video production: prompt expansion, scene
| planning, keyframe generation, video synthesis, scene stitching,
| camera controls, style consistency, and social media packaging.
|
| Pipeline:
|
|   User Prompt
|     → Prompt Expander (LLM)
|     → Scene Planner
|     → Keyframe Generator (Image model)
|     → Video Generator (Video model)
|     → Scene Stitcher (FFmpeg)
|     → Final Video
|
| Usage:
|
|   pipeline := ai.NewVideoPipeline().
|       ImageModel("dall-e-3").
|       VideoModel("kling-2.5").
|       GlobalStyle("cinematic, warm lighting, film grain").
|       OutputFormat(ai.FormatTikTok).
|       DraftMode(true)
|
|   project, err := pipeline.Generate(ctx, "cinematic pizza ad in italian cafe")
|
*/

package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Constants — output formats & camera moves
// ---------------------------------------------------------------------------

// OutputFormat defines the aspect ratio and sizing for the final render.
type OutputFormat struct {
	Name   string `json:"name"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Aspect string `json:"aspect"` // e.g. "9:16"
}

// Pre-defined social-media output formats.
var (
	FormatTikTok    = OutputFormat{Name: "tiktok", Width: 1080, Height: 1920, Aspect: "9:16"}
	FormatInstagram = OutputFormat{Name: "instagram_reel", Width: 1080, Height: 1920, Aspect: "9:16"}
	FormatYouTube   = OutputFormat{Name: "youtube_short", Width: 1080, Height: 1920, Aspect: "9:16"}
	FormatSquare    = OutputFormat{Name: "square", Width: 1080, Height: 1080, Aspect: "1:1"}
	FormatWide      = OutputFormat{Name: "wide", Width: 1920, Height: 1080, Aspect: "16:9"}
	FormatCinematic = OutputFormat{Name: "cinematic", Width: 1920, Height: 816, Aspect: "2.35:1"}
)

// CameraMove is a prompt modifier for camera motion.
type CameraMove string

const (
	CameraDolly    CameraMove = "slow cinematic dolly in"
	CameraOrbit    CameraMove = "smooth orbit around subject"
	CameraPan      CameraMove = "slow horizontal pan"
	CameraDrone    CameraMove = "aerial drone flyover"
	CameraHandheld CameraMove = "handheld camera movement with subtle shake"
	CameraMacro    CameraMove = "extreme macro close-up with rack focus"
	CameraZoomIn   CameraMove = "slow cinematic zoom in"
	CameraZoomOut  CameraMove = "slow cinematic zoom out"
	CameraStatic   CameraMove = "locked off static shot"
	CameraCrane    CameraMove = "crane up reveal"
	CameraTracking CameraMove = "tracking shot following subject"
)

// ---------------------------------------------------------------------------
// Scene — a single unit of the video
// ---------------------------------------------------------------------------

// Scene represents one segment of the planned video.
type Scene struct {
	Index       int            `json:"index"`
	Prompt      string         `json:"prompt"`
	Camera      CameraMove     `json:"camera"`
	Duration    int            `json:"duration"` // seconds
	KeyframeURL string         `json:"keyframe_url,omitempty"`
	VideoURL    string         `json:"video_url,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// ScenePlan is the output of the scene planner.
type ScenePlan struct {
	OriginalPrompt string  `json:"original_prompt"`
	ExpandedPrompt string  `json:"expanded_prompt"`
	GlobalStyle    string  `json:"global_style"`
	Scenes         []Scene `json:"scenes"`
}

// ---------------------------------------------------------------------------
// VideoPipelineConfig
// ---------------------------------------------------------------------------

// VideoPipelineConfig holds all settings for the video pipeline.
type VideoPipelineConfig struct {
	ImageModel      string                          `json:"image_model"`
	VideoModel      string                          `json:"video_model"`
	PlannerModel    string                          `json:"planner_model"`  // LLM for scene planning
	ExpanderModel   string                          `json:"expander_model"` // LLM for prompt expansion
	GlobalStyle     string                          `json:"global_style"`
	MaxScenes       int                             `json:"max_scenes"`
	DefaultDuration int                             `json:"default_duration"` // per-scene seconds
	DraftMode       bool                            `json:"draft_mode"`       // shorter preview clips
	Format          OutputFormat                    `json:"format"`
	Parallelism     int                             `json:"parallelism"` // concurrent scene renders
	Seeds           int                             `json:"seeds"`       // keyframe seed variants
	OnScene         func(scene Scene, stage string) // progress callback
}

// ---------------------------------------------------------------------------
// VideoProject — result of the pipeline
// ---------------------------------------------------------------------------

// VideoProject is the output of a full pipeline run.
type VideoProject struct {
	ID             string         `json:"id"`
	Prompt         string         `json:"prompt"`
	Plan           *ScenePlan     `json:"plan"`
	Scenes         []Scene        `json:"scenes"`
	FinalVideoURL  string         `json:"final_video_url,omitempty"`
	Format         OutputFormat   `json:"format"`
	Duration       time.Duration  `json:"duration"`
	Social         *SocialPackage `json:"social,omitempty"`
	RenderDuration time.Duration  `json:"render_duration"`
}

// SocialPackage holds auto-generated social media assets.
type SocialPackage struct {
	Caption      string            `json:"caption"`
	Hashtags     []string          `json:"hashtags"`
	ThumbnailURL string            `json:"thumbnail_url,omitempty"`
	Formats      map[string]string `json:"formats"` // format name → video URL
}

// ---------------------------------------------------------------------------
// VideoPipeline — fluent builder
// ---------------------------------------------------------------------------

// VideoPipeline orchestrates the full video production flow.
type VideoPipeline struct {
	config VideoPipelineConfig
	client *Client
}

// NewVideoPipeline creates a new pipeline with sensible defaults.
func NewVideoPipeline() *VideoPipeline {
	return &VideoPipeline{
		config: VideoPipelineConfig{
			ImageModel:      "dall-e-3",
			VideoModel:      "kling-2.5",
			PlannerModel:    "", // uses default
			ExpanderModel:   "", // uses default
			MaxScenes:       6,
			DefaultDuration: 3,
			DraftMode:       false,
			Format:          FormatWide,
			Parallelism:     3,
			Seeds:           1,
			GlobalStyle:     "cinematic lighting, shallow depth of field, film grain, professional color grading",
		},
	}
}

// ImageModel sets the model for keyframe image generation.
func (p *VideoPipeline) ImageModel(model string) *VideoPipeline {
	p.config.ImageModel = model
	return p
}

// VideoModel sets the model for video synthesis.
func (p *VideoPipeline) VideoModel(model string) *VideoPipeline {
	p.config.VideoModel = model
	return p
}

// PlannerModel sets the LLM used for scene planning.
func (p *VideoPipeline) PlannerModel(model string) *VideoPipeline {
	p.config.PlannerModel = model
	return p
}

// ExpanderModel sets the LLM used for prompt expansion.
func (p *VideoPipeline) ExpanderModel(model string) *VideoPipeline {
	p.config.ExpanderModel = model
	return p
}

// GlobalStyle sets the style prompt appended to every scene.
func (p *VideoPipeline) GlobalStyle(style string) *VideoPipeline {
	p.config.GlobalStyle = style
	return p
}

// MaxScenes limits the number of scenes generated.
func (p *VideoPipeline) MaxScenes(n int) *VideoPipeline {
	p.config.MaxScenes = n
	return p
}

// DefaultDuration sets the per-scene duration in seconds.
func (p *VideoPipeline) DefaultDuration(seconds int) *VideoPipeline {
	p.config.DefaultDuration = seconds
	return p
}

// DraftMode enables shorter preview clips for cost optimization.
func (p *VideoPipeline) DraftMode(draft bool) *VideoPipeline {
	p.config.DraftMode = draft
	return p
}

// OutputFmt sets the output video format/aspect ratio.
func (p *VideoPipeline) OutputFmt(format OutputFormat) *VideoPipeline {
	p.config.Format = format
	return p
}

// ConcurrentScenes sets parallelism for scene rendering.
func (p *VideoPipeline) ConcurrentScenes(n int) *VideoPipeline {
	p.config.Parallelism = n
	return p
}

// SeedVariants sets how many keyframe variants to generate per scene.
func (p *VideoPipeline) SeedVariants(n int) *VideoPipeline {
	p.config.Seeds = n
	return p
}

// OnScene registers a progress callback fired at each pipeline stage.
func (p *VideoPipeline) OnScene(fn func(scene Scene, stage string)) *VideoPipeline {
	p.config.OnScene = fn
	return p
}

// WithClient overrides the default AI client.
func (p *VideoPipeline) WithClient(c *Client) *VideoPipeline {
	p.client = c
	return p
}

// ---------------------------------------------------------------------------
// Generate — execute the full pipeline
// ---------------------------------------------------------------------------

// Generate runs the entire video production pipeline:
// expand → plan → keyframes → videos → stitch.
func (p *VideoPipeline) Generate(ctx context.Context, prompt string) (*VideoProject, error) {
	start := time.Now()
	project := &VideoProject{
		ID:     fmt.Sprintf("vp_%d", time.Now().UnixMilli()),
		Prompt: prompt,
		Format: p.config.Format,
	}

	// Step 1: Expand the user prompt into a richer description.
	expanded, err := p.expandPrompt(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("ai: video pipeline: expand: %w", err)
	}

	// Step 2: Plan scenes from the expanded prompt.
	plan, err := p.planScenes(ctx, prompt, expanded)
	if err != nil {
		return nil, fmt.Errorf("ai: video pipeline: plan: %w", err)
	}
	project.Plan = plan

	// Step 3: Generate keyframes + videos (parallelized).
	scenes, err := p.renderScenes(ctx, plan)
	if err != nil {
		return nil, fmt.Errorf("ai: video pipeline: render: %w", err)
	}
	project.Scenes = scenes

	// Step 4: Calculate total duration.
	var totalDuration int
	for _, s := range scenes {
		totalDuration += s.Duration
	}
	project.Duration = time.Duration(totalDuration) * time.Second
	project.RenderDuration = time.Since(start)

	return project, nil
}

// GenerateWithSocial runs the pipeline and also generates social media packaging.
func (p *VideoPipeline) GenerateWithSocial(ctx context.Context, prompt string) (*VideoProject, error) {
	project, err := p.Generate(ctx, prompt)
	if err != nil {
		return nil, err
	}

	social, err := p.generateSocialPackage(ctx, prompt, project)
	if err != nil {
		// Non-fatal — return project without social.
		project.Social = &SocialPackage{
			Caption:  prompt,
			Hashtags: []string{},
			Formats:  map[string]string{},
		}
		return project, nil
	}
	project.Social = social
	return project, nil
}

// ---------------------------------------------------------------------------
// Step 1: Prompt Expansion
// ---------------------------------------------------------------------------

func (p *VideoPipeline) expandPrompt(ctx context.Context, prompt string) (string, error) {
	opts := []GenerateOption{
		WithSystem("You are a cinematic video director. Expand the user's brief prompt into " +
			"a rich, detailed visual description suitable for AI video generation. Include " +
			"lighting, mood, color palette, camera style, and environment details. " +
			"Keep it under 200 words. Return ONLY the expanded description, no commentary."),
		WithMaxTokens(300),
	}
	if p.config.ExpanderModel != "" {
		opts = append(opts, WithModel(p.config.ExpanderModel))
	}

	client := p.getClient()
	resp, err := client.Generate(ctx, prompt, opts...)
	if err != nil {
		return prompt, err // fallback to original
	}
	return resp.Text, nil
}

// ---------------------------------------------------------------------------
// Step 2: Scene Planning
// ---------------------------------------------------------------------------

func (p *VideoPipeline) planScenes(ctx context.Context, original, expanded string) (*ScenePlan, error) {
	duration := p.config.DefaultDuration
	if p.config.DraftMode {
		duration = 2
	}

	systemPrompt := fmt.Sprintf(`You are a cinematic storyboard planner. Given a video concept, break it into %d or fewer scenes.

For each scene provide:
- "prompt": a detailed visual description (include environment, lighting, subject, action)
- "camera": one of: dolly, orbit, pan, drone, handheld, macro, zoom_in, zoom_out, static, crane, tracking
- "duration": seconds (%d default)

Return ONLY valid JSON:
{
  "scenes": [
    {"prompt": "...", "camera": "dolly", "duration": %d},
    ...
  ]
}`, p.config.MaxScenes, duration, duration)

	opts := []GenerateOption{
		WithSystem(systemPrompt),
		WithMaxTokens(1500),
		WithTemperature(0.7),
	}
	if p.config.PlannerModel != "" {
		opts = append(opts, WithModel(p.config.PlannerModel))
	}

	client := p.getClient()
	resp, err := client.Generate(ctx, expanded, opts...)
	if err != nil {
		return nil, err
	}

	// Parse the scene plan JSON.
	var raw struct {
		Scenes []struct {
			Prompt   string `json:"prompt"`
			Camera   string `json:"camera"`
			Duration int    `json:"duration"`
		} `json:"scenes"`
	}

	text := extractJSON(resp.Text)
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return nil, fmt.Errorf("ai: scene planner returned invalid JSON: %w\nResponse: %s", err, resp.Text)
	}

	plan := &ScenePlan{
		OriginalPrompt: original,
		ExpandedPrompt: expanded,
		GlobalStyle:    p.config.GlobalStyle,
	}

	for i, s := range raw.Scenes {
		if i >= p.config.MaxScenes {
			break
		}
		dur := s.Duration
		if dur <= 0 {
			dur = p.config.DefaultDuration
		}
		plan.Scenes = append(plan.Scenes, Scene{
			Index:    i,
			Prompt:   s.Prompt,
			Camera:   parseCameraMove(s.Camera),
			Duration: dur,
		})
	}

	return plan, nil
}

// ---------------------------------------------------------------------------
// Step 3: Scene Rendering (keyframes + video)
// ---------------------------------------------------------------------------

func (p *VideoPipeline) renderScenes(ctx context.Context, plan *ScenePlan) ([]Scene, error) {
	scenes := make([]Scene, len(plan.Scenes))
	copy(scenes, plan.Scenes)

	sem := make(chan struct{}, p.config.Parallelism)
	var mu sync.Mutex
	var wg sync.WaitGroup
	var firstErr error

	for i := range scenes {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			scene := &scenes[idx]

			// Fire progress callback.
			if p.config.OnScene != nil {
				p.config.OnScene(*scene, "keyframe_start")
			}

			// Generate keyframe image.
			imgPrompt := p.buildImagePrompt(scene, plan.GlobalStyle)
			keyframe, err := p.generateKeyframe(ctx, imgPrompt)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("scene %d keyframe: %w", idx, err)
				}
				mu.Unlock()
				return
			}
			scene.KeyframeURL = keyframe

			if p.config.OnScene != nil {
				p.config.OnScene(*scene, "keyframe_done")
			}

			// Generate video from keyframe.
			if p.config.OnScene != nil {
				p.config.OnScene(*scene, "video_start")
			}

			videoURL, err := p.generateSceneVideo(ctx, scene)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("scene %d video: %w", idx, err)
				}
				mu.Unlock()
				return
			}
			scene.VideoURL = videoURL

			if p.config.OnScene != nil {
				p.config.OnScene(*scene, "video_done")
			}
		}(i)
	}

	wg.Wait()
	if firstErr != nil {
		return scenes, firstErr
	}
	return scenes, nil
}

func (p *VideoPipeline) generateKeyframe(ctx context.Context, prompt string) (string, error) {
	client := p.getClient()
	ip, ok := client.provider.(ImageProvider)
	if !ok {
		return "", fmt.Errorf("ai: provider does not support image generation")
	}

	resp, err := ip.GenerateImage(ctx, &ImageRequest{
		Prompt: prompt,
		Model:  p.config.ImageModel,
		N:      1,
		Size:   fmt.Sprintf("%dx%d", p.config.Format.Width, p.config.Format.Height),
	})
	if err != nil {
		return "", err
	}
	if len(resp.Images) == 0 {
		return "", fmt.Errorf("ai: no keyframe image returned")
	}
	return resp.Images[0].URL, nil
}

func (p *VideoPipeline) generateSceneVideo(ctx context.Context, scene *Scene) (string, error) {
	client := p.getClient()
	vp, ok := client.provider.(VideoProvider)
	if !ok {
		return "", fmt.Errorf("ai: provider does not support video generation")
	}

	duration := scene.Duration
	if p.config.DraftMode && duration > 2 {
		duration = 2
	}

	videoPrompt := string(scene.Camera)
	resp, err := vp.GenerateVideo(ctx, &VideoRequest{
		Prompt:   videoPrompt,
		Model:    p.config.VideoModel,
		ImageURL: scene.KeyframeURL,
		Duration: duration,
		Size:     fmt.Sprintf("%dx%d", p.config.Format.Width, p.config.Format.Height),
	})
	if err != nil {
		return "", err
	}
	return resp.URL, nil
}

// ---------------------------------------------------------------------------
// Social Media Packaging
// ---------------------------------------------------------------------------

func (p *VideoPipeline) generateSocialPackage(ctx context.Context, prompt string, project *VideoProject) (*SocialPackage, error) {
	systemPrompt := `You are a social media manager. Given a video concept, generate:
1. A compelling caption (2-3 sentences)
2. 5-8 relevant hashtags
Return ONLY valid JSON: {"caption": "...", "hashtags": ["#tag1", "#tag2"]}`

	client := p.getClient()
	resp, err := client.Generate(ctx, "Video concept: "+prompt, WithSystem(systemPrompt), WithMaxTokens(300))
	if err != nil {
		return nil, err
	}

	var result struct {
		Caption  string   `json:"caption"`
		Hashtags []string `json:"hashtags"`
	}
	text := extractJSON(resp.Text)
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return &SocialPackage{Caption: prompt}, nil
	}

	pkg := &SocialPackage{
		Caption:  result.Caption,
		Hashtags: result.Hashtags,
		Formats:  make(map[string]string),
	}

	// Map the project's format to the primary output.
	if project.FinalVideoURL != "" {
		pkg.Formats[project.Format.Name] = project.FinalVideoURL
	}

	return pkg, nil
}

// ---------------------------------------------------------------------------
// FFmpeg Stitch Command Generator
// ---------------------------------------------------------------------------

// StitchCommand generates the FFmpeg command to concatenate scene videos
// with optional transitions. The caller is responsible for executing it.
type StitchConfig struct {
	FadeSeconds float64 `json:"fade_seconds"`
	MusicPath   string  `json:"music_path,omitempty"`
	CaptionText string  `json:"caption_text,omitempty"`
	OutputPath  string  `json:"output_path"`
	ConcatFile  string  `json:"concat_file"` // path to write the file list
}

// GenerateStitchCommand builds an FFmpeg concat command for the scenes.
func GenerateStitchCommand(scenes []Scene, cfg StitchConfig) []string {
	// Build the concat file content.
	var parts []string
	for _, s := range scenes {
		if s.VideoURL != "" {
			parts = append(parts, fmt.Sprintf("file '%s'", s.VideoURL))
		}
	}

	args := []string{
		"ffmpeg", "-y",
		"-f", "concat",
		"-safe", "0",
		"-i", cfg.ConcatFile,
	}

	if cfg.MusicPath != "" {
		args = append(args, "-i", cfg.MusicPath, "-shortest")
	}

	args = append(args, "-c", "copy")

	if cfg.FadeSeconds > 0 {
		// Crossfade filter would go here — requires re-encoding.
		args = append(args, "-movflags", "+faststart")
	}

	args = append(args, cfg.OutputPath)
	return args
}

// GenerateConcatFileContent returns the FFmpeg concat file content.
func GenerateConcatFileContent(scenes []Scene) string {
	var b strings.Builder
	for _, s := range scenes {
		if s.VideoURL != "" {
			fmt.Fprintf(&b, "file '%s'\n", s.VideoURL)
		}
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Multi-Format Export
// ---------------------------------------------------------------------------

// ExportFormats defines the standard social media output set.
var ExportFormats = map[string]OutputFormat{
	"tiktok":    FormatTikTok,
	"instagram": FormatInstagram,
	"youtube":   FormatYouTube,
	"square":    FormatSquare,
	"wide":      FormatWide,
	"cinematic": FormatCinematic,
}

// GenerateFFmpegResizeCommand generates an FFmpeg command to re-scale
// a video to a different output format.
func GenerateFFmpegResizeCommand(inputPath string, format OutputFormat, outputPath string) []string {
	return []string{
		"ffmpeg", "-y",
		"-i", inputPath,
		"-vf", fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2",
			format.Width, format.Height, format.Width, format.Height),
		"-c:a", "copy",
		outputPath,
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (p *VideoPipeline) getClient() *Client {
	if p.client != nil {
		return p.client
	}
	return GetClient()
}

func (p *VideoPipeline) buildImagePrompt(scene *Scene, globalStyle string) string {
	parts := []string{scene.Prompt}
	if globalStyle != "" {
		parts = append(parts, globalStyle)
	}
	// Add lens hint from camera move.
	switch scene.Camera {
	case CameraMacro:
		parts = append(parts, "macro lens, extreme close-up")
	case CameraDrone:
		parts = append(parts, "aerial view, birds eye perspective")
	case CameraCrane:
		parts = append(parts, "high angle, looking down")
	default:
		parts = append(parts, "35mm lens, professional photography")
	}
	return strings.Join(parts, ", ")
}

// extractJSON finds the first { ... } block in text (handling nested braces).
func extractJSON(text string) string {
	start := strings.Index(text, "{")
	if start < 0 {
		return text
	}
	depth := 0
	for i := start; i < len(text); i++ {
		switch text[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return text[start : i+1]
			}
		}
	}
	return text[start:]
}

func parseCameraMove(s string) CameraMove {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "dolly":
		return CameraDolly
	case "orbit":
		return CameraOrbit
	case "pan":
		return CameraPan
	case "drone":
		return CameraDrone
	case "handheld":
		return CameraHandheld
	case "macro":
		return CameraMacro
	case "zoom_in", "zoom-in":
		return CameraZoomIn
	case "zoom_out", "zoom-out":
		return CameraZoomOut
	case "static":
		return CameraStatic
	case "crane":
		return CameraCrane
	case "tracking":
		return CameraTracking
	default:
		return CameraDolly
	}
}
