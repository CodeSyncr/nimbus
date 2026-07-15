package supabase

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// ═══════════════════════════════════════════════════════════════════
// Supabase Auth — GoTrue REST API client for server-side auth
// ═══════════════════════════════════════════════════════════════════

// AuthClient wraps the Supabase GoTrue API.
type AuthClient struct {
	client *Client
}

// AuthUser represents a Supabase user.
type AuthUser struct {
	ID               string         `json:"id"`
	Email            string         `json:"email"`
	Phone            string         `json:"phone,omitempty"`
	Role             string         `json:"role,omitempty"`
	EmailConfirmedAt string         `json:"email_confirmed_at,omitempty"`
	CreatedAt        string         `json:"created_at,omitempty"`
	UpdatedAt        string         `json:"updated_at,omitempty"`
	AppMetadata      map[string]any `json:"app_metadata,omitempty"`
	UserMetadata     map[string]any `json:"user_metadata,omitempty"`
}

// AuthSession contains tokens returned after authentication.
type AuthSession struct {
	AccessToken  string   `json:"access_token"`
	TokenType    string   `json:"token_type"`
	ExpiresIn    int      `json:"expires_in"`
	ExpiresAt    int64    `json:"expires_at,omitempty"`
	RefreshToken string   `json:"refresh_token"`
	User         AuthUser `json:"user"`
}

// SignUpRequest is the payload for email/password sign-up.
type SignUpRequest struct {
	Email    string         `json:"email"`
	Password string        `json:"password"`
	Data     map[string]any `json:"data,omitempty"`
}

// SignInRequest is the payload for email/password sign-in.
type SignInRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// OTPRequest is the payload for magic link / OTP sign-in.
type OTPRequest struct {
	Email string `json:"email,omitempty"`
	Phone string `json:"phone,omitempty"`
}

