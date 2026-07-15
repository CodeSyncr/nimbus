package passport

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/CodeSyncr/nimbus/lucid"
	"github.com/CodeSyncr/nimbus/plugins/passport/models"
)

// Grant type constants (RFC 6749 + PKCE).
const (
	GrantAuthorizationCode = "authorization_code"
	GrantClientCredentials = "client_credentials"
	GrantRefreshToken      = "refresh_token"
)

// OAuth errors surfaced to callers and mapped to RFC 6749 error codes by routes.go.
var (
	ErrInvalidClient    = errors.New("invalid_client")
	ErrInvalidGrant     = errors.New("invalid_grant")
	ErrInvalidScope     = errors.New("invalid_scope")
	ErrUnauthorized     = errors.New("unauthorized_client")
	ErrUnsupportedGrant = errors.New("unsupported_grant_type")
	ErrInvalidRequest   = errors.New("invalid_request")
)

// Server is the OAuth2 authorization server. Resolve it from the container
// ("passport") or hold the plugin's instance.
type Server struct {
	db  *lucid.DB
	cfg Config
}

// NewServer builds an OAuth2 server over the given database.
func NewServer(db *lucid.DB, cfg Config) *Server {
	cfg.withDefaults()
	return &Server{db: db, cfg: cfg}
}

// TokenResponse is the RFC 6749 token endpoint success body.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"` // always "Bearer"
	ExpiresIn    int    `json:"expires_in"` // seconds
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// ── Client management ──────────────────────────────────────────────

// NewClientResult carries the plaintext secret (shown once) plus the record.
type NewClientResult struct {
	PlainSecret string             `json:"secret"`
	Client      models.OAuthClient `json:"client"`
}

// CreateClient registers an OAuth client. For a confidential client a random
// secret is generated and returned once (stored hashed). Pass confidential=false
// for public clients (no secret; PKCE required on the auth-code grant).
func (s *Server) CreateClient(ctx context.Context, name, ownerUserID string, redirects, grants, scopes []string, confidential bool) (*NewClientResult, error) {
	id, err := randomToken(16)
	if err != nil {
		return nil, err
	}
	c := models.OAuthClient{
		ClientID:     id,
		Name:         name,
		UserID:       ownerUserID,
		RedirectURIs: strings.Join(redirects, " "),
		Grants:       strings.Join(grants, " "),
		Scopes:       strings.Join(scopes, " "),
		Confidential: confidential,
	}
	var plain string
	if confidential {
		plain, err = randomToken(32)
		if err != nil {
			return nil, err
		}
		c.Secret = hashToken(plain)
	}
	if err := s.db.WithContext(ctx).Create(&c).Error; err != nil {
		return nil, fmt.Errorf("passport: create client: %w", err)
	}
	return &NewClientResult{PlainSecret: plain, Client: c}, nil
}

// findClient loads a non-revoked client by its public client_id.
func (s *Server) findClient(ctx context.Context, clientID string) (*models.OAuthClient, error) {
	var c models.OAuthClient
	err := s.db.WithContext(ctx).Where("client_id = ? AND revoked = ?", clientID, false).First(&c).Error
	if err != nil {
		if errors.Is(err, lucid.ErrRecordNotFound) {
			return nil, ErrInvalidClient
		}
		return nil, err
	}
	return &c, nil
}

// authenticateClient validates client_id + secret for confidential clients.
// Public clients (no stored secret) authenticate by client_id alone.
func (s *Server) authenticateClient(ctx context.Context, clientID, secret string) (*models.OAuthClient, error) {
	c, err := s.findClient(ctx, clientID)
	if err != nil {
		return nil, err
	}
	if c.Confidential {
		if secret == "" || subtle.ConstantTimeCompare([]byte(hashToken(secret)), []byte(c.Secret)) != 1 {
			return nil, ErrInvalidClient
		}
	}
	return c, nil
}

// ── Authorization code issuance (from /oauth/authorize approval) ───

// AuthorizeRequest captures a validated /oauth/authorize request.
type AuthorizeRequest struct {
	Client              *models.OAuthClient
	RedirectURI         string
	Scopes              []string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
}

