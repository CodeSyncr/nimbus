// Package socialite provides OAuth2-based social authentication (similar
// to Laravel Socialite). It ships with built-in providers for Google,
// GitHub, Discord, and Apple, and allows registering custom providers.
//
// # Quick start
//
//	s := socialite.New(socialite.Config{
//	    Providers: map[string]socialite.ProviderConfig{
//	        "github": {
//	            ClientID:     os.Getenv("GITHUB_CLIENT_ID"),
//	            ClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"),
//	            RedirectURL:  "http://localhost:3333/auth/github/callback",
//	        },
//	    },
//	})
//
//	// Redirect to provider
//	app.GET("/auth/:provider", s.RedirectHandler())
//	// Handle callback
//	app.GET("/auth/:provider/callback", s.CallbackHandler(myCallback))
package socialite

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ── Social User ─────────────────────────────────────────────────

// SocialUser represents the authenticated user returned by an OAuth provider.
type SocialUser struct {
	Provider    string `json:"provider"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Email       string `json:"email"`
	Avatar      string `json:"avatar"`
	AccessToken string `json:"access_token"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	Raw         map[string]any `json:"raw,omitempty"`
}

// ── Provider interface ──────────────────────────────────────────

// Provider defines the contract for an OAuth provider.
type Provider interface {
	// Name returns the provider name (e.g. "github", "google").
	Name() string
	// AuthURL returns the URL the user should be redirected to for authentication.
	AuthURL(state string, scopes []string) string
	// Exchange trades an authorization code for user information.
	Exchange(ctx context.Context, code string) (*SocialUser, error)
}

// ── Provider Config ─────────────────────────────────────────────

// ProviderConfig holds OAuth2 credentials for a provider.
type ProviderConfig struct {
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	RedirectURL  string   `json:"redirect_url"`
	Scopes       []string `json:"scopes"`
}

// ── Config ──────────────────────────────────────────────────────

// Config holds the socialite configuration.
type Config struct {
	// Providers maps provider name → credentials.
	Providers map[string]ProviderConfig
}

// ── Socialite Manager ───────────────────────────────────────────

// Socialite manages OAuth providers.
type Socialite struct {
	mu        sync.RWMutex
	providers map[string]Provider
	config    Config
}

// New creates a new Socialite manager with the given config.
// Built-in providers (github, google, discord, apple) are auto-registered
// if their config is present.
func New(cfg Config) *Socialite {
	s := &Socialite{
		providers: make(map[string]Provider),
		config:    cfg,
	}

	// Auto-register built-in providers
	for name, pc := range cfg.Providers {
		switch strings.ToLower(name) {
		case "github":
			s.Register(&GitHubProvider{config: pc})
		case "google":
			s.Register(&GoogleProvider{config: pc})
		case "discord":
			s.Register(&DiscordProvider{config: pc})
		case "apple":
			s.Register(&AppleProvider{config: pc})
		}
	}

	return s
}

// Register adds a custom provider.
func (s *Socialite) Register(p Provider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.providers[strings.ToLower(p.Name())] = p
}

// Provider returns a registered provider by name.
func (s *Socialite) Provider(name string) (Provider, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.providers[strings.ToLower(name)]
	if !ok {
		return nil, fmt.Errorf("socialite: provider %q not registered", name)
	}
	return p, nil
}

// generateState creates a random state string for CSRF protection.
func generateState() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ── Built-in Providers ──────────────────────────────────────────

// ─── GitHub ─────────────────────────────────────────────────────

type GitHubProvider struct{ config ProviderConfig }

func (p *GitHubProvider) Name() string { return "github" }

func (p *GitHubProvider) AuthURL(state string, scopes []string) string {
	if len(scopes) == 0 {
		scopes = append(p.config.Scopes, "user:email")
		if len(scopes) == 0 {
			scopes = []string{"user:email"}
		}
	}
	v := url.Values{
		"client_id":    {p.config.ClientID},
		"redirect_uri": {p.config.RedirectURL},
		"scope":        {strings.Join(scopes, " ")},
		"state":        {state},
	}
	return "https://github.com/login/oauth/authorize?" + v.Encode()
}

