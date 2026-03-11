package http

import (
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"net/http"
	stdlib "net/http"

	"github.com/CodeSyncr/nimbus/view"
)

// Context wraps an HTTP request and response with AdonisJS-style helpers.
type Context struct {
	Request  *http.Request
	Response http.ResponseWriter
	Params   map[string]string
	status   int
	store    map[string]any
}

// Set stores a key-value pair in the request-scoped store.
func (c *Context) Set(key string, val any) {
	if c.store == nil {
		c.store = make(map[string]any)
	}
	c.store[key] = val
}

// Get retrieves a value from the request-scoped store.
func (c *Context) Get(key string) (any, bool) {
	if c.store == nil {
		return nil, false
	}
	v, ok := c.store[key]
	return v, ok
}

// MustGet retrieves a value or panics if not found.
func (c *Context) MustGet(key string) any {
	v, ok := c.Get(key)
	if !ok {
		panic("nimbus: context key \"" + key + "\" not found")
	}
	return v
}

// New creates a new request context.
func New(w stdlib.ResponseWriter, r *stdlib.Request, params map[string]string) *Context {
	return &Context{
		Request:  r,
		Response: w,
		Params:   params,
		status:   stdlib.StatusOK,
	}
}

// QueryInt returns a query parameter as an integer, or the default value.
func (c *Context) QueryInt(key string, def int) int {
	v := c.Request.URL.Query().Get(key)
	if v == "" {
		return def
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
		return def
	}
	return n
}

// Param returns a route parameter by name.
func (c *Context) Param(name string) string {
	return c.Params[name]
}

// Status sets the HTTP status code.
func (c *Context) Status(code int) *Context {
	c.status = code
	c.Response.WriteHeader(code)
	return c
}

// JSON sends a JSON response.
func (c *Context) JSON(code int, body any) error {
	c.Response.Header().Set("Content-Type", "application/json")
	c.Response.WriteHeader(code)
	return json.NewEncoder(c.Response).Encode(body)
}

// String sends a plain text response.
func (c *Context) String(code int, s string) {
	c.Response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	c.Response.WriteHeader(code)
	c.Response.Write([]byte(s))
}

// Redirect sends a redirect response.
func (c *Context) Redirect(code int, url string) {
	stdlib.Redirect(c.Response, c.Request, url, code)
}

// View renders a .nimbus template and sends HTML response.
// When Shield CSRF is enabled, csrfField is auto-injected so templates can use {{ .csrfField }}.
func (c *Context) View(name string, data any) error {
	data = injectCSRFField(c, data)
	out, err := view.Render(name, data)
	if err != nil {
		return err
	}
	c.Response.Header().Set("Content-Type", "text/html; charset=utf-8")
	c.Response.WriteHeader(c.status)
	_, err = c.Response.Write([]byte(out))
	return err
}

// injectCSRFField adds csrfField (hidden input) to view data when Shield has set _csrf_token.
func injectCSRFField(c *Context, data any) any {
	token, ok := c.Get("_csrf_token")
	if !ok {
		return data
	}
	s, ok := token.(string)
	if !ok || s == "" {
		return data
	}
	m, ok := data.(map[string]any)
	if !ok {
		return data
	}
	merged := make(map[string]any, len(m)+1)
	for k, v := range m {
		merged[k] = v
	}
	merged["csrfField"] = template.HTML(`<input type="hidden" name="_csrf" value="` + html.EscapeString(s) + `">`)
	return merged
}
