package session

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

// Config holds session middleware options.
//
// Cookie security attributes are secure-by-default: the session cookie is
// always written with HttpOnly, and with SameSite=Lax unless overridden. The
// Secure flag is set automatically whenever the request is served over HTTPS
// (TLS or X-Forwarded-Proto: https); set Secure=true to force it on even
// behind a proxy that does not forward the scheme.
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
	// Secure-by-default attributes: a session id cookie is never meant to be
	// read by JavaScript, and Lax SameSite blocks the common CSRF vectors.
	cfg.HttpOnly = true
	if cfg.SameSite == 0 {
		cfg.SameSite = SameSiteLax
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
			c.Session = &Session{sd: sd}

			// Wrap the response writer so the session cookie is added
			// BEFORE Go's WriteHeader sends response headers on the wire.
			// Once WriteHeader fires, subsequent header mutations are
			// silently ignored, so we must persist the session just
			// before that call.
			sw := &sessionWriter{
				ResponseWriter: c.Response,
				sd:             sd,
				cfg:            cfg,
				secure:         cfg.Secure || isHTTPS(c.Request),
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
	sd     *sessionData
	cfg    Config
	secure bool
	saved  bool
}

// isHTTPS reports whether the request was served over TLS, either directly or
// via a trusted proxy that set X-Forwarded-Proto.
func isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// persistSession saves dirty session data to the store and adds the
// Set-Cookie header. Safe to call multiple times; only acts once.
func (sw *sessionWriter) persistSession() {
	if sw.saved {
		return
	}
	sw.saved = true
	sw.sd.mu.Lock()
	dirty := sw.sd.dirty
	data := sw.sd.data
	id := sw.sd.id
	staleID := sw.sd.staleID
	sw.sd.staleID = ""
	sw.sd.mu.Unlock()
	if !dirty {
		return
	}
	newID, err := sw.sd.config.Store.Set(
		context.Background(), id, data, sw.sd.config.MaxAge,
	)
	if err != nil {
		return
	}
	if newID != "" {
		sw.sd.mu.Lock()
		sw.sd.id = newID
		id = newID
		sw.sd.mu.Unlock()
	}
	// Destroy the pre-regeneration session so a fixed/leaked old id cannot be
	// replayed after login. Only destroy once we have a distinct new id.
	if staleID != "" && staleID != id {
		_ = sw.sd.config.Store.Destroy(context.Background(), staleID)
	}
	http.SetCookie(sw.ResponseWriter, &http.Cookie{
		Name:     sw.cfg.CookieName,
		Value:    id,
		Path:     "/",
		MaxAge:   int(sw.cfg.MaxAge.Seconds()),
		HttpOnly: sw.cfg.HttpOnly,
		Secure:   sw.secure,
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
	mu      sync.Mutex
	id      string
	staleID string // previous id pending destruction after Regenerate
	data    map[string]any
	store   Store
	config  Config
	dirty   bool
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
	s.sd.mu.Lock()
	defer s.sd.mu.Unlock()
	return s.sd.data[key]
}

// Set stores a value in the session.
func (s *Session) Set(key string, val any) {
	if s == nil || s.sd == nil {
		return
	}
	s.sd.mu.Lock()
	defer s.sd.mu.Unlock()
	s.sd.data[key] = val
	s.sd.dirty = true
}

// Delete removes a key from the session.
func (s *Session) Delete(key string) {
	if s == nil || s.sd == nil {
		return
	}
	s.sd.mu.Lock()
	defer s.sd.mu.Unlock()
	delete(s.sd.data, key)
	s.sd.dirty = true
}

// Regenerate regenerates the session ID (for security after login).
func (s *Session) Regenerate() {
	if s == nil || s.sd == nil {
		return
	}
	s.sd.mu.Lock()
	defer s.sd.mu.Unlock()
	// Remember the current id so it can be destroyed in the store once the new
	// id is issued, preventing session fixation via the pre-login id.
	if s.sd.id != "" {
		s.sd.staleID = s.sd.id
	}
	s.sd.id = ""
	s.sd.dirty = true
}

// GetFlash retrieves a value from the session and deletes it immediately.
func (s *Session) GetFlash(key string) any {
	if s == nil || s.sd == nil {
		return nil
	}
	s.sd.mu.Lock()
	defer s.sd.mu.Unlock()
	flash, ok := s.sd.data["_flash"].(map[string]any)
	if !ok {
		return nil
	}
	val := flash[key]
	delete(flash, key)
	s.sd.dirty = true
	return val
}

// SetFlash stores a value in the session that will be deleted after the next GetFlash.
func (s *Session) SetFlash(key string, val any) {
	if s == nil || s.sd == nil {
		return
	}
	s.sd.mu.Lock()
	defer s.sd.mu.Unlock()
	flash, ok := s.sd.data["_flash"].(map[string]any)
	if !ok {
		flash = make(map[string]any)
		s.sd.data["_flash"] = flash
	}
	flash[key] = val
	s.sd.dirty = true
}
