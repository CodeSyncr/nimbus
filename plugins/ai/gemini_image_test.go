package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// imagenServer stands in for the Imagen predict endpoint and records what it
// was asked for.
func imagenServer(t *testing.T, got *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, ":predict") {
			http.Error(w, "wrong endpoint: "+r.URL.Path, http.StatusNotFound)
			return
		}
		if r.Header.Get("x-goog-api-key") == "" {
			http.Error(w, "missing api key", http.StatusUnauthorized)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(got)
		fmt.Fprint(w, `{"predictions":[{"bytesBase64Encoded":"aW1hZ2U=","mimeType":"image/png"}]}`)
	}))
}

func TestGeminiGeneratesWithImagen(t *testing.T) {
	var body map[string]any
	srv := imagenServer(t, &body)
	defer srv.Close()

	p := &geminiProvider{apiKey: "test-key", baseURL: srv.URL}
	resp, err := p.GenerateImage(context.Background(), &ImageRequest{
		Prompt: "a red bicycle", Size: "1024x1024",
	})
	if err != nil {
		t.Fatalf("GenerateImage: %v", err)
	}
	if len(resp.Images) != 1 || resp.Images[0].B64JSON != "aW1hZ2U=" {
		t.Fatalf("unexpected images: %+v", resp.Images)
	}
	if resp.Model != defaultGeminiImageModel {
		t.Errorf("model = %q, want the default %q", resp.Model, defaultGeminiImageModel)
	}

	instances, _ := body["instances"].([]any)
	if len(instances) != 1 {
		t.Fatalf("instances = %+v", body["instances"])
	}
	if prompt := instances[0].(map[string]any)["prompt"]; prompt != "a red bicycle" {
		t.Errorf("prompt = %v", prompt)
	}
	params, _ := body["parameters"].(map[string]any)
	if params["aspectRatio"] != "1:1" {
		t.Errorf("1024x1024 should map to a 1:1 ratio, got %v", params["aspectRatio"])
	}
}

// A gemini-* model uses generateContent and must ask for the IMAGE modality,
// or it replies with prose about the picture instead of the picture.
func TestGeminiGeneratesWithInlineImageModel(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, ":generateContent") {
			http.Error(w, "wrong endpoint: "+r.URL.Path, http.StatusNotFound)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		fmt.Fprint(w, `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"cGl4"}}]}}]}`)
	}))
	defer srv.Close()

	p := &geminiProvider{apiKey: "test-key", baseURL: srv.URL}
	resp, err := p.GenerateImage(context.Background(), &ImageRequest{
		Prompt: "a blue door", Model: "gemini-2.0-flash-preview-image-generation",
	})
	if err != nil {
		t.Fatalf("GenerateImage: %v", err)
	}
	if len(resp.Images) != 1 || resp.Images[0].B64JSON != "cGl4" {
		t.Fatalf("unexpected images: %+v", resp.Images)
	}

	cfg, _ := body["generationConfig"].(map[string]any)
	mods, _ := cfg["responseModalities"].([]any)
	var sawImage bool
	for _, m := range mods {
		if m == "IMAGE" {
			sawImage = true
		}
	}
	if !sawImage {
		t.Errorf("responseModalities must request IMAGE, got %v", mods)
	}
}

// The model is resolved builder → config → default, so an app can set one
// image model globally and still override it per call.
func TestImageModelResolutionOrder(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		fmt.Fprint(w, `{"predictions":[{"bytesBase64Encoded":"eA=="}]}`)
	}))
	defer srv.Close()

	// Configured default.
	p := &geminiProvider{apiKey: "k", baseURL: srv.URL, imageModel: "imagen-config-model"}
	if _, err := p.GenerateImage(context.Background(), &ImageRequest{Prompt: "x"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(path, "imagen-config-model") {
		t.Errorf("config model ignored, called %s", path)
	}

	// Per-call override wins.
	if _, err := p.GenerateImage(context.Background(), &ImageRequest{Prompt: "x", Model: "imagen-call-model"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(path, "imagen-call-model") {
		t.Errorf("per-call model ignored, called %s", path)
	}
}

func TestImageErrorsAreReadable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"model not found"}}`, http.StatusNotFound)
	}))
	defer srv.Close()

	p := &geminiProvider{apiKey: "k", baseURL: srv.URL}
	_, err := p.GenerateImage(context.Background(), &ImageRequest{Prompt: "x"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "model not found") {
		t.Errorf("error should carry the status and the server's message: %v", err)
	}

	// An empty prompt is caught before any request is made.
	if _, err := p.GenerateImage(context.Background(), &ImageRequest{}); err == nil {
		t.Error("empty prompt should be rejected")
	}
}

// A response with no image data must say so rather than returning an empty
// success.
func TestNoImageDataIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"predictions":[]}`)
	}))
	defer srv.Close()

	p := &geminiProvider{apiKey: "k", baseURL: srv.URL}
	if _, err := p.GenerateImage(context.Background(), &ImageRequest{Prompt: "x"}); err == nil {
		t.Error("expected an error when no image came back")
	}
}

func TestAspectRatioMapping(t *testing.T) {
	cases := map[string]string{
		"1024x1024": "1:1",
		"":          "1:1",
		"1792x1024": "16:9",
		"1024x1792": "9:16",
		"16:9":      "16:9", // an explicit ratio passes through
	}
	for size, want := range cases {
		if got := aspectRatioFor(size); got != want {
			t.Errorf("aspectRatioFor(%q) = %q, want %q", size, got, want)
		}
	}
}
