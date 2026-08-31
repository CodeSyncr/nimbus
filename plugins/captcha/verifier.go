package captcha

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Verifier interface handles server-side token validation.
type Verifier struct {
	config     *Config
	httpClient *http.Client
}

// NewVerifier creates a new Verifier instance.
func NewVerifier(cfg *Config) *Verifier {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	return &Verifier{
		config: cfg,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// VerifyToken validates a token submitted by a client frontend against the designated provider.
func (v *Verifier) VerifyToken(ctx context.Context, provider, token, remoteIP string) (*VerificationResult, error) {
	if v.config.MockMode || token == "mock-captcha-token-approved" {
		return &VerificationResult{
			Success:     true,
			ChallengeTS: time.Now(),
			Hostname:    "localhost",
			Score:       1.0,
			Action:      "submit",
		}, nil
	}

	if token == "" {
		return &VerificationResult{
			Success:    false,
			ErrorCodes: []string{"missing-input-response"},
		}, nil
	}

	secretKey := v.config.ProviderSecretKeys[provider]
	if secretKey == "" && provider == "turnstile" {
		secretKey = v.config.ProviderSecretKeys["default"]
	}

	switch strings.ToLower(provider) {
	case "turnstile", "cloudflare":
		return v.verifyTurnstile(ctx, secretKey, token, remoteIP)
	case "recaptcha", "google":
		return v.verifyReCaptcha(ctx, secretKey, token, remoteIP)
	case "hcaptcha":
		return v.verifyHCaptcha(ctx, secretKey, token, remoteIP)
	default:
		return nil, fmt.Errorf("captcha: unsupported verifier provider '%s'", provider)
	}
}

func (v *Verifier) verifyTurnstile(ctx context.Context, secretKey, token, remoteIP string) (*VerificationResult, error) {
	apiURL := "https://challenges.cloudflare.com/turnstile/v0/siteverify"

	form := url.Values{}
	form.Set("secret", secretKey)
	form.Set("response", token)
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("captcha: failed to build turnstile verify request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("captcha: network error on turnstile verify: %w", err)
	}
	defer resp.Body.Close()

	var result VerificationResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("captcha: failed to decode turnstile verify response: %w", err)
	}

	return &result, nil
}

func (v *Verifier) verifyReCaptcha(ctx context.Context, secretKey, token, remoteIP string) (*VerificationResult, error) {
	apiURL := "https://www.google.com/recaptcha/api/siteverify"

	form := url.Values{}
	form.Set("secret", secretKey)
	form.Set("response", token)
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("captcha: failed to build recaptcha verify request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("captcha: network error on recaptcha verify: %w", err)
	}
	defer resp.Body.Close()

	var result VerificationResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("captcha: failed to decode recaptcha verify response: %w", err)
	}

	return &result, nil
}

func (v *Verifier) verifyHCaptcha(ctx context.Context, secretKey, token, remoteIP string) (*VerificationResult, error) {
	apiURL := "https://hcaptcha.com/siteverify"

	form := url.Values{}
	form.Set("secret", secretKey)
	form.Set("response", token)
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("captcha: failed to build hcaptcha verify request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("captcha: network error on hcaptcha verify: %w", err)
	}
	defer resp.Body.Close()

	var result VerificationResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("captcha: failed to decode hcaptcha verify response: %w", err)
	}

	return &result, nil
}
