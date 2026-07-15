package passport

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"testing"

	"github.com/CodeSyncr/nimbus/lucid"
	"github.com/CodeSyncr/nimbus/plugins/passport/models"
	"gorm.io/driver/sqlite"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	db, err := lucid.Open(sqlite.Open("file::memory:?cache=shared"), &lucid.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range models.Migrations() {
		if err := m.Up(db); err != nil {
			t.Fatalf("migrate %s: %v", m.Name, err)
		}
	}
	return NewServer(db, Config{})
}

func TestAuthorizationCodeGrant_WithPKCE(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()

	res, err := s.CreateClient(ctx, "Web App", "owner1",
		[]string{"https://app.test/callback"},
		[]string{GrantAuthorizationCode, GrantRefreshToken},
		[]string{"read:profile", "write:profile"}, false) // public client → PKCE required
	if err != nil {
		t.Fatal(err)
	}
	clientID := res.Client.ClientID

	// PKCE pair.
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	// Public client with no challenge must be rejected.
	if _, err := s.ValidateAuthorize(ctx, clientID, "code", "https://app.test/callback", "read:profile", "xyz", "", "S256"); err != ErrInvalidRequest {
		t.Fatalf("expected PKCE-required rejection, got %v", err)
	}

	req, err := s.ValidateAuthorize(ctx, clientID, "code", "https://app.test/callback", "read:profile", "xyz", challenge, "S256")
	if err != nil {
		t.Fatalf("validate authorize: %v", err)
	}
	code, err := s.IssueAuthCode(ctx, req, "user42")
	if err != nil {
		t.Fatal(err)
	}

	// Wrong verifier fails.
	if _, err := s.ExchangeAuthCode(ctx, clientID, "", code, "https://app.test/callback", "wrong-verifier"); err != ErrInvalidGrant {
		t.Fatalf("expected invalid_grant on bad PKCE, got %v", err)
	}

	// Re-issue (previous code was consumed on the failed attempt? No — only on success). Issue a fresh code.
	code2, _ := s.IssueAuthCode(ctx, req, "user42")
	tok, err := s.ExchangeAuthCode(ctx, clientID, "", code2, "https://app.test/callback", verifier)
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if tok.AccessToken == "" || tok.RefreshToken == "" || tok.TokenType != "Bearer" {
		t.Fatalf("token response: %+v", tok)
	}

	// Single-use: replaying the code fails.
	if _, err := s.ExchangeAuthCode(ctx, clientID, "", code2, "https://app.test/callback", verifier); err != ErrInvalidGrant {
		t.Fatalf("expected replay rejection, got %v", err)
	}

	// Access token validates and carries scope.
	at, err := s.ValidateAccessToken(ctx, tok.AccessToken)
	if err != nil || at.UserID != "user42" || !at.HasScope("read:profile") {
		t.Fatalf("validate access token: at=%+v err=%v", at, err)
	}
	if at.HasScope("write:profile") {
		t.Fatal("token should not have unrequested scope")
	}
}

func TestRefreshTokenGrant_Rotates(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	res, _ := s.CreateClient(ctx, "Conf App", "owner",
		[]string{"https://app.test/cb"},
		[]string{GrantAuthorizationCode, GrantRefreshToken},
		[]string{"*"}, true) // confidential

	req, _ := s.ValidateAuthorize(ctx, res.Client.ClientID, "code", "https://app.test/cb", "read", "", "", "")
	code, _ := s.IssueAuthCode(ctx, req, "u1")
	tok, err := s.ExchangeAuthCode(ctx, res.Client.ClientID, res.PlainSecret, code, "https://app.test/cb", "")
	if err != nil {
		t.Fatal(err)
	}

	// Refresh with the confidential client secret.
	newTok, err := s.Refresh(ctx, res.Client.ClientID, res.PlainSecret, tok.RefreshToken, "")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if newTok.AccessToken == tok.AccessToken {
		t.Fatal("access token not rotated")
	}
	// Old refresh token is revoked (rotation).
	if _, err := s.Refresh(ctx, res.Client.ClientID, res.PlainSecret, tok.RefreshToken, ""); err != ErrInvalidGrant {
		t.Fatalf("old refresh token should be revoked, got %v", err)
	}
	// Old access token is revoked.
	if _, err := s.ValidateAccessToken(ctx, tok.AccessToken); err != ErrInvalidGrant {
		t.Fatalf("old access token should be revoked, got %v", err)
	}
}

func TestClientCredentialsGrant(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	res, _ := s.CreateClient(ctx, "Machine", "", nil,
		[]string{GrantClientCredentials}, []string{"api:read"}, true)

	tok, err := s.ClientCredentials(ctx, res.Client.ClientID, res.PlainSecret, "api:read")
	if err != nil {
		t.Fatalf("client_credentials: %v", err)
	}
	if tok.RefreshToken != "" {
		t.Fatal("client_credentials must not return a refresh token")
	}
	// Bad secret rejected.
	if _, err := s.ClientCredentials(ctx, res.Client.ClientID, "wrong", "api:read"); err != ErrInvalidClient {
		t.Fatalf("expected invalid_client, got %v", err)
	}
	// Unpermitted scope rejected.
	if _, err := s.ClientCredentials(ctx, res.Client.ClientID, res.PlainSecret, "api:write"); err != ErrInvalidScope {
		t.Fatalf("expected invalid_scope, got %v", err)
	}
}

func TestAuthorize_RejectsBadRedirectAndScope(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	res, _ := s.CreateClient(ctx, "App", "o",
		[]string{"https://ok.test/cb"},
		[]string{GrantAuthorizationCode}, []string{"read"}, true)
	id := res.Client.ClientID

	if _, err := s.ValidateAuthorize(ctx, id, "code", "https://evil.test/cb", "read", "", "", ""); err != ErrInvalidRequest {
		t.Fatalf("expected redirect rejection, got %v", err)
	}
	if _, err := s.ValidateAuthorize(ctx, id, "code", "https://ok.test/cb", "admin", "", "", ""); err != ErrInvalidScope {
		t.Fatalf("expected scope rejection, got %v", err)
	}
	if _, err := s.ValidateAuthorize(ctx, "nope", "code", "https://ok.test/cb", "read", "", "", ""); err != ErrInvalidClient {
		t.Fatalf("expected client rejection, got %v", err)
	}
}

func TestRevokeAccessToken(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	res, _ := s.CreateClient(ctx, "M", "", nil, []string{GrantClientCredentials}, []string{"*"}, true)
	tok, _ := s.ClientCredentials(ctx, res.Client.ClientID, res.PlainSecret, "")
	if err := s.RevokeAccessToken(ctx, tok.AccessToken); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ValidateAccessToken(ctx, tok.AccessToken); err != ErrInvalidGrant {
		t.Fatalf("revoked token should be invalid, got %v", err)
	}
}
