/*
|--------------------------------------------------------------------------
| Transmit Channel Authorization
|--------------------------------------------------------------------------
|
| Restrict access to private channels. Patterns use :param syntax.
|   Authorize("users/:id", func(ctx, params) bool { return ctx.Auth.UserID == params["id"] })
|
*/

package transmit

import (
	"regexp"
	"strings"
	"sync"

	reqctx "github.com/CodeSyncr/nimbus/http"
)

// AuthorizeFunc returns true to allow subscription, false to deny.
type AuthorizeFunc func(ctx *reqctx.Context, params map[string]string) bool

type authRule struct {
	pattern *regexp.Regexp
	params  []string
	fn      AuthorizeFunc
}

var (
	authRules   []authRule
	authRulesMu sync.RWMutex
)

// Authorize registers an authorization callback for channels matching pattern.
// Pattern uses :param syntax (e.g. "users/:id", "chats/:chatId/messages").
// Return true to allow, false to deny. Channels without a matching rule are public.
func Authorize(pattern string, fn AuthorizeFunc) {
	authRulesMu.Lock()
	defer authRulesMu.Unlock()
	re, params := patternToRegex(pattern)
	authRules = append(authRules, authRule{pattern: re, params: params, fn: fn})
}

func patternToRegex(pattern string) (*regexp.Regexp, []string) {
	parts := strings.Split(pattern, "/")
	var params []string
	for i, p := range parts {
		if strings.HasPrefix(p, ":") {
			name := strings.TrimPrefix(p, ":")
			params = append(params, name)
			parts[i] = `([^/]+)`
		} else {
			parts[i] = regexp.QuoteMeta(p)
		}
	}
	re := regexp.MustCompile("^" + strings.Join(parts, "/") + "$")
	return re, params
}

// CheckChannel returns true if the client may subscribe to channel.
func CheckChannel(ctx *reqctx.Context, channel string) bool {
	authRulesMu.RLock()
	rules := authRules
	authRulesMu.RUnlock()
	for _, r := range rules {
		if m := r.pattern.FindStringSubmatch(channel); m != nil {
			params := make(map[string]string)
			for i, name := range r.params {
				if i+1 < len(m) {
					params[name] = m[i+1]
				}
			}
			if !r.fn(ctx, params) {
				return false
			}
			return true
		}
	}
	return true // no matching rule = public
}
