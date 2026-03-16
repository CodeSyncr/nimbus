package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/CodeSyncr/nimbus/mail"
)

// ── Email Verification ──────────────────────────────────────────

// MustVerifyEmail should be implemented by User models that require email verification.
type MustVerifyEmail interface {
	User
	GetEmail() string
	HasVerifiedEmail() bool
	MarkEmailAsVerified() error
}

// EmailVerifierStore looks up users for the verification flow.
type EmailVerifierStore interface {
	FindByID(ctx context.Context, id string) (MustVerifyEmail, error)
}

// EmailVerifier orchestrates the email-verification flow.
type EmailVerifier struct {
	tokens    *TokenStore
	store     EmailVerifierStore
	mailer    mail.Driver
	fromAddr  string
	verifyURL string // base URL; token appended as ?id=xxx&token=yyy
}

// NewEmailVerifier creates a verifier.
//   - secret: HMAC key (use APP_KEY).
//   - ttl: token validity (e.g. 24*time.Hour).
//   - verifyURL: your verification endpoint, e.g. "https://app.com/verify-email".
func NewEmailVerifier(secret string, ttl time.Duration, store EmailVerifierStore, mailer mail.Driver, fromAddr, verifyURL string) *EmailVerifier {
	return &EmailVerifier{
		tokens:    NewTokenStore(secret, ttl),
		store:     store,
		mailer:    mailer,
		fromAddr:  fromAddr,
		verifyURL: verifyURL,
	}
}

// SendVerification generates a token and sends the verification email.
func (v *EmailVerifier) SendVerification(ctx context.Context, user MustVerifyEmail) error {
	if user.HasVerifiedEmail() {
		return nil // already verified
	}

	token, err := v.tokens.Create(user.GetID())
	if err != nil {
		return err
	}

	link := fmt.Sprintf("%s?id=%s&token=%s", v.verifyURL, user.GetID(), token)
	msg := &mail.Message{
		From:    v.fromAddr,
		To:      []string{user.GetEmail()},
		Subject: "Verify Your Email Address",
		HTML:    true,
		Body: fmt.Sprintf(`
			<h2>Email Verification</h2>
			<p>Please verify your email address by clicking the link below:</p>
			<p><a href="%s">Verify Email</a></p>
			<p>This link will expire in %d hours.</p>
			<p>If you didn't create an account, no further action is required.</p>
		`, link, int(v.tokens.ttl.Hours())),
	}
	return v.mailer.Send(msg)
}

// Verify checks the token and marks the user's email as verified.
func (v *EmailVerifier) Verify(ctx context.Context, userID, token string) error {
	user, err := v.store.FindByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("email verification: %w", err)
	}
	if user == nil {
		return fmt.Errorf("email verification: user not found")
	}
	if user.HasVerifiedEmail() {
		return nil // idempotent
	}

	if !v.tokens.Verify(userID, token) {
		return fmt.Errorf("email verification: invalid or expired token")
	}

	return user.MarkEmailAsVerified()
}

// Cleanup removes expired tokens. Call via scheduler.
func (v *EmailVerifier) Cleanup() {
	v.tokens.Cleanup()
}

// RequireVerifiedEmail middleware rejects requests from unverified users.
// Must be used after RequireAuth middleware.
func RequireVerifiedEmail(redirectTo string) func(next func(*MustVerifyEmail)) {
	// This is a conceptual middleware; apps should use it with their HandlerFunc.
	// The actual middleware is provided as a pattern:
	//
	//   func VerifiedMiddleware(next router.HandlerFunc) router.HandlerFunc {
	//       return func(c *http.Context) error {
	//           user := auth.UserFromContext(c.Request.Context())
	//           if u, ok := user.(auth.MustVerifyEmail); ok && !u.HasVerifiedEmail() {
	//               return c.Redirect(http.StatusFound, "/verify-email")
	//           }
	//           return next(c)
	//       }
	//   }
	return nil
}
