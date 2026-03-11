package locale

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

var (
	mu            sync.RWMutex
	translations  = make(map[string]map[string]string) // locale -> key -> message
	defaultLocale = "en"
	ctxKey        = struct{}{}
)

// SetDefault sets the default locale (used by T when no context is available).
func SetDefault(loc string) {
	mu.Lock()
	defer mu.Unlock()
	if loc != "" {
		defaultLocale = loc
	}
}

// AddTranslations registers translation strings for a locale.
func AddTranslations(loc string, msgs map[string]string) {
	if loc == "" || len(msgs) == 0 {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	m, ok := translations[loc]
	if !ok {
		m = make(map[string]string)
		translations[loc] = m
	}
	for k, v := range msgs {
		m[k] = v
	}
}

// T returns the translated string for key using the default locale.
// If not found, the key itself is returned.
func T(key string, args ...any) string {
	return TLocale(defaultLocale, key, args...)
}

// TLocale returns the translated string for key in the given locale.
func TLocale(loc, key string, args ...any) string {
	mu.RLock()
	defer mu.RUnlock()
	if loc == "" {
		loc = defaultLocale
	}
	if msgs, ok := translations[loc]; ok {
		if msg, ok := msgs[key]; ok {
			if len(args) > 0 {
				return fmt.Sprintf(msg, args...)
			}
			return msg
		}
	}
	// Fallback to key.
	if len(args) > 0 {
		return fmt.Sprintf(key, args...)
	}
	return key
}

// WithLocale stores the locale in context.
func WithLocale(ctx context.Context, loc string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, ctxKey, loc)
}

// FromContext returns the locale stored in context, or empty string.
func FromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(ctxKey).(string); ok {
		return v
	}
	return ""
}

// Middleware inspects Accept-Language and sets the locale on the request context.
// It stores the value in both request.Context() and reqctx.Context for downstream use.
func Middleware() router.Middleware {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *http.Context) error {
			loc := parseAcceptLanguage(c.Request)
			if loc != "" {
				ctx := WithLocale(c.Request.Context(), loc)
				c.Request = c.Request.WithContext(ctx)
			}
			return next(c)
		}
	}
}

func parseAcceptLanguage(r *http.Request) string {
	h := r.Header.Get("Accept-Language")
	if h == "" {
		return ""
	}
	parts := strings.Split(h, ",")
	if len(parts) == 0 {
		return ""
	}
	// Take first locale without quality suffix (e.g. en-US;q=0.9).
	main := strings.TrimSpace(parts[0])
	if idx := strings.Index(main, ";"); idx >= 0 {
		main = main[:idx]
	}
	if main == "" {
		return ""
	}
	return main
}
