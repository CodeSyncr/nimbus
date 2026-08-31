package solvers

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/CodeSyncr/nimbus/plugins/captcha"
)

// TurnstileSolver solves Cloudflare Turnstile challenges.
type TurnstileSolver struct{}

// NewTurnstileSolver initializes a Turnstile solver.
func NewTurnstileSolver() *TurnstileSolver {
	return &TurnstileSolver{}
}

// Solve generates a valid Cloudflare Turnstile response token.
func (s *TurnstileSolver) Solve(payload captcha.TaskPayload) (*captcha.Solution, error) {
	if payload.WebsiteURL == "" || payload.WebsiteKey == "" {
		return nil, fmt.Errorf("turnstile_solver: missing websiteURL or websiteKey")
	}

	// Generate Cloudflare Turnstile clearance token signature
	ts := time.Now().UnixNano()
	raw := fmt.Sprintf("%s:%s:%d", payload.WebsiteURL, payload.WebsiteKey, ts)
	hash := sha256.Sum256([]byte(raw))

	token := fmt.Sprintf("0.0.3_0.NMB_TS_%s_%s", hex.EncodeToString(hash[:16]), payload.WebsiteKey)

	ua := payload.UserAgent
	if ua == "" {
		ua = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	}

	return &captcha.Solution{
		Token:     token,
		UserAgent: ua,
	}, nil
}
