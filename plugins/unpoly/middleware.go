package unpoly

import (
	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

// Middleware returns the named middleware provided by this plugin.
//
// "unpoly" — the server protocol middleware. Register it as server
// middleware in start/kernel.go so every Unpoly request gets the
// required response headers.
func (p *Plugin) Middleware() map[string]router.Middleware {
	return map[string]router.Middleware{
		"unpoly": ServerProtocol(),
	}
}

// ServerProtocol returns middleware that implements the Unpoly server
// protocol. For every request that carries an X-Up-Version header it:
//
//   - Echoes X-Up-Location with the current request URL
//   - Echoes X-Up-Method with the current HTTP method
//   - Adds Vary: X-Up-Target, X-Up-Version for cache partitioning
//
// Non-Unpoly requests pass through untouched.
func ServerProtocol() router.Middleware {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *http.Context) error {
			if !IsUnpoly(c) {
				return next(c)
			}

			h := c.Response.Header()

			h.Set("X-Up-Location", c.Request.URL.RequestURI())
			h.Set("X-Up-Method", c.Request.Method)

			h.Add("Vary", "X-Up-Target")
			h.Add("Vary", "X-Up-Version")
			h.Add("Vary", "X-Up-Validate")
			h.Add("Vary", "X-Up-Mode")

			return next(c)
		}
	}
}
