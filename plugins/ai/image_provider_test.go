package ai

import (
	"strings"
	"testing"
)

// Text and images can come from different providers: a deployment may reason
// through one endpoint and draw through another. This is the shape of a real
// configuration — AI_PROVIDER=openai with AI_IMAGE_PROVIDER=gemini.
func TestImageClientUsesTheConfiguredImageProvider(t *testing.T) {
	cfg := &Config{
		Provider:      "openai",
		Model:         "minimax-m3-free",
		OpenAIKey:     "sk-test",
		ImageProvider: "gemini  ", // trailing whitespace is tolerated
		ImageModel:    "gemini-3.1-flash-image-preview",
		GeminiKey:     "gm-test",
	}
	textClient, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	setClient(textClient)
	t.Cleanup(func() { setClient(nil) })

	img := imageClient()
	if img == textClient {
		t.Fatal("images went to the text provider despite AI_IMAGE_PROVIDER")
	}
	if _, ok := img.provider.(ImageProvider); !ok {
		t.Fatalf("the image client's provider %T cannot generate images", img.provider)
	}
	gp, ok := img.provider.(*geminiProvider)
	if !ok {
		t.Fatalf("image provider = %T, want gemini", img.provider)
	}
	if gp.imageModel != "gemini-3.1-flash-image-preview" {
		t.Errorf("image model = %q, want the configured one", gp.imageModel)
	}
}

// With no image provider configured, images stay with the text provider —
// which is right when that provider can draw, and produces a clear error when
// it cannot.
func TestImageClientFallsBackToTheTextProvider(t *testing.T) {
	cfg := &Config{Provider: "gemini", Model: "gemini-2.0-flash", GeminiKey: "gm-test"}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	setClient(client)
	t.Cleanup(func() { setClient(nil) })

	if imageClient() != client {
		t.Error("with no AI_IMAGE_PROVIDER the text client should serve images")
	}
}

// A provider that cannot draw must say so in a way that points at the fix.
func TestUnsupportedImageProviderExplainsItself(t *testing.T) {
	cfg := &Config{Provider: "openai", Model: "gpt-4o", OpenAIKey: "sk-test"}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	setClient(client)
	t.Cleanup(func() { setClient(nil) })

	_, err = Image().Prompt("a cat").Generate(t.Context())
	if err == nil {
		t.Fatal("expected an error from a provider that cannot generate images")
	}
	if !strings.Contains(err.Error(), "AI_IMAGE_PROVIDER") {
		t.Errorf("the error should name the setting that fixes it: %v", err)
	}
}
