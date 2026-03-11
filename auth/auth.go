package auth

import (
	"context"
	"sync"

	"github.com/CodeSyncr/nimbus/session"
)

// User is the authenticated user interface (apps implement this).
type User interface {
	GetID() string
}

// UserLoader loads a user by ID (e.g. from database).
type UserLoader interface {
	LoadUser(ctx context.Context, id string) (User, error)
}

// UserLoaderFunc adapts a function to UserLoader.
type UserLoaderFunc func(ctx context.Context, id string) (User, error)

func (f UserLoaderFunc) LoadUser(ctx context.Context, id string) (User, error) {
	return f(ctx, id)
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

const sessionUserKey = "user_id"

// SessionGuard uses the session store to persist user ID. Requires session.Middleware to run first.
// Use NewSessionGuardWithLoader for production (loads user from DB). NewSessionGuard keeps in-memory for backward compat.
type SessionGuard struct {
	mu       sync.RWMutex
	sessions map[string]User
	loader   UserLoader
}

// NewSessionGuard returns an in-memory session guard (backward compatible). Sessions lost on restart.
func NewSessionGuard() *SessionGuard {
	return &SessionGuard{sessions: make(map[string]User)}
}

// NewSessionGuardWithLoader returns a guard that uses session store + user loader (persistent auth).
func NewSessionGuardWithLoader(loader UserLoader) *SessionGuard {
	return &SessionGuard{loader: loader, sessions: make(map[string]User)}
}

// User returns the user from session. With loader: loads from DB via session user_id. Without loader: uses in-memory map (legacy, keyed by session_id).
func (g *SessionGuard) User(ctx context.Context) (User, error) {
	sess := session.FromContext(ctx)
	if sess != nil {
		userID, _ := sess.Get(sessionUserKey).(string)
		if userID != "" {
			if g.loader != nil {
				return g.loader.LoadUser(ctx, userID)
			}
			g.mu.RLock()
			u := g.sessions[userID]
			g.mu.RUnlock()
			return u, nil
		}
	}
	// Legacy: session_id from context (no session middleware)
	sid, _ := ctx.Value("session_id").(string)
	if sid != "" {
		g.mu.RLock()
		u := g.sessions[sid]
		g.mu.RUnlock()
		return u, nil
	}
	return nil, nil
}

// Login stores user in session (or in-memory for legacy).
func (g *SessionGuard) Login(ctx context.Context, user User) error {
	sess := session.FromContext(ctx)
	if sess != nil {
		sess.Regenerate()
		sess.Set(sessionUserKey, user.GetID())
		if g.loader == nil {
			g.mu.Lock()
			g.sessions[user.GetID()] = user
			g.mu.Unlock()
		}
		return nil
	}
	sid, _ := ctx.Value("session_id").(string)
	if sid != "" {
		g.mu.Lock()
		g.sessions[sid] = user
		g.mu.Unlock()
	}
	return nil
}

// Logout removes the user from session.
func (g *SessionGuard) Logout(ctx context.Context) error {
	sess := session.FromContext(ctx)
	if sess != nil {
		userID, _ := sess.Get(sessionUserKey).(string)
		sess.Delete(sessionUserKey)
		if userID != "" && g.loader == nil {
			g.mu.Lock()
			delete(g.sessions, userID)
			g.mu.Unlock()
		}
		return nil
	}
	sid, _ := ctx.Value("session_id").(string)
	if sid != "" {
		g.mu.Lock()
		delete(g.sessions, sid)
		g.mu.Unlock()
	}
	return nil
}
