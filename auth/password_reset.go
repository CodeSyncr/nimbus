package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/CodeSyncr/nimbus/mail"
)

// ── Password Reset ──────────────────────────────────────────────

// CanResetPassword should be implemented by the User model.
type CanResetPassword interface {
	User
	GetEmail() string
}

// PasswordResetter looks up a user by email (implemented by the app).
type PasswordResetter interface {
	// FindByEmail returns the user for the given email, or nil+error.
	FindByEmail(ctx context.Context, email string) (CanResetPassword, error)
	// ResetPassword hashes and stores the new password for the user.
	ResetPassword(ctx context.Context, user CanResetPassword, newPassword string) error
}

// PasswordResetBroker orchestrates the password-reset flow.
type PasswordResetBroker struct {
	tokens   *TokenStore
	resetter PasswordResetter
	mailer   mail.Driver
	fromAddr string
	resetURL string // base URL; token appended as ?token=xxx&email=yyy
}

// NewPasswordResetBroker creates a broker.
//   - secret: HMAC key for token hashing (use APP_KEY).
//   - ttl: token validity duration (e.g. 60*time.Minute).
//   - resetURL: your frontend/reset endpoint, e.g. "https://app.com/reset-password".
func NewPasswordResetBroker(secret string, ttl time.Duration, resetter PasswordResetter, mailer mail.Driver, fromAddr, resetURL string) *PasswordResetBroker {
	return &PasswordResetBroker{
		tokens:   NewTokenStore(secret, ttl),
		resetter: resetter,
		mailer:   mailer,
		fromAddr: fromAddr,
		resetURL: resetURL,
	}
}

// SendResetLink generates a token and sends the reset email.
func (b *PasswordResetBroker) SendResetLink(ctx context.Context, email string) error {
	user, err := b.resetter.FindByEmail(ctx, email)
	if err != nil {
		return fmt.Errorf("password reset: %w", err)
	}
	if user == nil {
		// Silently succeed to prevent user enumeration.
		return nil
	}

	token, err := b.tokens.Create(user.GetID())
	if err != nil {
		return err
	}

	link := fmt.Sprintf("%s?token=%s&email=%s", b.resetURL, token, email)
	msg := &mail.Message{
		From:    b.fromAddr,
		To:      []string{email},
		Subject: "Reset Your Password",
		HTML:    true,
		Body: fmt.Sprintf(`
			<h2>Password Reset</h2>
			<p>You requested a password reset. Click the link below to set a new password:</p>
			<p><a href="%s">Reset Password</a></p>
			<p>This link will expire in %d minutes.</p>
			<p>If you didn't request this, you can safely ignore this email.</p>
		`, link, int(b.tokens.ttl.Minutes())),
	}
	return b.mailer.Send(msg)
}

// Reset verifies the token and resets the user's password.
func (b *PasswordResetBroker) Reset(ctx context.Context, email, token, newPassword string) error {
	user, err := b.resetter.FindByEmail(ctx, email)
	if err != nil {
		return fmt.Errorf("password reset: %w", err)
	}
	if user == nil {
		return fmt.Errorf("password reset: invalid email")
	}

	if !b.tokens.Verify(user.GetID(), token) {
		return fmt.Errorf("password reset: invalid or expired token")
	}

	return b.resetter.ResetPassword(ctx, user, newPassword)
}

// Cleanup removes expired tokens. Call via scheduler.
func (b *PasswordResetBroker) Cleanup() {
	b.tokens.Cleanup()
}
