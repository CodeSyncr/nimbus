package context

import (
	"encoding/json"
	"net/http"

	"github.com/nimbus-framework/nimbus/view"
)

// Context wraps http.Request and ResponseWriter with AdonisJS-style helpers.
type Context struct {
	Request  *http.Request
	Response http.ResponseWriter
	Params   map[string]string
	status   int
}

// New creates a new request context.
func New(w http.ResponseWriter, r *http.Request, params map[string]string) *Context {
	return &Context{
		Request:  r,
		Response: w,
		Params:   params,
		status:   http.StatusOK,
	}
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
	http.Redirect(c.Response, c.Request, url, code)
}

// View renders a .nimbus template and sends HTML response (plan: ctx.View("home", data)).
func (c *Context) View(name string, data any) error {
	html, err := view.Render(name, data)
	if err != nil {
		return err
	}
	c.Response.Header().Set("Content-Type", "text/html; charset=utf-8")
	c.Response.WriteHeader(http.StatusOK)
	_, err = c.Response.Write([]byte(html))
	return err
}