// ValidateAuthorize checks an incoming authorization request: client exists,
// response_type=code, redirect is allowlisted, scopes are permitted, and PKCE
// is present when required. It does NOT issue a code (the user must approve first).
func (s *Server) ValidateAuthorize(ctx context.Context, clientID, responseType, redirectURI, scope, state, challenge, method string) (*AuthorizeRequest, error) {
	if responseType != "code" {
		return nil, ErrUnsupportedGrant
	}
	c, err := s.findClient(ctx, clientID)
	if err != nil {
		return nil, err
	}
	if !c.AllowsGrant(GrantAuthorizationCode) {
		return nil, ErrUnauthorized
	}
	if redirectURI == "" || !c.AllowsRedirect(redirectURI) {
		return nil, ErrInvalidRequest
	}
	scopes := strings.Fields(scope)
	for _, sc := range scopes {
		if !c.AllowsScope(sc) {
			return nil, ErrInvalidScope
		}
	}
	if method == "" {
		method = "plain"
	}
	if !c.Confidential && challenge == "" && !s.cfg.AllowPublicClientsWithoutPKCE {
		return nil, ErrInvalidRequest // PKCE required for public clients
	}
	if challenge != "" && method != "plain" && method != "S256" {
		return nil, ErrInvalidRequest
	}
	return &AuthorizeRequest{
		Client: c, RedirectURI: redirectURI, Scopes: scopes, State: state,
		CodeChallenge: challenge, CodeChallengeMethod: method,
	}, nil
}

// IssueAuthCode persists a single-use authorization code for an approved
// request and returns the plaintext code to redirect back to the client.
func (s *Server) IssueAuthCode(ctx context.Context, req *AuthorizeRequest, userID string) (string, error) {
	plain, err := randomToken(32)
	if err != nil {
		return "", err
	}
	code := models.OAuthAuthCode{
		Code:                hashToken(plain),
		ClientID:            req.Client.ClientID,
		UserID:              userID,
		Scopes:              strings.Join(req.Scopes, " "),
		RedirectURI:         req.RedirectURI,
		CodeChallenge:       req.CodeChallenge,
		CodeChallengeMethod: req.CodeChallengeMethod,
		ExpiresAt:           time.Now().Add(s.cfg.AuthCodeTTL),
	}
	if err := s.db.WithContext(ctx).Create(&code).Error; err != nil {
		return "", fmt.Errorf("passport: issue code: %w", err)
	}
	return plain, nil
}

// ── Token endpoint grants ──────────────────────────────────────────

// ExchangeAuthCode runs the authorization_code grant: validates the code,
// client, redirect, and PKCE verifier, then issues access + refresh tokens.
func (s *Server) ExchangeAuthCode(ctx context.Context, clientID, clientSecret, code, redirectURI, codeVerifier string) (*TokenResponse, error) {
	client, err := s.authenticateClient(ctx, clientID, clientSecret)
	if err != nil {
		return nil, err
	}
	var ac models.OAuthAuthCode
	err = s.db.WithContext(ctx).Where("code = ?", hashToken(code)).First(&ac).Error
	if err != nil {
		if errors.Is(err, lucid.ErrRecordNotFound) {
			return nil, ErrInvalidGrant
		}
		return nil, err
	}
	if ac.Used || ac.IsExpired() || ac.ClientID != client.ClientID || ac.RedirectURI != redirectURI {
		return nil, ErrInvalidGrant
	}
	if !verifyPKCE(ac.CodeChallenge, ac.CodeChallengeMethod, codeVerifier) {
		return nil, ErrInvalidGrant
	}
	// Single-use: mark consumed before issuing.
	if err := s.db.WithContext(ctx).Model(&ac).Update("used", true).Error; err != nil {
		return nil, err
	}
	return s.issueTokens(ctx, client.ClientID, ac.UserID, strings.Fields(ac.Scopes), true)
}

// ClientCredentials runs the client_credentials grant (machine-to-machine).
func (s *Server) ClientCredentials(ctx context.Context, clientID, clientSecret, scope string) (*TokenResponse, error) {
	client, err := s.authenticateClient(ctx, clientID, clientSecret)
	if err != nil {
		return nil, err
	}
	if !client.Confidential {
		return nil, ErrUnauthorized // public clients cannot use client_credentials
	}
	if !client.AllowsGrant(GrantClientCredentials) {
		return nil, ErrUnauthorized
	}
	scopes := strings.Fields(scope)
	for _, sc := range scopes {
		if !client.AllowsScope(sc) {
			return nil, ErrInvalidScope
		}
	}
	// No refresh token for client_credentials (RFC 6749 §4.4.3).
	return s.issueTokens(ctx, client.ClientID, "", scopes, false)
}

