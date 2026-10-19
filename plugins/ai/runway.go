package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode/utf16"
)

// RunwayKlingStandard is the lowest-priced Kling tier on Runway, with audio off.
const RunwayKlingStandard = "kling3.0_standard"
const runwayVersion = "2024-11-06"

type runwayProvider struct {
	key, baseURL string
	http         *http.Client
	pollInterval time.Duration
}

func init() { RegisterProvider("runway", newRunwayProvider) }
func newRunwayProvider(cfg *Config) (Provider, error) {
	key := firstNonBlank(cfg.RunwayKey, os.Getenv("RUNWAY_API_KEY"))
	if cfg.mediaOf == "video" {
		key = firstNonBlank(cfg.VideoAPIKey, key)
	}
	if key == "" {
		return nil, errors.New("ai: Runway requires RUNWAY_API_KEY or AI_VIDEO_API_KEY")
	}
	base := firstNonBlank(cfg.RunwayBaseURL, "https://api.dev.runwayml.com")
	if cfg.mediaOf == "video" {
		base = firstNonBlank(cfg.VideoBaseURL, base)
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, errors.New("ai: invalid Runway base URL")
	}
	return &runwayProvider{key: key, baseURL: strings.TrimRight(base, "/"), http: &http.Client{Timeout: 30 * time.Second}, pollInterval: 5 * time.Second}, nil
}
func (p *runwayProvider) Name() string { return "runway" }
func (p *runwayProvider) Generate(context.Context, *GenerateRequest) (*GenerateResponse, error) {
	return nil, errors.New("ai: Runway is a video provider; configure it with AI_VIDEO_PROVIDER")
}
func (p *runwayProvider) Stream(context.Context, *GenerateRequest) (*StreamResponse, error) {
	return nil, errors.New("ai: Runway does not stream text")
}
func (p *runwayProvider) call(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.key)
	req.Header.Set("X-Runway-Version", runwayVersion)
	req.Header.Set("Content-Type", "application/json")
	res, err := p.http.Do(req)
	if err != nil {
		return fmt.Errorf("ai: Runway request failed: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("ai: Runway returned HTTP %d (check credentials, credits and request settings)", res.StatusCode)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(out); err != nil {
		return fmt.Errorf("ai: invalid Runway response: %w", err)
	}
	return nil
}
func runwayPayload(req *VideoRequest) (map[string]any, error) {
	if req == nil {
		return nil, errors.New("ai: video request is required")
	}
	prompt := strings.TrimSpace(req.Prompt)
	if len(utf16.Encode([]rune(prompt))) < 2 || len(utf16.Encode([]rune(prompt))) > 2500 {
		return nil, errors.New("ai: Kling prompt must be 2–2500 UTF-16 characters")
	}
	model := firstNonBlank(req.Model, RunwayKlingStandard)
	duration := req.Duration
	if duration == 0 {
		duration = 5
	}
	ratio := strings.ReplaceAll(req.Size, "x", ":")
	if ratio == "" {
		ratio = "1280:720"
	}
	allowed := []string{"1280:720", "720:1280", "960:960"}
	switch model {
	case RunwayKlingStandard:
	case "kling3.0_pro":
		allowed = []string{"1920:1080", "1080:1920", "1440:1440"}
	case "kling3.0_4k":
		allowed = []string{"3840:2160", "2160:3840", "2880:2880"}
	case "kling2.5_turbo_pro":
		allowed = []string{"1920:1080", "1080:1920", "1440:1440", "1280:720", "720:1280", "720:720"}
		if duration != 5 && duration != 10 {
			return nil, errors.New("ai: Kling 2.5 duration must be 5 or 10 seconds")
		}
	default:
		return nil, fmt.Errorf("ai: unsupported Runway video model %q", model)
	}
	if duration < 3 || duration > 15 {
		return nil, errors.New("ai: Kling duration must be 3–15 seconds")
	}
	if req.FPS != 0 {
		return nil, errors.New("ai: Runway Kling does not expose an FPS parameter")
	}
	valid := false
	for _, r := range allowed {
		valid = valid || r == ratio
	}
	if !valid {
		return nil, fmt.Errorf("ai: model %s does not support size %s", model, ratio)
	}
	body := map[string]any{"model": model, "promptText": prompt, "duration": duration, "ratio": ratio}
	if model != "kling2.5_turbo_pro" {
		body["audio"] = req.Audio != nil && *req.Audio
	}
	if req.ImageURL != "" {
		if !(strings.HasPrefix(req.ImageURL, "https://") || strings.HasPrefix(req.ImageURL, "runway://") || strings.HasPrefix(req.ImageURL, "data:image/")) {
			return nil, errors.New("ai: Runway image must be an HTTPS URL, runway URI or image data URI")
		}
		body["promptImage"] = req.ImageURL
	}
	return body, nil
}
func (p *runwayProvider) StartVideo(ctx context.Context, req *VideoRequest) (*VideoTask, error) {
	body, err := runwayPayload(req)
	if err != nil {
		return nil, err
	}
	path := "/v1/text_to_video"
	if req.ImageURL != "" {
		path = "/v1/image_to_video"
	}
	var raw struct {
		ID            string `json:"id"`
		EstimatedCost struct {
			Credits float64 `json:"credits"`
		} `json:"estimatedCost"`
	}
	// Starting a task is billable: never automatically retry this POST.
	if err = p.call(ctx, http.MethodPost, path, body, &raw); err != nil {
		return nil, err
	}
	if raw.ID == "" {
		return nil, errors.New("ai: Runway returned no task id")
	}
	return &VideoTask{ID: raw.ID, Status: "PENDING", EstimatedCredits: raw.EstimatedCost.Credits}, nil
}
func runwayTaskPath(id string) (string, error) {
	if id == "" || len(id) > 100 || strings.ContainsAny(id, "/\\?#%") || strings.Contains(id, "..") {
		return "", errors.New("ai: invalid video task id")
	}
	return "/v1/tasks/" + url.PathEscape(id), nil
}
func (p *runwayProvider) GetVideoTask(ctx context.Context, id string) (*VideoTask, error) {
	path, err := runwayTaskPath(id)
	if err != nil {
		return nil, err
	}
	var raw struct {
		ID            string   `json:"id"`
		Status        string   `json:"status"`
		Output        []string `json:"output"`
		Failure       string   `json:"failure"`
		FailureCode   string   `json:"failureCode"`
		Progress      float64  `json:"progress"`
		EstimatedCost struct {
			Credits float64 `json:"credits"`
		} `json:"estimatedCost"`
	}
	if err = p.call(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return nil, err
	}
	if raw.ID == "" || raw.Status == "" {
		return nil, errors.New("ai: incomplete Runway task response")
	}
	return &VideoTask{ID: raw.ID, Status: raw.Status, Output: raw.Output, Failure: raw.Failure, FailureCode: raw.FailureCode, Progress: raw.Progress, EstimatedCredits: raw.EstimatedCost.Credits}, nil
}
func (p *runwayProvider) CancelVideoTask(ctx context.Context, id string) error {
	path, err := runwayTaskPath(id)
	if err != nil {
		return err
	}
	return p.call(ctx, http.MethodDelete, path, nil, nil)
}
func (p *runwayProvider) GenerateVideo(ctx context.Context, req *VideoRequest) (*VideoResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	task, err := p.StartVideo(ctx, req)
	if err != nil {
		return nil, err
	}
	terminal := false
	defer func() {
		if !terminal {
			cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			_ = p.CancelVideoTask(cleanup, task.ID)
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(p.pollInterval):
		}
		state, err := p.GetVideoTask(ctx, task.ID)
		if err != nil {
			return nil, err
		}
		switch state.Status {
		case "SUCCEEDED":
			terminal = true
			if len(state.Output) == 0 {
				return nil, errors.New("ai: Runway completed without video output")
			}
			duration := req.Duration
			if duration == 0 {
				duration = 5
			}
			return &VideoResponse{URL: state.Output[0], Model: firstNonBlank(req.Model, RunwayKlingStandard), Duration: duration, TaskID: task.ID, Credits: task.EstimatedCredits}, nil
		case "FAILED", "CANCELED", "CANCELLED":
			terminal = true
			return nil, fmt.Errorf("ai: Runway task %s (%s)", state.Status, state.FailureCode)
		}
	}
}