func (p *GitHubProvider) Exchange(ctx context.Context, code string) (*SocialUser, error) {
	// Exchange code for access token
	tokenURL := "https://github.com/login/oauth/access_token"
	data := url.Values{
		"client_id":     {p.config.ClientID},
		"client_secret": {p.config.ClientSecret},
		"code":          {code},
		"redirect_uri":  {p.config.RedirectURL},
	}

	req, _ := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("socialite/github: token exchange failed: %w", err)
	}
	defer resp.Body.Close()

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		Scope       string `json:"scope"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("socialite/github: failed to decode token: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("socialite/github: empty access token")
	}

	// Fetch user info
	return fetchGitHubUser(ctx, tokenResp.AccessToken)
}

func fetchGitHubUser(ctx context.Context, token string) (*SocialUser, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/user", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("socialite/github: user fetch failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var raw map[string]any
	_ = json.Unmarshal(body, &raw)

	id := fmt.Sprintf("%v", raw["id"])
	name, _ := raw["name"].(string)
	if name == "" {
		name, _ = raw["login"].(string)
	}
	email, _ := raw["email"].(string)
	avatar, _ := raw["avatar_url"].(string)

	// If email is private, fetch from /user/emails
	if email == "" {
		email = fetchGitHubEmail(ctx, token)
	}

	return &SocialUser{
		Provider:    "github",
		ID:          id,
		Name:        name,
		Email:       email,
		Avatar:      avatar,
		AccessToken: token,
		Raw:         raw,
	}, nil
}

func fetchGitHubEmail(ctx context.Context, token string) string {
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/user/emails", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&emails); err != nil {
		return ""
	}
	for _, e := range emails {
		if e.Primary && e.Verified {
			return e.Email
		}
	}
	if len(emails) > 0 {
		return emails[0].Email
	}
	return ""
}

// ─── Google ─────────────────────────────────────────────────────

type GoogleProvider struct{ config ProviderConfig }

func (p *GoogleProvider) Name() string { return "google" }

func (p *GoogleProvider) AuthURL(state string, scopes []string) string {
	if len(scopes) == 0 {
		scopes = p.config.Scopes
		if len(scopes) == 0 {
			scopes = []string{"openid", "email", "profile"}
		}
	}
	v := url.Values{
		"client_id":     {p.config.ClientID},
		"redirect_uri":  {p.config.RedirectURL},
		"scope":         {strings.Join(scopes, " ")},
		"state":         {state},
		"response_type": {"code"},
		"access_type":   {"offline"},
	}
	return "https://accounts.google.com/o/oauth2/v2/auth?" + v.Encode()
}

func (p *GoogleProvider) Exchange(ctx context.Context, code string) (*SocialUser, error) {
	tokenURL := "https://oauth2.googleapis.com/token"
	data := url.Values{
		"client_id":     {p.config.ClientID},
		"client_secret": {p.config.ClientSecret},
		"code":          {code},
		"redirect_uri":  {p.config.RedirectURL},
		"grant_type":    {"authorization_code"},
	}

	req, _ := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("socialite/google: token exchange failed: %w", err)
	}
	defer resp.Body.Close()

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("socialite/google: failed to decode token: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("socialite/google: empty access token")
	}

	// Fetch user info
	req2, _ := http.NewRequestWithContext(ctx, "GET", "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	req2.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)

	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		return nil, fmt.Errorf("socialite/google: user fetch failed: %w", err)
	}
	defer resp2.Body.Close()

	body, _ := io.ReadAll(resp2.Body)
	var raw map[string]any
	_ = json.Unmarshal(body, &raw)

	var expiresAt *time.Time
	if tokenResp.ExpiresIn > 0 {
		t := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
		expiresAt = &t
	}

	return &SocialUser{
		Provider:    "google",
		ID:          fmt.Sprintf("%v", raw["id"]),
		Name:        strVal(raw, "name"),
		Email:       strVal(raw, "email"),
		Avatar:      strVal(raw, "picture"),
		AccessToken: tokenResp.AccessToken,
		ExpiresAt:   expiresAt,
		Raw:         raw,
	}, nil
}

// ─── Discord ────────────────────────────────────────────────────

type DiscordProvider struct{ config ProviderConfig }

func (p *DiscordProvider) Name() string { return "discord" }

func (p *DiscordProvider) AuthURL(state string, scopes []string) string {
	if len(scopes) == 0 {
		scopes = p.config.Scopes
		if len(scopes) == 0 {
			scopes = []string{"identify", "email"}
		}
	}
	v := url.Values{
		"client_id":     {p.config.ClientID},
		"redirect_uri":  {p.config.RedirectURL},
		"scope":         {strings.Join(scopes, " ")},
		"state":         {state},
		"response_type": {"code"},
	}
	return "https://discord.com/api/oauth2/authorize?" + v.Encode()
}

func (p *DiscordProvider) Exchange(ctx context.Context, code string) (*SocialUser, error) {
	tokenURL := "https://discord.com/api/oauth2/token"
	data := url.Values{
		"client_id":     {p.config.ClientID},
		"client_secret": {p.config.ClientSecret},
		"code":          {code},
		"redirect_uri":  {p.config.RedirectURL},
		"grant_type":    {"authorization_code"},
	}

	req, _ := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("socialite/discord: token exchange failed: %w", err)
	}
	defer resp.Body.Close()

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("socialite/discord: failed to decode token: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("socialite/discord: empty access token")
	}

	// Fetch user info
	req2, _ := http.NewRequestWithContext(ctx, "GET", "https://discord.com/api/users/@me", nil)
	req2.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)

	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		return nil, fmt.Errorf("socialite/discord: user fetch failed: %w", err)
	}
	defer resp2.Body.Close()

	body, _ := io.ReadAll(resp2.Body)
	var raw map[string]any
	_ = json.Unmarshal(body, &raw)

	id := fmt.Sprintf("%v", raw["id"])
	username := strVal(raw, "username")
	globalName := strVal(raw, "global_name")
	name := globalName
	if name == "" {
		name = username
	}
	email := strVal(raw, "email")

	// Construct avatar URL
	avatar := ""
	if av := strVal(raw, "avatar"); av != "" {
		avatar = fmt.Sprintf("https://cdn.discordapp.com/avatars/%s/%s.png", id, av)
	}

	var expiresAt *time.Time
	if tokenResp.ExpiresIn > 0 {
		t := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
		expiresAt = &t
	}

	return &SocialUser{
		Provider:    "discord",
		ID:          id,
		Name:        name,
		Email:       email,
		Avatar:      avatar,
		AccessToken: tokenResp.AccessToken,
		ExpiresAt:   expiresAt,
		Raw:         raw,
	}, nil
}

// ─── Apple ──────────────────────────────────────────────────────

type AppleProvider struct{ config ProviderConfig }

func (p *AppleProvider) Name() string { return "apple" }

func (p *AppleProvider) AuthURL(state string, scopes []string) string {
	if len(scopes) == 0 {
		scopes = p.config.Scopes
		if len(scopes) == 0 {
			scopes = []string{"name", "email"}
		}
	}
	v := url.Values{
		"client_id":     {p.config.ClientID},
		"redirect_uri":  {p.config.RedirectURL},
		"scope":         {strings.Join(scopes, " ")},
		"state":         {state},
		"response_type": {"code"},
		"response_mode": {"form_post"},
	}
	return "https://appleid.apple.com/auth/authorize?" + v.Encode()
}

func (p *AppleProvider) Exchange(ctx context.Context, code string) (*SocialUser, error) {
	tokenURL := "https://appleid.apple.com/auth/token"
	data := url.Values{
		"client_id":     {p.config.ClientID},
		"client_secret": {p.config.ClientSecret},
		"code":          {code},
		"redirect_uri":  {p.config.RedirectURL},
		"grant_type":    {"authorization_code"},
	}

	req, _ := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("socialite/apple: token exchange failed: %w", err)
	}
	defer resp.Body.Close()

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		IDToken     string `json:"id_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("socialite/apple: failed to decode token: %w", err)
	}

	// Decode the id_token JWT payload (we skip signature validation here;
	// for production Apple requires proper JWT validation).
	claims, err := decodeJWTPayload(tokenResp.IDToken)
	if err != nil {
		return nil, fmt.Errorf("socialite/apple: failed to decode id_token: %w", err)
	}

	var expiresAt *time.Time
	if tokenResp.ExpiresIn > 0 {
		t := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
		expiresAt = &t
	}

	return &SocialUser{
		Provider:    "apple",
		ID:          strVal(claims, "sub"),
		Name:        strVal(claims, "name"),
		Email:       strVal(claims, "email"),
		AccessToken: tokenResp.AccessToken,
		ExpiresAt:   expiresAt,
		Raw:         claims,
	}, nil
}

// ── Helpers ─────────────────────────────────────────────────────

func strVal(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}