// Refresh runs the refresh_token grant, rotating the refresh token.
func (s *Server) Refresh(ctx context.Context, clientID, clientSecret, refreshToken, scope string) (*TokenResponse, error) {
	client, err := s.authenticateClient(ctx, clientID, clientSecret)
	if err != nil {
		return nil, err
	}
	var rt models.OAuthRefreshToken
	err = s.db.WithContext(ctx).Where("token = ?", hashToken(refreshToken)).First(&rt).Error
	if err != nil {
		if errors.Is(err, lucid.ErrRecordNotFound) {
			return nil, ErrInvalidGrant
		}
		return nil, err
	}
	if rt.Revoked || rt.IsExpired() {
		return nil, ErrInvalidGrant
	}
	var old models.OAuthAccessToken
	if err := s.db.WithContext(ctx).First(&old, rt.AccessTokenID).Error; err != nil {
		return nil, ErrInvalidGrant
	}
	if old.ClientID != client.ClientID {
		return nil, ErrInvalidGrant
	}
	// Optionally narrow scopes; never widen them.
	scopes := old.ScopeList()
	if scope != "" {
		requested := strings.Fields(scope)
		for _, sc := range requested {
			if !old.HasScope(sc) {
				return nil, ErrInvalidScope
			}
		}
		scopes = requested
	}
	// Rotate: revoke the old access + refresh tokens, then issue a new pair.
	s.db.WithContext(ctx).Model(&old).Update("revoked", true)
	s.db.WithContext(ctx).Model(&rt).Update("revoked", true)
	return s.issueTokens(ctx, client.ClientID, old.UserID, scopes, true)
}

// issueTokens creates an access token (and optionally a refresh token).
func (s *Server) issueTokens(ctx context.Context, clientID, userID string, scopes []string, withRefresh bool) (*TokenResponse, error) {
	plainAccess, err := randomToken(40)
	if err != nil {
		return nil, err
	}
	at := models.OAuthAccessToken{
		Token:     hashToken(plainAccess),
		ClientID:  clientID,
		UserID:    userID,
		Scopes:    strings.Join(scopes, " "),
		ExpiresAt: time.Now().Add(s.cfg.AccessTokenTTL),
	}
	if err := s.db.WithContext(ctx).Create(&at).Error; err != nil {
		return nil, fmt.Errorf("passport: issue access token: %w", err)
	}
	resp := &TokenResponse{
		AccessToken: plainAccess,
		TokenType:   "Bearer",
		ExpiresIn:   int(s.cfg.AccessTokenTTL.Seconds()),
		Scope:       strings.Join(scopes, " "),
	}
	if withRefresh {
		plainRefresh, err := randomToken(40)
		if err != nil {
			return nil, err
		}
		rt := models.OAuthRefreshToken{
			Token:         hashToken(plainRefresh),
			AccessTokenID: at.ID,
			ExpiresAt:     time.Now().Add(s.cfg.RefreshTokenTTL),
		}
		if err := s.db.WithContext(ctx).Create(&rt).Error; err != nil {
			return nil, fmt.Errorf("passport: issue refresh token: %w", err)
		}
		resp.RefreshToken = plainRefresh
	}
	return resp, nil
}

// ── Resource-server validation ─────────────────────────────────────

// ValidateAccessToken resolves a plaintext bearer token to a live, non-revoked,
// non-expired access-token record. Returns ErrInvalidGrant otherwise.
func (s *Server) ValidateAccessToken(ctx context.Context, plain string) (*models.OAuthAccessToken, error) {
	var at models.OAuthAccessToken
	err := s.db.WithContext(ctx).Where("token = ?", hashToken(plain)).First(&at).Error
	if err != nil {
		if errors.Is(err, lucid.ErrRecordNotFound) {
			return nil, ErrInvalidGrant
		}
		return nil, err
	}
	if at.Revoked || at.IsExpired() {
		return nil, ErrInvalidGrant
	}
	return &at, nil
}

// RevokeAccessToken revokes a token by its plaintext value (RFC 7009).
func (s *Server) RevokeAccessToken(ctx context.Context, plain string) error {
	return s.db.WithContext(ctx).Model(&models.OAuthAccessToken{}).
		Where("token = ?", hashToken(plain)).Update("revoked", true).Error
}

// ── crypto helpers ─────────────────────────────────────────────────

// randomToken returns a hex-encoded cryptographically random token of n bytes.
func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("passport: rng: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// hashToken returns the SHA-256 hex digest used for at-rest storage.
func hashToken(plain string) string {
	h := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(h[:])
}

// verifyPKCE validates a code_verifier against the stored challenge.
// When no challenge was recorded, PKCE is not in play and this passes.
func verifyPKCE(challenge, method, verifier string) bool {
	if challenge == "" {
		return true
	}
	if verifier == "" {
		return false
	}
	switch method {
	case "S256":
		sum := sha256.Sum256([]byte(verifier))
		computed := base64.RawURLEncoding.EncodeToString(sum[:])
		return subtle.ConstantTimeCompare([]byte(computed), []byte(challenge)) == 1
	default: // "plain"
		return subtle.ConstantTimeCompare([]byte(verifier), []byte(challenge)) == 1
	}
}
