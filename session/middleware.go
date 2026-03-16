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
			sd := &sessionData{
				id:     sid,
				data:   data,
				store:  cfg.Store,
				config: cfg,
				dirty:  false,
			}
			ctx := context.WithValue(c.Request.Context(), sessionKey, sd)
			c.Request = c.Request.WithContext(ctx)

			// Wrap the response writer so the session cookie is added
			// BEFORE Go's WriteHeader sends response headers on the wire.
			// Once WriteHeader fires, subsequent header mutations are
			// silently ignored, so we must persist the session just
			// before that call.
			sw := &sessionWriter{
				ResponseWriter: c.Response,
				sd:             sd,
				cfg:            cfg,
			}
			c.Response = sw

			err := next(c)

			// Safety net: if the handler never wrote a response (no
			// WriteHeader/Write call), persist the session now.
			sw.persistSession()

			return err
		}
	}
}

// sessionWriter intercepts WriteHeader and Write so the session cookie can
// be added to the response headers just before they are flushed to the wire.
type sessionWriter struct {
	http.ResponseWriter
	sd    *sessionData
	cfg   Config
	saved bool
}

// persistSession saves dirty session data to the store and adds the
// Set-Cookie header. Safe to call multiple times; only acts once.
func (sw *sessionWriter) persistSession() {
	if sw.saved {
		return
	}
	sw.saved = true
	if sw.sd == nil || !sw.sd.dirty {
		return
	}
	newID, _ := sw.sd.config.Store.Set(
		context.Background(), sw.sd.id, sw.sd.data, sw.sd.config.MaxAge,
	)
	if newID != "" {
		sw.sd.id = newID
	}
	http.SetCookie(sw.ResponseWriter, &http.Cookie{
		Name:     sw.cfg.CookieName,
		Value:    sw.sd.id,
		Path:     "/",
		MaxAge:   int(sw.cfg.MaxAge.Seconds()),
		HttpOnly: sw.cfg.HttpOnly,
		Secure:   sw.cfg.Secure,
		SameSite: sw.cfg.SameSite,
	})
}

func (sw *sessionWriter) WriteHeader(code int) {
	sw.persistSession()
	sw.ResponseWriter.WriteHeader(code)
}

func (sw *sessionWriter) Write(b []byte) (int, error) {
	sw.persistSession()
	return sw.ResponseWriter.Write(b)
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
