package router

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/validation"
	"github.com/go-chi/chi/v5"
)

// HandlerFunc is the handler signature (AdonisJS controller action style).
type HandlerFunc func(*http.Context) error

// Middleware runs before/after handlers.
type Middleware func(HandlerFunc) HandlerFunc

// Router wraps Chi as the HTTP router (solid, net/http compatible).
type Router struct {
	chi             chi.Router
	middlewares     []Middleware
	namedRoutes     map[string]*Route
	allRoutes       []*Route
	fallbackHandler HandlerFunc
}

// New creates a new Router backed by Chi.
func New() *Router {
	return &Router{
		chi:         chi.NewRouter(),
		middlewares: nil,
		namedRoutes: make(map[string]*Route),
	}
}

// Use adds global middleware (like AdonisJS start/kernel).
func (r *Router) Use(m ...Middleware) {
	r.middlewares = append(r.middlewares, m...)
}

// Group returns a group that shares a path prefix and optional middleware.
func (r *Router) Group(prefix string, middleware ...Middleware) *Group {
	return &Group{
		router:      r,
		prefix:      strings.TrimSuffix(prefix, "/"),
		middlewares: middleware,
	}
}

// Get registers a GET route.
func (r *Router) Get(path string, handler HandlerFunc) *Route {
	return r.addRoute(http.MethodGet, path, handler, nil)
}

// Post registers a POST route.
func (r *Router) Post(path string, handler HandlerFunc) *Route {
	return r.addRoute(http.MethodPost, path, handler, nil)
}

// Put registers a PUT route.
func (r *Router) Put(path string, handler HandlerFunc) *Route {
	return r.addRoute(http.MethodPut, path, handler, nil)
}

// Patch registers a PATCH route.
func (r *Router) Patch(path string, handler HandlerFunc) *Route {
	return r.addRoute(http.MethodPatch, path, handler, nil)
}

// Delete registers a DELETE route.
func (r *Router) Delete(path string, handler HandlerFunc) *Route {
	return r.addRoute(http.MethodDelete, path, handler, nil)
}

// Any registers a route that matches all standard HTTP methods.
func (r *Router) Any(path string, handler HandlerFunc) *Route {
	methods := []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodHead, http.MethodOptions}
	var rt *Route
	for _, m := range methods {
		rt = r.addRoute(m, path, handler, nil)
	}
	return rt
}

// Route registers a handler for the given custom HTTP methods.
func (r *Router) Route(path string, methods []string, handler HandlerFunc) *Route {
	var rt *Route
	for _, m := range methods {
		rt = r.addRoute(m, path, handler, nil)
	}
	return rt
}

// Resource registers RESTful resource routes for a controller.
// Generates: index, create, store, show, edit, update, destroy.
func (r *Router) Resource(name string, ctrl ResourceController, opts ...ResourceOption) {
	registerResource(r, "", name, ctrl, nil, opts)
}

// Mount attaches an http.Handler at the given path. Useful for mounting
// sub-applications (e.g. MCP servers, SSE endpoints) that implement http.Handler.
func (r *Router) Mount(path string, handler http.Handler) {
	r.chi.Mount(path, handler)
}

