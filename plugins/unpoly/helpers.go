package unpoly

import (
	"encoding/json"
	"strings"

	"github.com/CodeSyncr/nimbus/http"
)

// ---------------------------------------------------------------------------
// Request helpers — read Unpoly request headers
// ---------------------------------------------------------------------------

// IsUnpoly returns true if the request was made by Unpoly (has X-Up-Version).
func IsUnpoly(c *http.Context) bool {
	return c.Request.Header.Get("X-Up-Version") != ""
}

// Target returns the CSS selector Unpoly is targeting (X-Up-Target),
// or an empty string for non-Unpoly / full-page requests.
func Target(c *http.Context) string {
	return c.Request.Header.Get("X-Up-Target")
}

// FailTarget returns the CSS selector for a failed update (X-Up-Fail-Target).
func FailTarget(c *http.Context) string {
	return c.Request.Header.Get("X-Up-Fail-Target")
}

// Mode returns the targeted layer's mode (X-Up-Mode): "root", "modal",
// "popup", "drawer", or "cover".
func Mode(c *http.Context) string {
	return c.Request.Header.Get("X-Up-Mode")
}

// FailMode returns the mode for a failed fragment update (X-Up-Fail-Mode).
func FailMode(c *http.Context) string {
	return c.Request.Header.Get("X-Up-Fail-Mode")
}

// Version returns the Unpoly version string from the request (X-Up-Version).
func Version(c *http.Context) string {
	return c.Request.Header.Get("X-Up-Version")
}

// Validate returns the names of form fields being validated (X-Up-Validate),
// or an empty string if this is not a validation request.
func Validate(c *http.Context) string {
	return c.Request.Header.Get("X-Up-Validate")
}

// IsValidating returns true if Unpoly is validating form fields.
func IsValidating(c *http.Context) bool {
	return Validate(c) != ""
}

// ValidateNames returns the individual field names being validated as a slice.
func ValidateNames(c *http.Context) []string {
	v := Validate(c)
	if v == "" {
		return nil
	}
	parts := strings.Split(v, " ")
	names := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			names = append(names, s)
		}
	}
	return names
}

// Context returns the targeted layer's context object from
// X-Up-Context (parsed from JSON), or nil.
func Context(c *http.Context) map[string]any {
	raw := c.Request.Header.Get("X-Up-Context")
	if raw == "" {
		return nil
	}
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		return nil
	}
	return m
}

// FailContext returns the layer context for a failed update
// (X-Up-Fail-Context).
func FailContext(c *http.Context) map[string]any {
	raw := c.Request.Header.Get("X-Up-Fail-Context")
	if raw == "" {
		return nil
	}
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		return nil
	}
	return m
}

// ---------------------------------------------------------------------------
// Response helpers — set Unpoly response headers
// ---------------------------------------------------------------------------

// SetTarget overrides the CSS selector that Unpoly will update
// (X-Up-Target response header). Useful when the server wants to
// render a different fragment than what was requested.
func SetTarget(c *http.Context, selector string) {
	c.Response.Header().Set("X-Up-Target", selector)
}

// RenderNothing tells Unpoly to skip rendering by setting
// X-Up-Target: :none. The response body can be empty.
func RenderNothing(c *http.Context) {
	c.Response.Header().Set("X-Up-Target", ":none")
}

// SetTitle changes the document title after a fragment update
// (X-Up-Title). The value is JSON-encoded as Unpoly expects.
// Set to "false" to prevent Unpoly from changing the title.
func SetTitle(c *http.Context, title string) {
	b, _ := json.Marshal(title)
	c.Response.Header().Set("X-Up-Title", string(b))
}

// EmitEvent sends a client-side event via X-Up-Events. The event
// is serialized as JSON. Call multiple times to emit multiple events.
func EmitEvent(c *http.Context, eventType string, props map[string]any) {
	event := map[string]any{"type": eventType}
	for k, v := range props {
		event[k] = v
	}
	existing := c.Response.Header().Get("X-Up-Events")
	var events []map[string]any
	if existing != "" {
		_ = json.Unmarshal([]byte(existing), &events)
	}
	events = append(events, event)
	b, _ := json.Marshal(events)
	c.Response.Header().Set("X-Up-Events", string(b))
}

// AcceptLayer closes the targeted overlay and accepts it with the
// given value (X-Up-Accept-Layer, serialized as JSON). The value
// becomes the overlay's acceptance value.
func AcceptLayer(c *http.Context, value any) {
	b, _ := json.Marshal(value)
	c.Response.Header().Set("X-Up-Accept-Layer", string(b))
}

// DismissLayer closes the targeted overlay and dismisses it with
// the given value (X-Up-Dismiss-Layer, serialized as JSON).
func DismissLayer(c *http.Context, value any) {
	b, _ := json.Marshal(value)
	c.Response.Header().Set("X-Up-Dismiss-Layer", string(b))
}

// ExpireCache tells Unpoly to expire cached responses matching the
// given URL pattern (X-Up-Expire-Cache). Use "*" to expire all.
// Expired entries are re-validated on next use.
func ExpireCache(c *http.Context, pattern string) {
	c.Response.Header().Set("X-Up-Expire-Cache", pattern)
}

// EvictCache tells Unpoly to evict (delete) cached responses matching
// the given URL pattern (X-Up-Evict-Cache). Use "*" to evict all.
func EvictCache(c *http.Context, pattern string) {
	c.Response.Header().Set("X-Up-Evict-Cache", pattern)
}

// SetLocation overrides the browser location shown after a fragment
// update (X-Up-Location). Useful after redirects or rewrites.
func SetLocation(c *http.Context, url string) {
	c.Response.Header().Set("X-Up-Location", url)
}

// SetMethod overrides the HTTP method echoed back to Unpoly
// (X-Up-Method). Useful after method-changing redirects.
func SetMethod(c *http.Context, method string) {
	c.Response.Header().Set("X-Up-Method", method)
}
