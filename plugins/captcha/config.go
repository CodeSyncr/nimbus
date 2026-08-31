package captcha

import (
	"time"
)

// Config defines settings for the Captcha plugin.
type Config struct {
	// APIKey for Nimbus Cloud Captcha API (or CapSolver fallback).
	APIKey string

	// Endpoint base URL for Nimbus Cloud Captcha API.
	// Default: "https://api.nimbuscloud.io/v1/captcha"
	Endpoint string

	// DefaultProvider for verification (e.g. "turnstile", "recaptcha", "hcaptcha", "nimbus").
	DefaultProvider string

	// ProviderSecretKeys maps provider names to secret keys used for server verification.
	// e.g. {"turnstile": "0x4AAAAAA...", "recaptcha": "6LeIx..."}
	ProviderSecretKeys map[string]string

	// MockMode when set to true will auto-solve tasks and approve verification
	// without reaching out to external networks (useful in dev/tests).
	MockMode bool

	// Timeout for captcha solving requests. Default: 60s.
	Timeout time.Duration

	// PollingInterval for checking task status. Default: 1s.
	PollingInterval time.Duration

	// MaxRetries for task polling. Default: 60.
	MaxRetries int
}

// DefaultConfig returns the standard default settings.
func DefaultConfig() *Config {
	return &Config{
		Endpoint:           "https://api.nimbuscloud.io/v1/captcha",
		DefaultProvider:    "turnstile",
		ProviderSecretKeys: make(map[string]string),
		MockMode:           false,
		Timeout:            60 * time.Second,
		PollingInterval:    1 * time.Second,
		MaxRetries:         60,
	}
}