// UpdateUserRequest is the payload for updating user attributes.
type UpdateUserRequest struct {
	Email    string         `json:"email,omitempty"`
	Password string        `json:"password,omitempty"`
	Phone    string         `json:"phone,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
}

// ResetPasswordRequest is the payload for password reset.
type ResetPasswordRequest struct {
	Email string `json:"email"`
}

// RefreshTokenRequest is the payload for refreshing tokens.
type RefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// InviteRequest is the payload for admin user invitation.
type InviteRequest struct {
	Email string         `json:"email"`
	Data  map[string]any `json:"data,omitempty"`
}

// AdminUserListResponse is the response from admin list users.
type AdminUserListResponse struct {
	Users []AuthUser `json:"users"`
}

// AdminUpdateUserRequest is the payload for admin user update.
type AdminUpdateUserRequest struct {
	Email        string         `json:"email,omitempty"`
	Password     string         `json:"password,omitempty"`
	Phone        string         `json:"phone,omitempty"`
	UserMetadata map[string]any `json:"user_metadata,omitempty"`
	AppMetadata  map[string]any `json:"app_metadata,omitempty"`
	BanDuration  string         `json:"ban_duration,omitempty"`
}

func (a *AuthClient) authURL(path string) string {
	return a.client.url + "/auth/v1" + path
}

// SignUp registers a new user with email and password.
//
//	session, err := client.Auth.SignUp(supabase.SignUpRequest{
//	    Email: "user@example.com", Password: "secret123",
//	})
func (a *AuthClient) SignUp(req SignUpRequest) (*AuthSession, error) {
	resp, err := a.client.doJSON("POST", a.authURL("/signup"), req)
	if err != nil {
		return nil, err
	}
	session, err := decodeJSON[AuthSession](resp)
	if err != nil {
		return nil, fmt.Errorf("supabase: auth signup: %w", err)
	}
	return &session, nil
}

// SignInWithPassword authenticates with email and password.
//
//	session, err := client.Auth.SignInWithPassword(supabase.SignInRequest{
//	    Email: "user@example.com", Password: "secret123",
//	})
func (a *AuthClient) SignInWithPassword(req SignInRequest) (*AuthSession, error) {
	resp, err := a.client.doJSON("POST", a.authURL("/token?grant_type=password"), req)
	if err != nil {
		return nil, err
	}
	session, err := decodeJSON[AuthSession](resp)
	if err != nil {
		return nil, fmt.Errorf("supabase: auth signin: %w", err)
	}
	return &session, nil
}

// SignInWithOTP sends a magic link or OTP to the user.
func (a *AuthClient) SignInWithOTP(req OTPRequest) error {
	resp, err := a.client.doJSON("POST", a.authURL("/otp"), req)
	if err != nil {
		return err
	}
	return drainResp(resp)
}

// SignOut invalidates the user's session.
func (a *AuthClient) SignOut(accessToken string) error {
	resp, err := a.client.do("POST", a.authURL("/logout"), nil, map[string]string{
		"Authorization": "Bearer " + accessToken,
	})
	if err != nil {
		return err
	}
	return drainResp(resp)
}

// GetUser retrieves the user associated with the given access token.
func (a *AuthClient) GetUser(accessToken string) (*AuthUser, error) {
	resp, err := a.client.do("GET", a.authURL("/user"), nil, map[string]string{
		"Authorization": "Bearer " + accessToken,
	})
	if err != nil {
		return nil, err
	}
	user, err := decodeJSON[AuthUser](resp)
	if err != nil {
		return nil, fmt.Errorf("supabase: auth get user: %w", err)
	}
	return &user, nil
}

// UpdateUser updates the currently authenticated user.
func (a *AuthClient) UpdateUser(accessToken string, req UpdateUserRequest) (*AuthUser, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("supabase: auth update marshal: %w", err)
	}
	resp, err := a.client.do("PUT", a.authURL("/user"), bytes.NewReader(b), map[string]string{
		"Authorization": "Bearer " + accessToken,
		"Content-Type":  "application/json",
	})
	if err != nil {
		return nil, err
	}
	user, err := decodeJSON[AuthUser](resp)
	if err != nil {
		return nil, fmt.Errorf("supabase: auth update user: %w", err)
	}
	return &user, nil
}

// ResetPasswordForEmail sends a password reset email.
func (a *AuthClient) ResetPasswordForEmail(email string) error {
	resp, err := a.client.doJSON("POST", a.authURL("/recover"), ResetPasswordRequest{Email: email})
	if err != nil {
		return err
	}
	return drainResp(resp)
}

// RefreshToken exchanges a refresh token for new access/refresh tokens.
func (a *AuthClient) RefreshToken(refreshToken string) (*AuthSession, error) {
	resp, err := a.client.doJSON("POST", a.authURL("/token?grant_type=refresh_token"),
		RefreshTokenRequest{RefreshToken: refreshToken})
	if err != nil {
		return nil, err
	}
	session, err := decodeJSON[AuthSession](resp)
	if err != nil {
		return nil, fmt.Errorf("supabase: auth refresh: %w", err)
	}
	return &session, nil
}

// ── Admin Methods (require ServiceRoleKey) ──────────────────────

// AdminListUsers lists all users (requires service role key).
func (a *AuthClient) AdminListUsers(page, perPage int) ([]AuthUser, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 50
	}
	url := fmt.Sprintf("%s?page=%d&per_page=%d", a.authURL("/admin/users"), page, perPage)
	resp, err := a.client.do("GET", url, nil, map[string]string{
		"Authorization": "Bearer " + a.client.serviceRoleKey,
	})
	if err != nil {
		return nil, err
	}
	result, err := decodeJSON[AdminUserListResponse](resp)
	if err != nil {
		return nil, fmt.Errorf("supabase: admin list users: %w", err)
	}
	return result.Users, nil
}

// AdminGetUser retrieves a user by ID (requires service role key).
func (a *AuthClient) AdminGetUser(userID string) (*AuthUser, error) {
	resp, err := a.client.do("GET", a.authURL("/admin/users/"+userID), nil, map[string]string{
		"Authorization": "Bearer " + a.client.serviceRoleKey,
	})
	if err != nil {
		return nil, err
	}
	user, err := decodeJSON[AuthUser](resp)
	if err != nil {
		return nil, fmt.Errorf("supabase: admin get user: %w", err)
	}
	return &user, nil
}

// AdminUpdateUser updates a user by ID (requires service role key).
func (a *AuthClient) AdminUpdateUser(userID string, req AdminUpdateUserRequest) (*AuthUser, error) {
	resp, err := a.client.doJSON("PUT", a.authURL("/admin/users/"+userID), req)
	if err != nil {
		return nil, err
	}
	user, err := decodeJSON[AuthUser](resp)
	if err != nil {
		return nil, fmt.Errorf("supabase: admin update user: %w", err)
	}
	return &user, nil
}

// AdminDeleteUser deletes a user by ID (requires service role key).
func (a *AuthClient) AdminDeleteUser(userID string) error {
	resp, err := a.client.do("DELETE", a.authURL("/admin/users/"+userID), nil, map[string]string{
		"Authorization": "Bearer " + a.client.serviceRoleKey,
	})
	if err != nil {
		return err
	}
	return drainResp(resp)
}

// InviteByEmail sends an invitation email (requires service role key).
func (a *AuthClient) InviteByEmail(req InviteRequest) (*AuthUser, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("supabase: auth invite marshal: %w", err)
	}
	resp, err := a.client.do("POST", a.authURL("/invite"), io.NopCloser(bytes.NewReader(b)), map[string]string{
		"Authorization": "Bearer " + a.client.serviceRoleKey,
		"Content-Type":  "application/json",
	})
	if err != nil {
		return nil, err
	}
	user, err := decodeJSON[AuthUser](resp)
	if err != nil {
		return nil, fmt.Errorf("supabase: auth invite: %w", err)
	}
	return &user, nil
}

// GenerateLink creates an admin action link (confirm, recovery, etc.).
func (a *AuthClient) GenerateLink(linkType, email string, options map[string]any) (map[string]any, error) {
	payload := map[string]any{
		"type":  linkType,
		"email": email,
	}
	for k, v := range options {
		payload[k] = v
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("supabase: auth generate link marshal: %w", err)
	}
	resp, err := a.client.do("POST", a.authURL("/admin/generate_link"), bytes.NewReader(b), map[string]string{
		"Authorization": "Bearer " + a.client.serviceRoleKey,
		"Content-Type":  "application/json",
	})
	if err != nil {
		return nil, err
	}
	result, err := decodeJSON[map[string]any](resp)
	if err != nil {
		return nil, fmt.Errorf("supabase: auth generate link: %w", err)
	}
	return result, nil
}

// ── JWT Helpers ──────────────────────────────────────────────────

// JWTClaims contains decoded Supabase JWT claims.
type JWTClaims struct {
	Sub       string         `json:"sub"`
	Email     string         `json:"email"`
	Phone     string         `json:"phone,omitempty"`
	Role      string         `json:"role"`
	Aud       string         `json:"aud"`
	Exp       int64          `json:"exp"`
	Iat       int64          `json:"iat"`
	Iss       string         `json:"iss"`
	AppMetadata  map[string]any `json:"app_metadata,omitempty"`
	UserMetadata map[string]any `json:"user_metadata,omitempty"`
}

// IsExpired returns true if the token has expired.
func (c *JWTClaims) IsExpired() bool {
	return time.Now().Unix() > c.Exp
}

// UserID returns the subject (user ID) from the claims.
func (c *JWTClaims) UserID() string {
	return c.Sub
}
