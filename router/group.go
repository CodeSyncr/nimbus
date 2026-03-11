package router

import (
	"strings"

	"github.com/CodeSyncr/nimbus/http"
)

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

func (g *Group) fullPath(path string) string {
	base := strings.TrimSuffix(g.prefix, "/")
	path = strings.TrimPrefix(path, "/")
	if path == "" {
		return base
	}
	return base + "/" + path
}

// Get registers GET path (prefixed).
func (g *Group) Get(path string, handler HandlerFunc) *Route {
	return g.router.addRoute(http.MethodGet, g.fullPath(path), handler, g.middlewares)
}

// Post registers POST path (prefixed).
func (g *Group) Post(path string, handler HandlerFunc) *Route {
	return g.router.addRoute(http.MethodPost, g.fullPath(path), handler, g.middlewares)
}

// Put registers PUT path (prefixed).
func (g *Group) Put(path string, handler HandlerFunc) *Route {
	return g.router.addRoute(http.MethodPut, g.fullPath(path), handler, g.middlewares)
}

// Patch registers PATCH path (prefixed).
func (g *Group) Patch(path string, handler HandlerFunc) *Route {
	return g.router.addRoute(http.MethodPatch, g.fullPath(path), handler, g.middlewares)
}

// Delete registers DELETE path (prefixed).
func (g *Group) Delete(path string, handler HandlerFunc) *Route {
	return g.router.addRoute(http.MethodDelete, g.fullPath(path), handler, g.middlewares)
}

// Any registers a handler for all HTTP methods (prefixed).
func (g *Group) Any(path string, handler HandlerFunc) *Route {
	methods := []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodHead, http.MethodOptions}
	var rt *Route
	for _, m := range methods {
		rt = g.router.addRoute(m, g.fullPath(path), handler, g.middlewares)
	}
	return rt
}

// Resource registers RESTful resource routes within this group's prefix.
func (g *Group) Resource(name string, ctrl ResourceController, opts ...ResourceOption) {
	registerResource(g.router, g.prefix, name, ctrl, g.middlewares, opts)
}
