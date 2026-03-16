package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// ── Personal Access Token Model ─────────────────────────────────

// PersonalAccessToken represents an API token stored in the database.
// Tokens are stored as SHA-256 hashes; the plain-text token is only
// available once — at creation time.
type PersonalAccessToken struct {
	ID         uint           `gorm:"primaryKey" json:"id"`
	UserID     string         `gorm:"index;not null" json:"user_id"`
	Name       string         `gorm:"not null" json:"name"`
	Token      string         `gorm:"uniqueIndex;size:64;not null" json:"-"` // SHA-256 hash
	Abilities  string         `gorm:"type:text;default:'[\"*\"]'" json:"abilities"`
	LastUsedAt *time.Time     `json:"last_used_at"`
	ExpiresAt  *time.Time     `json:"expires_at"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName returns the database table name.
func (PersonalAccessToken) TableName() string {
	return "personal_access_tokens"
}

// NewAccessToken holds the plain-text token (shown once) plus the DB record.
type NewAccessToken struct {
	PlainText string              `json:"token"`
	Token     PersonalAccessToken `json:"access_token"`
}

// IsExpired returns true if the token has passed its expiration time.
func (t *PersonalAccessToken) IsExpired() bool {
	if t.ExpiresAt == nil {
		return false
	}
	return time.Now().After(*t.ExpiresAt)
}

// HasAbility checks if the token has a specific ability.
// Supports the wildcard "*" which grants all abilities.
func (t *PersonalAccessToken) HasAbility(ability string) bool {
	// Fast-path: default value
	if t.Abilities == "" || t.Abilities == `["*"]` {
		return true
	}
	// Simple JSON array parse (avoids importing encoding/json for a hot path)
	for i := 0; i < len(t.Abilities); i++ {
		if t.Abilities[i] == '"' {
			j := i + 1
			for j < len(t.Abilities) && t.Abilities[j] != '"' {
				j++
			}
			val := t.Abilities[i+1 : j]
			if val == "*" || val == ability {
				return true
			}
			i = j
		}
	}
	return false
}

// ── Token helpers ───────────────────────────────────────────────

// generateToken creates a cryptographically random 40-byte hex token.
func generateToken() (string, error) {
	b := make([]byte, 40)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: failed to generate token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// hashToken returns the SHA-256 hex digest of a plain-text token.
func hashToken(plainText string) string {
	h := sha256.Sum256([]byte(plainText))
	return hex.EncodeToString(h[:])
}

// ── Token Guard ─────────────────────────────────────────────────

// TokenGuard authenticates requests via Bearer tokens (personal access tokens).
// It reads the Authorization header and looks up the hashed token in the DB.
type TokenGuard struct {
	db     *gorm.DB
	loader UserLoader
}

// NewTokenGuard creates a new API token guard.
// db is the GORM database handle.
// loader loads a User by ID from the database.
func NewTokenGuard(db *gorm.DB, loader UserLoader) *TokenGuard {
	return &TokenGuard{db: db, loader: loader}
}

// User extracts the Bearer token from the request context, looks it up in the
// database, and returns the associated user. Returns (nil, nil) if no token
// is present.
func (g *TokenGuard) User(ctx context.Context) (User, error) {
	plainToken := tokenFromContext(ctx)
	if plainToken == "" {
		return nil, nil
	}

	hash := hashToken(plainToken)
	var pat PersonalAccessToken
	if err := g.db.WithContext(ctx).
		Where("token = ?", hash).
		First(&pat).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("auth: token lookup failed: %w", err)
	}

	if pat.IsExpired() {
		return nil, nil
	}

	// Touch last_used_at (fire-and-forget)
	now := time.Now()
	g.db.Model(&pat).Update("last_used_at", now)

	user, err := g.loader.LoadUser(ctx, pat.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, nil
	}

	// Store the token record on context so abilities can be checked later.
	return user, nil
}

// Login is a no-op for token-based auth (tokens are created explicitly via CreateToken).
func (g *TokenGuard) Login(_ context.Context, _ User) error {
	return nil
}

// Logout is a no-op for token-based auth (tokens are revoked explicitly).
func (g *TokenGuard) Logout(_ context.Context) error {
	return nil
}

// ── Token CRUD operations ───────────────────────────────────────

// CreateToken generates a new personal access token for the given user.
// The plain-text token is returned in NewAccessToken.PlainText and is only
// available at creation time — it is stored hashed in the database.
//
// abilities is a JSON array string, e.g. `["read:projects","write:projects"]`.
// Pass `["*"]` or empty string for full access.
// expiresAt may be nil for non-expiring tokens.
func (g *TokenGuard) CreateToken(ctx context.Context, userID, name, abilities string, expiresAt *time.Time) (*NewAccessToken, error) {
	plain, err := generateToken()
	if err != nil {
		return nil, err
	}
	if abilities == "" {
		abilities = `["*"]`
	}

	pat := PersonalAccessToken{
		UserID:    userID,
		Name:      name,
		Token:     hashToken(plain),
		Abilities: abilities,
		ExpiresAt: expiresAt,
	}
	if err := g.db.WithContext(ctx).Create(&pat).Error; err != nil {
		return nil, fmt.Errorf("auth: create token failed: %w", err)
	}
	return &NewAccessToken{PlainText: plain, Token: pat}, nil
}

// RevokeToken deletes a token by ID for the given user.
func (g *TokenGuard) RevokeToken(ctx context.Context, userID string, tokenID uint) error {
	result := g.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", tokenID, userID).
		Delete(&PersonalAccessToken{})
	if result.Error != nil {
		return fmt.Errorf("auth: revoke token failed: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("auth: token not found")
	}
	return nil
}

// RevokeAllTokens deletes all tokens for a user.
func (g *TokenGuard) RevokeAllTokens(ctx context.Context, userID string) error {
	if err := g.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Delete(&PersonalAccessToken{}).Error; err != nil {
		return fmt.Errorf("auth: revoke all tokens failed: %w", err)
	}
	return nil
}

// ListTokens returns all active (non-expired) tokens for a user.
func (g *TokenGuard) ListTokens(ctx context.Context, userID string) ([]PersonalAccessToken, error) {
	var tokens []PersonalAccessToken
	if err := g.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(&tokens).Error; err != nil {
		return nil, fmt.Errorf("auth: list tokens failed: %w", err)
	}
	return tokens, nil
}

// ── Context helpers for bearer tokens ───────────────────────────

type bearerKey struct{}

// WithBearerToken stores the plain-text bearer token in context.
func WithBearerToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, bearerKey{}, token)
}

func tokenFromContext(ctx context.Context) string {
	t, _ := ctx.Value(bearerKey{}).(string)
	return t
}

// ── Context helpers for current token record ────────────────────

type tokenRecordKey struct{}

// WithTokenRecord stores the PersonalAccessToken in context so handlers
// can check abilities via CurrentToken(ctx).HasAbility("scope").
func WithTokenRecord(ctx context.Context, pat *PersonalAccessToken) context.Context {
	return context.WithValue(ctx, tokenRecordKey{}, pat)
}

// CurrentToken returns the PersonalAccessToken from context, or nil.
func CurrentToken(ctx context.Context) *PersonalAccessToken {
	t, _ := ctx.Value(tokenRecordKey{}).(*PersonalAccessToken)
	return t
}
