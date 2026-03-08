package router

import (
	"net/http"
	"regexp"

	"github.com/nimbus-framework/nimbus/context"
)

// HandlerFunc is the handler signature (AdonisJS controller action style).
type HandlerFunc func(*context.Context) error

// Middleware runs before/after handlers.
type Middleware func(HandlerFunc) HandlerFunc

// Route holds a single route definition.
type Route struct {
	Method      string
	Path        string
	Handler     HandlerFunc
	Middlewares []Middleware
	pattern     *regexp.Regexp
	paramNames  []string
}

// Router is the HTTP router (AdonisJS Route group style).
type Router struct {
	routes      []*Route
	middlewares []Middleware
}

// New creates a new Router.
func New() *Router {
	return &Router{
		routes:      nil,
		middlewares: nil,
	}
}

// Use adds global middleware (like AdonisJS start/kernel).
func (r *Router) Use(m ...Middleware) {
	r.middlewares = append(r.middlewares, m...)
}

// Group returns a group that shares a path prefix and optional middleware.
func (r *Router) Group(prefix string, middleware ...Middleware) *Group {
	return &Group{
		router:     r,
		prefix:     prefix,
		middlewares: middleware,
	}
}

// Get registers a GET route.
func (r *Router) Get(path string, handler HandlerFunc) {
	r.addRoute(http.MethodGet, path, handler)
}

// Post registers a POST route.
func (r *Router) Post(path string, handler HandlerFunc) {
	r.addRoute(http.MethodPost, path, handler)
}

// Put registers a PUT route.
func (r *Router) Put(path string, handler HandlerFunc) {
	r.addRoute(http.MethodPut, path, handler)
}

// Patch registers a PATCH route.
func (r *Router) Patch(path string, handler HandlerFunc) {
	r.addRoute(http.MethodPatch, path, handler)
}

// Delete registers a DELETE route.
func (r *Router) Delete(path string, handler HandlerFunc) {
	r.addRoute(http.MethodDelete, path, handler)
}

func (r *Router) addRoute(method, path string, handler HandlerFunc) {
	r.addRouteWithGroup(method, path, handler, nil)
}

func (r *Router) addRouteWithGroup(method, path string, handler HandlerFunc, middlewares []Middleware) {
	pattern, paramNames := pathToRegex(path)
	route := &Route{
		Method:      method,
		Path:        path,
		Handler:     handler,
		Middlewares: middlewares,
		pattern:     pattern,
		paramNames:  paramNames,
	}
	r.routes = append(r.routes, route)
}

// ServeHTTP implements http.Handler.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	route, params := r.match(req.Method, req.URL.Path)
	if route == nil {
		http.NotFound(w, req)
		return
	}
	ctx := context.New(w, req, params)
	chain := route.Handler
	for i := len(route.Middlewares) - 1; i >= 0; i-- {
		chain = route.Middlewares[i](chain)
	}
	for i := len(r.middlewares) - 1; i >= 0; i-- {
		chain = r.middlewares[i](chain)
	}
	if err := chain(ctx); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (r *Router) match(method, path string) (*Route, map[string]string) {
	for _, route := range r.routes {
		if route.Method != method {
			continue
		}
		if params := route.pattern.FindStringSubmatch(path); params != nil {
			m := make(map[string]string)
			for i, name := range route.paramNames {
				if i+1 < len(params) {
					m[name] = params[i+1]
				}
			}
			return route, m
		}
	}
	return nil, nil
}

// pathToRegex converts AdonisJS-style path "/users/:id" to regex and param names.
func pathToRegex(path string) (*regexp.Regexp, []string) {
	var paramNames []string
	re := regexp.QuoteMeta(path)
	re = regexp.MustCompile(`\\:([a-zA-Z_][a-zA-Z0-9_]*)`).ReplaceAllStringFunc(re, func(m string) string {
		// m is \:id
		name := m[2:]
		paramNames = append(paramNames, name)
		return "([^/]+)"
	})
	regex := regexp.MustCompile("^" + re + "$")
	return regex, paramNames
}
