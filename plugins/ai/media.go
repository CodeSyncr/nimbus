package ai

import (
	"fmt"
	"strings"
)

/*
|--------------------------------------------------------------------------
| Image and video generation: their own model, account and endpoint
|--------------------------------------------------------------------------
|
| The model that draws, and the one that makes video, are rarely the one
| that writes, and often live on another account or gateway. Each can be
| set apart from the text settings:
|
|   AI_IMAGE_PROVIDER   AI_VIDEO_PROVIDER    default: AI_PROVIDER
|   AI_IMAGE_MODEL      AI_VIDEO_MODEL       default: the provider's own
|   AI_IMAGE_API_KEY    AI_VIDEO_API_KEY     default: the provider's key
|   AI_IMAGE_BASE_URL   AI_VIDEO_BASE_URL    default: the provider's URL
|
| (…_API_URL is accepted for …_BASE_URL.) With none of them set, images and
| video use the text client, as before.
|
| The fallback text model can live apart the same way: AI_FALLBACK_MODEL
| with AI_FALLBACK_PROVIDER / _API_KEY / _BASE_URL. A request that names
| the fallback model is then sent to that account.
*/

// mediaConfig derives the config of an image or video client from the
// text config: the provider, key and URL swapped in, the rest kept.
func mediaConfig(base *Config, what, provider, key, url string) (*Config, error) {
	c := *base
	// A media client makes no media clients of its own. The Image*/Video*
	// fields stay, so a provider registered by the app can read its key and
	// URL from them.
	c.mediaOf = what
	if p := strings.ToLower(strings.TrimSpace(provider)); p != "" {
		c.Provider = p
	}
	if c.Provider != base.Provider {
		c.Model = "" // the text model belongs to the other provider
	}
	key, url = strings.TrimSpace(key), strings.TrimRight(strings.TrimSpace(url), "/")
	// The clients add /chat/completions themselves; a URL copied from a
	// gateway's docs often carries it already.
	url = strings.TrimRight(strings.TrimSuffix(url, "/chat/completions"), "/")
	upper := strings.ToUpper(what)

	switch c.Provider {
	case "openai", "":
		if key != "" {
			c.OpenAIKey = key
		}
		if url != "" {
			c.OpenAIBaseURL = url
		}
	case "anthropic":
		if key != "" {
			c.AnthropicKey = key
		}
		if url != "" {
			c.AnthropicBaseURL, c.AnthropicAPIURL = url, url
		}
	case "ollama":
		if url != "" {
			c.OllamaHost = url
		}
	case "gemini", "mistral", "cohere", "xai":
		if url != "" {
			return nil, fmt.Errorf("AI_%s_BASE_URL is set but the %s provider does not take a custom URL; use AI_%s_PROVIDER=openai for an OpenAI-compatible gateway", upper, c.Provider, upper)
		}
		if key != "" {
			switch c.Provider {
			case "gemini":
				c.GeminiKey = key
			case "mistral":
				c.MistralKey = key
			case "cohere":
				c.CohereKey = key
			case "xai":
				c.XAIKey = key
			}
		}
	default:
		// A provider the app registered (ai.RegisterProvider) reads its own
		// key and URL, from AI_IMAGE_* / AI_VIDEO_* on the config it is given.
		if _, ok := GetProviderFactory(c.Provider); !ok {
			return nil, fmt.Errorf("unknown AI_%s_PROVIDER %q", upper, c.Provider)
		}
	}
	return &c, nil
}

// configureMedia gives the client its image and video clients when their
// settings differ from the text ones. A bad media setting is kept as an
// error for image or video calls to report; it never takes text down.
func (c *Client) configureMedia() {
	cfg := c.config
	if cfg == nil || cfg.mediaOf != "" {
		return
	}
	apart := func(provider, key, url string) bool {
		p := strings.TrimSpace(provider)
		return (p != "" && !strings.EqualFold(p, cfg.Provider)) || strings.TrimSpace(key) != "" || strings.TrimSpace(url) != ""
	}
	build := func(what, provider, key, url string) (*Client, error) {
		mc, err := mediaConfig(cfg, what, provider, key, url)
		if err != nil {
			return nil, err
		}
		return NewClient(mc)
	}
	if apart(cfg.ImageProvider, cfg.ImageAPIKey, cfg.ImageBaseURL) {
		c.image, c.imageErr = build("image", cfg.ImageProvider, cfg.ImageAPIKey, cfg.ImageBaseURL)
	}
	if apart(cfg.VideoProvider, cfg.VideoAPIKey, cfg.VideoBaseURL) {
		c.video, c.videoErr = build("video", cfg.VideoProvider, cfg.VideoAPIKey, cfg.VideoBaseURL)
	}
	if strings.TrimSpace(cfg.FallbackModel) != "" && apart(cfg.FallbackProvider, cfg.FallbackAPIKey, cfg.FallbackBaseURL) {
		mc, err := mediaConfig(cfg, "fallback", cfg.FallbackProvider, cfg.FallbackAPIKey, cfg.FallbackBaseURL)
		if err == nil {
			mc.Model = strings.TrimSpace(cfg.FallbackModel)
			c.fallback, c.fallbackErr = NewClient(mc)
		} else {
			c.fallbackErr = err
		}
	}
}

// fallbackFor is the client a request goes to when it names the fallback
// model and that model lives on its own account; nil keeps it here.
func (c *Client) fallbackFor(model string) (*Client, error) {
	if model == "" || c.config == nil || model != strings.TrimSpace(c.config.FallbackModel) {
		return nil, nil
	}
	if c.fallbackErr != nil {
		return nil, fmt.Errorf("ai: the fallback model is misconfigured: %w", c.fallbackErr)
	}
	return c.fallback, nil
}

// imageFor is the client image requests go to.
func (c *Client) imageFor() (*Client, error) {
	if c.imageErr != nil {
		return nil, fmt.Errorf("ai: image generation is misconfigured: %w", c.imageErr)
	}
	if c.image != nil {
		return c.image, nil
	}
	return c, nil
}

// videoFor is the client video requests go to.
func (c *Client) videoFor() (*Client, error) {
	if c.videoErr != nil {
		return nil, fmt.Errorf("ai: video generation is misconfigured: %w", c.videoErr)
	}
	if c.video != nil {
		return c.video, nil
	}
	return c, nil
}
