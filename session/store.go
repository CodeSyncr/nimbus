package session

import (
	"context"
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
