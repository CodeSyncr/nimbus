package router

import "net/http"

// Group allows defining routes with a shared prefix and middleware (AdonisJS Route.group).
type Group struct {
	router      *Router
	prefix      string
	middlewares []Middleware
}

// Use adds middleware to this group only.
func (g *Group) Use(m ...Middleware) {
	g.middlewares = append(g.middlewares, m...)
}

// Get registers GET path (prefixed).
func (g *Group) Get(path string, handler HandlerFunc) {
	g.router.addRouteWithGroup(http.MethodGet, g.prefix+path, handler, g.middlewares)
}

// Post registers POST path (prefixed).
func (g *Group) Post(path string, handler HandlerFunc) {
	g.router.addRouteWithGroup(http.MethodPost, g.prefix+path, handler, g.middlewares)
}

// Put registers PUT path (prefixed).
func (g *Group) Put(path string, handler HandlerFunc) {
	g.router.addRouteWithGroup(http.MethodPut, g.prefix+path, handler, g.middlewares)
}

// Patch registers PATCH path (prefixed).
func (g *Group) Patch(path string, handler HandlerFunc) {
	g.router.addRouteWithGroup(http.MethodPatch, g.prefix+path, handler, g.middlewares)
}

// Delete registers DELETE path (prefixed).
func (g *Group) Delete(path string, handler HandlerFunc) {
	g.router.addRouteWithGroup(http.MethodDelete, g.prefix+path, handler, g.middlewares)
}
