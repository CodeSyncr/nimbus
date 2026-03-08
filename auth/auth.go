package auth

import (
	"context"
	"sync"
)

// User is the authenticated user interface (apps implement this).
type User interface {
	GetID() string
}

// Guard authenticates requests and returns the current user (plan: auth:web, auth:api).
type Guard interface {
	User(ctx context.Context) (User, error)
	Login(ctx context.Context, user User) error
	Logout(ctx context.Context) error
}

// key type for context.
type key struct{}

var userKey = key{}

// WithUser sets the user in the request context (used by guards after auth).
func WithUser(ctx context.Context, user User) context.Context {
	return context.WithValue(ctx, userKey, user)
}

// UserFromContext returns the authenticated user from context, or nil.
func UserFromContext(ctx context.Context) User {
	u, _ := ctx.Value(userKey).(User)
	return u
}

// SessionGuard is a simple in-memory session guard (plan: session auth).
type SessionGuard struct {
	mu      sync.RWMutex
	sessions map[string]User
}

// NewSessionGuard returns a session-based guard (session ID -> user).
func NewSessionGuard() *SessionGuard {
	return &SessionGuard{sessions: make(map[string]User)}
}

// User returns the user for the session ID from context (set by middleware that reads cookie).
func (g *SessionGuard) User(ctx context.Context) (User, error) {
	sid, _ := ctx.Value("session_id").(string)
	if sid == "" {
		return nil, nil
	}
	g.mu.RLock()
	u := g.sessions[sid]
	g.mu.RUnlock()
	return u, nil
}

// Login stores user under session ID (call after setting session cookie).
func (g *SessionGuard) Login(ctx context.Context, user User) error {
	sid, _ := ctx.Value("session_id").(string)
	if sid != "" {
		g.mu.Lock()
		g.sessions[sid] = user
		g.mu.Unlock()
	}
	return nil
}

// Logout removes the session.
func (g *SessionGuard) Logout(ctx context.Context) error {
	sid, _ := ctx.Value("session_id").(string)
	if sid != "" {
		g.mu.Lock()
		delete(g.sessions, sid)
		g.mu.Unlock()
	}
	return nil
}
