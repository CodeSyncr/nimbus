package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"
)

// Store persists session data. Implementations: MemoryStore, CookieStore, DatabaseStore, RedisStore.
type Store interface {
	// Get retrieves session data by ID. Returns nil map if not found.
	Get(ctx context.Context, id string) (map[string]any, error)
	// Set stores session data. id may be empty for new sessions; returns the session ID.
	Set(ctx context.Context, id string, data map[string]any, maxAge time.Duration) (string, error)
	// Destroy removes the session.
	Destroy(ctx context.Context, id string) error
}

// ErrExpired means an update lost a race with revocation or expiry.
var ErrExpired = errors.New("session: session expired or revoked")

// Updater atomically saves an existing live session. It must never recreate a
// missing or expired ID. Middleware requires this capability for existing IDs.
type Updater interface {
	Update(ctx context.Context, id string, data map[string]any, maxAge time.Duration) (string, error)
}

// Replacer atomically replaces a live server-side record, optionally changing
// its ID. Used by encrypted cookie registries to consume the old fingerprint
// and publish its replacement in one operation. Return ErrExpired if absent.
type Replacer interface {
	Replace(ctx context.Context, oldID, newID string, data map[string]any, maxAge time.Duration) error
}

// Rotator atomically changes a live session's ID, as required by Regenerate.
type Rotator interface {
	Rotate(ctx context.Context, id string, data map[string]any, maxAge time.Duration) (string, error)
}

func newSessionID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func rotateRecord(ctx context.Context, s Replacer, id string, data map[string]any, maxAge time.Duration) (string, error) {
	next, err := newSessionID()
	if err != nil {
		return "", err
	}
	if err := s.Replace(ctx, id, next, data, maxAge); err != nil {
		return "", err
	}
	return next, nil
}
