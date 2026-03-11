package session

import (
	"context"
	"time"

	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

// Config holds session middleware options.
type Config struct {
	Store      Store
	CookieName string
	MaxAge     time.Duration
	HttpOnly   bool
	Secure     bool
	SameSite   http.SameSite
}

// SameSite values.
const (
	SameSiteLax    = http.SameSiteLaxMode
	SameSiteStrict = http.SameSiteStrictMode
	SameSiteNone   = http.SameSiteNoneMode
)

// contextKey is the key for session data in request context.
type contextKey struct{}

var sessionKey = contextKey{}

// Middleware returns middleware that loads the session from the store and sets it on the request context.
// If no session cookie exists, a new session is created on first write.
func Middleware(cfg Config) router.Middleware {
	if cfg.MaxAge == 0 {
		cfg.MaxAge = 7 * 24 * time.Hour
	}
	if cfg.CookieName == "" {
		cfg.CookieName = "nimbus_session"
	}
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *http.Context) error {
			cookie, _ := c.Request.Cookie(cfg.CookieName)
			var sid string
			if cookie != nil {
				sid = cookie.Value
			}
			data, _ := cfg.Store.Get(c.Request.Context(), sid)
			if data == nil {
				data = make(map[string]any)
			}
			ctx := context.WithValue(c.Request.Context(), sessionKey, &sessionData{
				id:     sid,
				data:   data,
				store:  cfg.Store,
				config: cfg,
				dirty:  false,
			})
			c.Request = c.Request.WithContext(ctx)
			err := next(c)
			// Save session if dirty (after response)
			sd, _ := c.Request.Context().Value(sessionKey).(*sessionData)
			if sd != nil && sd.dirty {
				newID, _ := cfg.Store.Set(c.Request.Context(), sd.id, sd.data, cfg.MaxAge)
				if newID != "" {
					sd.id = newID
				}
				cookie := &http.Cookie{
					Name:     cfg.CookieName,
					Value:    sd.id,
					Path:     "/",
					MaxAge:   int(cfg.MaxAge.Seconds()),
					HttpOnly: cfg.HttpOnly,
					Secure:   cfg.Secure,
					SameSite: cfg.SameSite,
				}
				http.SetCookie(c.Response, cookie)
			}
			return err
		}
	}
}

type sessionData struct {
	id     string
	data   map[string]any
	store  Store
	config Config
	dirty  bool
}

// FromContext returns the session data from the request context.
// Returns nil if session middleware was not used.
func FromContext(ctx context.Context) *Session {
	sd, _ := ctx.Value(sessionKey).(*sessionData)
	if sd == nil {
		return nil
	}
	return &Session{sd: sd}
}

// Session provides access to session data.
type Session struct {
	sd *sessionData
}

// Get returns a value from the session.
func (s *Session) Get(key string) any {
	if s == nil || s.sd == nil {
		return nil
	}
	return s.sd.data[key]
}

// Set stores a value in the session.
func (s *Session) Set(key string, val any) {
	if s == nil || s.sd == nil {
		return
	}
	s.sd.data[key] = val
	s.sd.dirty = true
}

// Delete removes a key from the session.
func (s *Session) Delete(key string) {
	if s == nil || s.sd == nil {
		return
	}
	delete(s.sd.data, key)
	s.sd.dirty = true
}

// Regenerate regenerates the session ID (for security after login).
func (s *Session) Regenerate() {
	if s == nil || s.sd == nil {
		return
	}
	s.sd.id = ""
	s.sd.dirty = true
}
