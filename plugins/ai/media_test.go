package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeOpenAI records every call an OpenAI-compatible gateway receives.
type fakeOpenAI struct {
	*httptest.Server
	mu    sync.Mutex
	calls []fakeCall
}

type fakeCall struct{ path, key, model string }

func newFakeOpenAI(t *testing.T) *fakeOpenAI {
	f := &fakeOpenAI{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(body, &req)
		f.mu.Lock()
		f.calls = append(f.calls, fakeCall{r.URL.Path, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), req.Model})
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/images/generations") {
			io.WriteString(w, `{"created":1,"data":[{"b64_json":"aGVsbG8="}]}`)
			return
		}
		io.WriteString(w, `{"id":"1","object":"chat.completion","model":"`+req.Model+`","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeOpenAI) only(t *testing.T) fakeCall {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) != 1 {
		t.Fatalf("%d calls, want 1: %+v", len(f.calls), f.calls)
	}
	c := f.calls[0]
	f.calls = nil
	return c
}

func useClient(t *testing.T, cfg *Config) *Client {
	t.Helper()
	c, err := NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	setClient(c)
	t.Cleanup(func() { setClient(nil) })
	return c
}

// Text and pictures on two gateways with two keys: each call goes where it
// belongs.
func TestImagesUseTheirOwnKeyAndEndpoint(t *testing.T) {
	text, draw := newFakeOpenAI(t), newFakeOpenAI(t)
	useClient(t, &Config{
		Provider: "openai", Model: "text-model", MaxTokens: 64,
		OpenAIKey: "text-key", OpenAIBaseURL: text.URL + "/v1",
		ImageModel: "draw-model", ImageAPIKey: "draw-key", ImageBaseURL: draw.URL + "/v1",
	})

	if _, err := Generate(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	if c := text.only(t); c.key != "text-key" || c.model != "text-model" {
		t.Errorf("text call: %+v", c)
	}

	res, err := Image().Prompt("a lighthouse").Generate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Images) != 1 {
		t.Fatalf("images: %+v", res)
	}
	if c := draw.only(t); c.path != "/v1/images/generations" || c.key != "draw-key" || c.model != "draw-model" {
		t.Errorf("image call: %+v", c)
	}
	if len(text.calls) != 0 {
		t.Errorf("the text gateway was asked to draw: %+v", text.calls)
	}
}

// With only AI_IMAGE_MODEL set, pictures stay on the text account, as before.
func TestImagesWithoutTheirOwnKeyStayOnTheTextAccount(t *testing.T) {
	text := newFakeOpenAI(t)
	c := useClient(t, &Config{
		Provider: "openai", Model: "text-model", OpenAIKey: "text-key", OpenAIBaseURL: text.URL + "/v1",
		ImageModel: "draw-model",
	})
	if c.image != nil {
		t.Fatal("a separate image client was made with no image key, URL or provider")
	}
	if _, err := Image().Prompt("a lighthouse").Generate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if call := text.only(t); call.key != "text-key" || call.model != "draw-model" {
		t.Errorf("image call: %+v", call)
	}
}

// fakeVideo is a provider an app might register for video.
type fakeVideo struct {
	cfg  *Config
	mu   sync.Mutex
	last *VideoRequest
}

func (f *fakeVideo) Name() string { return "fakevideo" }
func (f *fakeVideo) Generate(context.Context, *GenerateRequest) (*GenerateResponse, error) {
	return &GenerateResponse{Text: "not a writer"}, nil
}
func (f *fakeVideo) Stream(ctx context.Context, req *GenerateRequest) (*StreamResponse, error) {
	return GenerateToStream(ctx, f, req)
}
func (f *fakeVideo) GenerateVideo(_ context.Context, req *VideoRequest) (*VideoResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := *req
	f.last = &r
	return &VideoResponse{URL: "https://video.test/1.mp4", Model: req.Model}, nil
}

func TestVideoGoesToItsOwnProviderWithItsOwnModelAndKey(t *testing.T) {
	var made *fakeVideo
	RegisterProvider("fakevideo", func(cfg *Config) (Provider, error) {
		made = &fakeVideo{cfg: cfg}
		return made, nil
	})
	text := newFakeOpenAI(t)
	useClient(t, &Config{
		Provider: "openai", Model: "text-model", OpenAIKey: "text-key", OpenAIBaseURL: text.URL + "/v1",
		VideoProvider: "fakevideo", VideoModel: "clip-1", VideoAPIKey: "video-key", VideoBaseURL: "https://video.test/api",
	})

	res, err := Video().Prompt("waves at dusk").Generate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if made == nil || made.last == nil || made.last.Model != "clip-1" || res.URL == "" {
		t.Fatalf("video request: %+v %+v", made, res)
	}
	// A registered provider reads its key and URL from the config it is given.
	if made.cfg.VideoAPIKey != "video-key" || made.cfg.VideoBaseURL != "https://video.test/api" {
		t.Errorf("video provider config: key %q url %q", made.cfg.VideoAPIKey, made.cfg.VideoBaseURL)
	}
	// A model named on the call wins.
	if _, err := Video().Model("clip-2").Prompt("x").Generate(context.Background()); err != nil || made.last.Model != "clip-2" {
		t.Errorf("explicit model: %v %+v", err, made.last)
	}
	if len(text.calls) != 0 {
		t.Errorf("the text gateway was asked for video: %+v", text.calls)
	}
}

// A bad video setting is reported by video calls and leaves text working.
func TestABadVideoSettingDoesNotTakeTextDown(t *testing.T) {
	text := newFakeOpenAI(t)
	useClient(t, &Config{
		Provider: "openai", Model: "text-model", OpenAIKey: "text-key", OpenAIBaseURL: text.URL + "/v1",
		VideoProvider: "gemini", VideoBaseURL: "https://example.test",
	})
	_, err := Video().Prompt("x").Generate(context.Background())
	if err == nil || !strings.Contains(err.Error(), "AI_VIDEO_BASE_URL") || !strings.Contains(err.Error(), "gemini") {
		t.Errorf("want an error naming AI_VIDEO_BASE_URL and gemini, got %v", err)
	}
	if _, err := Generate(context.Background(), "still here?"); err != nil {
		t.Errorf("text broke with the video setting: %v", err)
	}
	_, err = Video().Prompt("x").Generate(context.Background())
	if err == nil {
		t.Error("expected the video error again")
	}
}

func TestUnknownMediaProviderIsNamed(t *testing.T) {
	_, err := mediaConfig(&Config{Provider: "openai"}, "image", "nope", "", "")
	if err == nil || !strings.Contains(err.Error(), "AI_IMAGE_PROVIDER") || !strings.Contains(err.Error(), "nope") {
		t.Errorf("got %v", err)
	}
}
