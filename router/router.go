package router

import (
	"net/http"
	"strings"

	"github.com/CodeSyncr/nimbus/context"
	"github.com/go-chi/chi/v5"
)

// HandlerFunc is the handler signature (AdonisJS controller action style).
type HandlerFunc func(*context.Context) error

// Middleware runs before/after handlers.
type Middleware func(HandlerFunc) HandlerFunc

// Router wraps Chi as the HTTP router (solid, net/http compatible).
type Router struct {
	chi         chi.Router
	middlewares []Middleware
	namedRoutes map[string]*Route
}

// New creates a new Router backed by Chi.
func New() *Router {
	return &Router{
		chi:         chi.NewRouter(),
		middlewares:  nil,
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
	return &Route{router: r, method: method, path: path}
}

func toHandler(fn HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		params := make(map[string]string)
		if rc := chi.RouteContext(req.Context()); rc != nil {
			for i, key := range rc.URLParams.Keys {
				if key != "" && i < len(rc.URLParams.Values) {
					params[key] = rc.URLParams.Values[i]
				}
			}
		}
		ctx := context.New(w, req, params)
		if err := fn(ctx); err != nil {
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
