// Package models holds the persisted Passport (OAuth2 server) tables:
// clients, authorization codes, access tokens, and refresh tokens.
//
// Secrets and tokens are stored as SHA-256 hashes — plaintext values are
// returned to the caller only once, at creation time.
package models

import (
	"strings"
	"time"

	"github.com/CodeSyncr/nimbus/database"
)

// ── Client ────────────────────────────────────────────────────────

// OAuthClient is a registered application allowed to request tokens.
// Confidential clients authenticate with a secret; public clients (SPAs,
// mobile, CLIs) have no secret and MUST use PKCE on the authorization_code grant.
type OAuthClient struct {
	database.Model
	ClientID     string `json:"client_id" gorm:"uniqueIndex;size:64;not null"`
	Secret       string `json:"-" gorm:"size:64"`                 // SHA-256 hash; empty for public clients
	Name         string `json:"name" gorm:"not null"`
	UserID       string `json:"user_id" gorm:"index"`             // owner (first-party clients may leave empty)
	RedirectURIs string `json:"redirect_uris" gorm:"type:text"`   // space-separated allowlist
	Grants       string `json:"grants" gorm:"type:text"`          // space-separated: authorization_code refresh_token client_credentials
	Scopes       string `json:"scopes" gorm:"type:text"`          // space-separated allowed scopes ("*" = any)
	Confidential bool   `json:"confidential"` // false = public (PKCE required); always set explicitly by CreateClient
	FirstParty   bool   `json:"first_party"`                      // skip the consent screen
	Revoked      bool   `json:"revoked" gorm:"index"`
}

func (OAuthClient) TableName() string { return "oauth_clients" }

// RedirectList splits the stored redirect allowlist.
func (c *OAuthClient) RedirectList() []string { return fields(c.RedirectURIs) }

// GrantList splits the stored allowed grants.
func (c *OAuthClient) GrantList() []string { return fields(c.Grants) }

// AllowsGrant reports whether the client may use a grant type.
func (c *OAuthClient) AllowsGrant(grant string) bool { return contains(c.GrantList(), grant) }

// AllowsRedirect reports whether uri exactly matches an allowlisted redirect.
func (c *OAuthClient) AllowsRedirect(uri string) bool { return contains(c.RedirectList(), uri) }

// AllowsScope reports whether the client is permitted to request a scope.
func (c *OAuthClient) AllowsScope(scope string) bool {
	list := fields(c.Scopes)
	return len(list) == 0 || contains(list, "*") || contains(list, scope)
}

// ── Authorization code ────────────────────────────────────────────

// OAuthAuthCode is a short-lived, single-use code issued from /oauth/authorize
// and exchanged at /oauth/token for tokens.
type OAuthAuthCode struct {
	database.Model
	Code                string    `json:"-" gorm:"uniqueIndex;size:64;not null"` // SHA-256 hash
	ClientID            string    `json:"client_id" gorm:"index;not null"`
	UserID              string    `json:"user_id" gorm:"index;not null"`
	Scopes              string    `json:"scopes" gorm:"type:text"`
	RedirectURI         string    `json:"redirect_uri"`
	CodeChallenge       string    `json:"-"`             // PKCE challenge
	CodeChallengeMethod string    `json:"-"`             // "S256" or "plain"
	Used                bool      `json:"used" gorm:"index"`
	ExpiresAt           time.Time `json:"expires_at"`
}

func (OAuthAuthCode) TableName() string { return "oauth_auth_codes" }

// IsExpired reports whether the code has passed its expiry.
func (a *OAuthAuthCode) IsExpired() bool { return time.Now().After(a.ExpiresAt) }

// ── Access & refresh tokens ───────────────────────────────────────

// OAuthAccessToken is an issued bearer token. Stored hashed; opaque to clients.
// UserID is empty for client_credentials tokens (machine-to-machine).
type OAuthAccessToken struct {
	database.Model
	Token     string    `json:"-" gorm:"uniqueIndex;size:64;not null"` // SHA-256 hash
	ClientID  string    `json:"client_id" gorm:"index;not null"`
	UserID    string    `json:"user_id" gorm:"index"`
	Name      string    `json:"name"`
	Scopes    string    `json:"scopes" gorm:"type:text"`
	Revoked   bool      `json:"revoked" gorm:"index"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (OAuthAccessToken) TableName() string { return "oauth_access_tokens" }

// IsExpired reports whether the access token has passed its expiry.
func (t *OAuthAccessToken) IsExpired() bool { return time.Now().After(t.ExpiresAt) }

// ScopeList splits the granted scopes.
func (t *OAuthAccessToken) ScopeList() []string { return fields(t.Scopes) }

// HasScope reports whether the token was granted a scope ("*" grants all).
func (t *OAuthAccessToken) HasScope(scope string) bool {
	list := t.ScopeList()
	return len(list) == 0 || contains(list, "*") || contains(list, scope)
}

// OAuthRefreshToken lets a client obtain a fresh access token without the user.
type OAuthRefreshToken struct {
	database.Model
	Token         string    `json:"-" gorm:"uniqueIndex;size:64;not null"` // SHA-256 hash
	AccessTokenID uint      `json:"access_token_id" gorm:"index;not null"`
	Revoked       bool      `json:"revoked" gorm:"index"`
	ExpiresAt     time.Time `json:"expires_at"`
}

func (OAuthRefreshToken) TableName() string { return "oauth_refresh_tokens" }

// IsExpired reports whether the refresh token has passed its expiry.
func (t *OAuthRefreshToken) IsExpired() bool { return time.Now().After(t.ExpiresAt) }

// ── helpers ───────────────────────────────────────────────────────

func fields(s string) []string { return strings.Fields(s) }

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