// Fallback registers a catch-all handler that is invoked when no routes match.
// This is the equivalent of AdonisJS's Route.fallback(). If no Fallback is
// registered, Chi's default 404 handling applies.
//
//	app.Router.Fallback(func(c *http.Context) error {
//	    return c.JSON(404, map[string]string{"error": "Not found"})
//	})
func (r *Router) Fallback(handler HandlerFunc) {
	r.fallbackHandler = handler
	r.chi.NotFound(func(w http.ResponseWriter, req *http.Request) {
		ctx := http.New(w, req, nil)
		if err := handler(ctx); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
}

// URL generates a URL for a named route, substituting params.
// Params are key-value pairs: router.URL("users.show", "id", "42") → "/users/42".
func (r *Router) URL(name string, params ...string) string {
	rt, ok := r.namedRoutes[name]
	if !ok {
		return ""
	}
	path := rt.path
	for i := 0; i+1 < len(params); i += 2 {
		path = strings.Replace(path, ":"+params[i], params[i+1], 1)
	}
	return path
}

func pathToChi(path string) string {
	for {
		i := strings.Index(path, ":")
		if i < 0 {
			break
		}
		end := i + 1
		for end < len(path) && (path[end] == '_' || (path[end] >= 'a' && path[end] <= 'z') || (path[end] >= 'A' && path[end] <= 'Z') || (path[end] >= '0' && path[end] <= '9')) {
			end++
		}
		if end > i+1 {
			path = path[:i] + "{" + path[i+1:end] + "}" + path[end:]
		} else {
			break
		}
	}
	return path
}

func (r *Router) addRoute(method, path string, handler HandlerFunc, groupMiddleware []Middleware) *Route {
	chiPath := pathToChi(path)
	chain := handler
	for i := len(groupMiddleware) - 1; i >= 0; i-- {
		chain = groupMiddleware[i](chain)
	}
	for i := len(r.middlewares) - 1; i >= 0; i-- {
		chain = r.middlewares[i](chain)
	}
	h := toHandler(chain)
	switch method {
	case http.MethodGet:
		r.chi.Get(chiPath, h)
	case http.MethodPost:
		r.chi.Post(chiPath, h)
	case http.MethodPut:
		r.chi.Put(chiPath, h)
	case http.MethodPatch:
		r.chi.Patch(chiPath, h)
	case http.MethodDelete:
		r.chi.Delete(chiPath, h)
	case http.MethodHead:
		r.chi.Head(chiPath, h)
	case http.MethodOptions:
		r.chi.Options(chiPath, h)
	}
	rt := &Route{router: r, method: method, path: path}
	r.allRoutes = append(r.allRoutes, rt)
	return rt
}

// Routes returns all registered routes.
func (r *Router) Routes() []*Route {
	return r.allRoutes
}

func toHandler(fn HandlerFunc) http.StdHandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		params := make(map[string]string)
		if rc := chi.RouteContext(req.Context()); rc != nil {
			for i, key := range rc.URLParams.Keys {
				if key != "" && i < len(rc.URLParams.Values) {
					params[key] = rc.URLParams.Values[i]
				}
			}
		}
		ctx := http.New(w, req, params)
		if err := fn(ctx); err != nil {
			// Fallback safety net when no global error middleware is installed.
			// For richer behavior (HTTPError, custom JSON), install errors.Handler.
			if ve, ok := err.(validation.ValidationErrors); ok {
				_ = ctx.JSON(http.StatusUnprocessableEntity, ve.ToMap())
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

// ServeHTTP implements http.Handler.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	path := req.URL.Path
	if len(path) > 1 && path[len(path)-1] == '/' {
		req = req.Clone(req.Context())
		u2 := *req.URL
		u2.Path = strings.TrimSuffix(path, "/")
		req.URL = &u2
	}
	r.chi.ServeHTTP(w, req)
}

// PrintRoutes prints a formatted table of all registered routes.
// If w is nil, it prints to os.Stdout.
func (r *Router) PrintRoutes(w io.Writer) {
	if w == nil {
		w = os.Stdout
	}
	routes := r.Routes()
	if len(routes) == 0 {
		fmt.Fprintln(w, "  No routes registered.")
		return
	}

	// Determine column widths.
	maxMethod, maxPath, maxName := 6, 4, 4
	for _, rt := range routes {
		if len(rt.Method()) > maxMethod {
			maxMethod = len(rt.Method())
		}
		if len(rt.Path()) > maxPath {
			maxPath = len(rt.Path())
		}
		if len(rt.Name()) > maxName {
			maxName = len(rt.Name())
		}
	}
	if maxPath > 60 {
		maxPath = 60
	}

	header := fmt.Sprintf("  %-*s  %-*s  %-*s  %s", maxMethod, "Method", maxPath, "Path", maxName, "Name", "Summary")
	sep := "  " + strings.Repeat("─", len(header))

	fmt.Fprintln(w)
	fmt.Fprintln(w, header)
	fmt.Fprintln(w, sep)
	for _, rt := range routes {
		name := rt.Name()
		if name == "" {
			name = "·"
		}
		summary := rt.Meta.Summary
		fmt.Fprintf(w, "  %-*s  %-*s  %-*s  %s\n", maxMethod, rt.Method(), maxPath, rt.Path(), maxName, name, summary)
	}
	fmt.Fprintln(w)
}
