package passport

import "time"

// Config tunes the OAuth2 server. Zero values fall back to sane defaults.
type Config struct {
	// RoutePrefix is where the OAuth endpoints mount. Default "/oauth".
	RoutePrefix string

	// AccessTokenTTL is how long issued access tokens live. Default 1h.
	AccessTokenTTL time.Duration
	// RefreshTokenTTL is how long refresh tokens live. Default 30 days.
	RefreshTokenTTL time.Duration
	// AuthCodeTTL is how long an authorization code is valid. Default 10m.
	AuthCodeTTL time.Duration

	// ConsentView is the .nimbus template rendered on the consent screen.
	// Default "oauth-authorize" (bundled). It receives: client_name, scopes
	// ([]string), and the raw authorize query params for the approve form.
	ConsentView string

	// AllowPublicClientsWithoutPKCE, when true, lets public clients run the
	// authorization_code grant without a code_challenge. The default (false)
	// enforces PKCE for public clients, matching OAuth 2.1.
	AllowPublicClientsWithoutPKCE bool
}

func (c *Config) withDefaults() {
	if c.RoutePrefix == "" {
		c.RoutePrefix = "/oauth"
	}
	if c.AccessTokenTTL == 0 {
		c.AccessTokenTTL = time.Hour
	}
	if c.RefreshTokenTTL == 0 {
		c.RefreshTokenTTL = 30 * 24 * time.Hour
	}
	if c.AuthCodeTTL == 0 {
		c.AuthCodeTTL = 10 * time.Minute
	}
	if c.ConsentView == "" {
		c.ConsentView = "passport/oauth-authorize"
	}
}
