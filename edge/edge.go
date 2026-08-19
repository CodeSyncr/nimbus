// Package edge provides request-preprocessing middleware for Nimbus.
//
// Despite the name, this runs inside your application (it is Nimbus
// middleware), not on a CDN. It sits in front of your routes and can short-
// circuit, redirect, rewrite, or decorate requests before they reach your
// handlers — useful for geo routing, A/B tests, maintenance windows, security
// headers, basic auth, CORS, simple rate limiting, and response caching. It
// reads CDN geo headers (CF-IPCountry, X-Vercel-IP-Country, …) when a real CDN
// sits in front of your app.
//
// For deploying to an actual edge/serverless runtime, see the `serverless`
// package (AWS Lambda).
//
// Usage:
//
//	rt := edge.New(edge.Config{MaxExecTime: 50 * time.Millisecond})
//	rt.Handle("/geo", func(req *edge.Request) *edge.Response {
//	    if req.Geo.Country == "DE" {
//	        return edge.Redirect("/de"+req.Path, 302)
//	    }
//	    return edge.Next() // continue to the normal handler
//	})
//
//	app.Use(rt.Plugin())            // registers the middleware + /_edge/metrics
//	// or, without the plugin:
//	// app.Router.Use(rt.Middleware())
package edge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/CodeSyncr/nimbus"
	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

// Config for the edge middleware runtime.
type Config struct {
	// MaxExecTime bounds how long the runtime waits for a handler (default 50ms).
	// A timeout-scoped context is passed to the handler via req.Context(); note
	// that Go cannot forcibly stop a goroutine, so a handler that ignores its
	// context keeps running in the background after a timeout.
	MaxExecTime time.Duration

	// MaxBodyBytes caps how many request-body bytes the runtime reads and
	// exposes on req.Body (default 4MB). The full body is still forwarded to
	// downstream handlers.
	MaxBodyBytes int64

	// CacheDefault TTL for route caching when none is given (default 60s).
	CacheDefault time.Duration

	// Fallback behavior when a handler panics or times out (default FallbackNext).
	Fallback FallbackMode

	// OnError is called for handler panics and timeouts.
	OnError func(path string, err error)

	// Prefix is prepended to every registered edge path (default "").
	Prefix string
}

// FallbackMode determines behavior on handler failure.
type FallbackMode int

const (
	// FallbackNext passes the request through to the normal handler.
	FallbackNext FallbackMode = iota
	// FallbackError returns a 502 Bad Gateway.
	FallbackError
	// FallbackCached returns the last successful response for the route, if any.
	FallbackCached
)

// ---------------------------------------------------------------------------
// Request / Response types
// ---------------------------------------------------------------------------

// Request is a lightweight view of an HTTP request for edge handlers.
type Request struct {
	Method    string            `json:"method"`
	Path      string            `json:"path"`
	Query     map[string]string `json:"query"`
	Headers   map[string]string `json:"headers"`
	Body      []byte            `json:"body,omitempty"`
	IP        string            `json:"ip"`
	Geo       GeoInfo           `json:"geo"`
	StartTime time.Time         `json:"-"`
	ctx       context.Context
}

// GeoInfo provides geographic information derived from CDN headers.
type GeoInfo struct {
	Country    string  `json:"country"`
	Region     string  `json:"region"`
	City       string  `json:"city"`
	Latitude   float64 `json:"latitude"`
	Longitude  float64 `json:"longitude"`
	Timezone   string  `json:"timezone"`
	ISP        string  `json:"isp"`
	Datacenter string  `json:"datacenter"`
}

// Header returns a request header value (canonicalized).
func (r *Request) Header(key string) string { return r.Headers[http.CanonicalHeaderKey(key)] }

// QueryParam returns a query parameter value.
func (r *Request) QueryParam(key string) string { return r.Query[key] }

// ParseJSON decodes the request body into v.
func (r *Request) ParseJSON(v any) error { return json.Unmarshal(r.Body, v) }

// Context returns the request context (timeout-scoped to MaxExecTime).
func (r *Request) Context() context.Context {
	if r.ctx == nil {
		return context.Background()
	}
	return r.ctx
}

// Response is an edge handler's response.
type Response struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    []byte            `json:"body,omitempty"`
	BodyStr string            `json:"-"`

	// Internal flags.
	passThru bool   // pass to the normal handler
	rewrite  string // rewrite URL path
	cached   bool
}

// IsNext reports whether the request should continue to the normal handler.
func (r *Response) IsNext() bool { return r.passThru }

// SetHeader sets a response header.
func (r *Response) SetHeader(key, value string) *Response {
	if r.Headers == nil {
		r.Headers = make(map[string]string)
	}
	r.Headers[key] = value
	return r
}

// body returns the effective response body regardless of which field was set.
func (r *Response) body() []byte {
	if len(r.Body) > 0 {
		return r.Body
	}
	if r.BodyStr != "" {
		return []byte(r.BodyStr)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Response Constructors
// ---------------------------------------------------------------------------

// Next continues to the normal handler.
func Next() *Response { return &Response{passThru: true} }

// Respond creates a plain-text response.
func Respond(status int, body string) *Response {
	return &Response{Status: status, BodyStr: body,
		Headers: map[string]string{"Content-Type": "text/plain; charset=utf-8"}}
}

// JSON creates a JSON response.
func JSON(status int, data any) *Response {
	body, _ := json.Marshal(data)
	return &Response{Status: status, Body: body,
		Headers: map[string]string{"Content-Type": "application/json"}}
}

// HTML creates an HTML response.
func HTML(status int, html string) *Response {
	return &Response{Status: status, BodyStr: html,
		Headers: map[string]string{"Content-Type": "text/html; charset=utf-8"}}
}

// Redirect creates a redirect response.
func Redirect(url string, status int) *Response {
	return &Response{Status: status, Headers: map[string]string{"Location": url}}
}

// Rewrite rewrites the request URL path without a client-visible redirect.
func Rewrite(url string) *Response { return &Response{passThru: true, rewrite: url} }

// HandlerFunc is the signature for edge handlers.
type HandlerFunc func(req *Request) *Response

// ---------------------------------------------------------------------------
// Runtime
// ---------------------------------------------------------------------------

// Runtime holds registered edge handlers and metrics.
type Runtime struct {
	config   Config
	handlers map[string]edgeRoute
	cache    *Cache
	mu       sync.RWMutex

	totalInvocations uint64
	totalErrors      uint64
	totalCacheHits   uint64
	totalTimeouts    uint64
	totalLatencyNs   uint64
}

type edgeRoute struct {
	pattern string
	handler HandlerFunc
	methods []string // empty = all methods
	cache   *routeCache
}

type routeCache struct {
	ttl time.Duration
	key func(req *Request) string
}

// cachedResponse is the on-cache representation (survives round-tripping,
// unlike a *Response whose BodyStr is json:"-").
type cachedResponse struct {
	Status  int               `json:"s"`
	Headers map[string]string `json:"h"`
	Body    []byte            `json:"b"`
}

// New creates an edge middleware runtime.
func New(cfgs ...Config) *Runtime {
	cfg := Config{}
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}
	if cfg.MaxExecTime == 0 {
		cfg.MaxExecTime = 50 * time.Millisecond
	}
	if cfg.MaxBodyBytes == 0 {
		cfg.MaxBodyBytes = 4 * 1024 * 1024
	}
	if cfg.CacheDefault == 0 {
		cfg.CacheDefault = 60 * time.Second
	}
	return &Runtime{
		config:   cfg,
		handlers: make(map[string]edgeRoute),
		cache:    NewCache(10000),
	}
}

// Handle registers an edge handler for a path (supports a trailing "*" wildcard).
func (rt *Runtime) Handle(path string, handler HandlerFunc) *edgeRouteBuilder {
	fullPath := rt.config.Prefix + path
	rt.mu.Lock()
	rt.handlers[fullPath] = edgeRoute{pattern: fullPath, handler: handler}
	rt.mu.Unlock()
	return &edgeRouteBuilder{rt: rt, path: fullPath}
}

// edgeRouteBuilder is a fluent configurator for an edge route.
type edgeRouteBuilder struct {
	rt   *Runtime
	path string
}

// Methods restricts the handler to specific HTTP methods.
func (b *edgeRouteBuilder) Methods(methods ...string) *edgeRouteBuilder {
	b.rt.mu.Lock()
	defer b.rt.mu.Unlock()
	r := b.rt.handlers[b.path]
	r.methods = methods
	b.rt.handlers[b.path] = r
	return b
}

// WithCache caches the handler's response for ttl (0 → CacheDefault). An
// optional key function customizes the cache key (default "method:path").
func (b *edgeRouteBuilder) WithCache(ttl time.Duration, keyFn ...func(req *Request) string) *edgeRouteBuilder {
	b.rt.mu.Lock()
	defer b.rt.mu.Unlock()
	if ttl == 0 {
		ttl = b.rt.config.CacheDefault
	}
	r := b.rt.handlers[b.path]
	r.cache = &routeCache{ttl: ttl}
	if len(keyFn) > 0 {
		r.cache.key = keyFn[0]
	}
	b.rt.handlers[b.path] = r
	return b
}

// Middleware returns the Nimbus middleware that runs matching edge handlers.
func (rt *Runtime) Middleware() router.Middleware {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *nhttp.Context) error {
			rt.mu.RLock()
			route, found := rt.findRoute(c.Request.URL.Path)
			rt.mu.RUnlock()
			if !found || !methodAllowed(route, c.Request.Method) {
				return next(c)
			}

			// Build the request view ONCE; this restores c.Request.Body so both
			// the edge handler and any downstream handler can read it.
			req := rt.buildRequest(c)

			// Serve from cache if present.
			if route.cache != nil {
				if data, ok := rt.cache.Get(rt.cacheKey(route, req)); ok {
					atomic.AddUint64(&rt.totalCacheHits, 1)
					var cr cachedResponse
					if json.Unmarshal(data, &cr) == nil {
						return writeCached(c, cr)
					}
				}
			}

			resp := rt.execute(route, req)
			if resp == nil || resp.IsNext() {
				if resp != nil {
					if resp.rewrite != "" {
						c.Request.URL.Path = resp.rewrite
					}
					for k, v := range resp.Headers {
						c.Response.Header().Set(k, v)
					}
				}
				return next(c)
			}

			// Cache and/or record as the fallback for this route.
			if route.cache != nil {
				cr := cachedResponse{Status: statusOr(resp.Status, 200), Headers: resp.Headers, Body: resp.body()}
				if data, err := json.Marshal(cr); err == nil {
					rt.cache.Set(rt.cacheKey(route, req), data, route.cache.ttl)
				}
			}
			if rt.config.Fallback == FallbackCached {
				cr := cachedResponse{Status: statusOr(resp.Status, 200), Headers: resp.Headers, Body: resp.body()}
				if data, err := json.Marshal(cr); err == nil {
					rt.cache.Set("fallback:"+route.pattern, data, time.Hour)
				}
			}
			return rt.writeResponse(c, resp)
		}
	}
}

func methodAllowed(route edgeRoute, method string) bool {
	if len(route.methods) == 0 {
		return true
	}
	for _, m := range route.methods {
		if strings.EqualFold(m, method) {
			return true
		}
	}
	return false
}

func (rt *Runtime) findRoute(path string) (edgeRoute, bool) {
	if route, ok := rt.handlers[path]; ok {
		return route, true
	}
	for pattern, route := range rt.handlers {
		if strings.HasSuffix(pattern, "*") && strings.HasPrefix(path, strings.TrimSuffix(pattern, "*")) {
			return route, true
		}
	}
	return edgeRoute{}, false
}

func (rt *Runtime) buildRequest(c *nhttp.Context) *Request {
	req := &Request{
		Method:    c.Request.Method,
		Path:      c.Request.URL.Path,
		Query:     make(map[string]string),
		Headers:   make(map[string]string),
		IP:        extractIP(c.Request),
		StartTime: time.Now(),
		ctx:       c.Request.Context(),
	}
	for k, v := range c.Request.URL.Query() {
		if len(v) > 0 {
			req.Query[k] = v[0]
		}
	}
	for k, v := range c.Request.Header {
		if len(v) > 0 {
			req.Headers[k] = v[0]
		}
	}
	// Read a bounded copy of the body and RESTORE it so downstream handlers
	// (and cache re-checks) still see the full body.
	if c.Request.Body != nil {
		body, _ := io.ReadAll(io.LimitReader(c.Request.Body, rt.config.MaxBodyBytes))
		_ = c.Request.Body.Close()
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		req.Body = body
	}
	req.Geo = GeoInfo{
		Country:    c.Request.Header.Get("CF-IPCountry"),
		City:       c.Request.Header.Get("CF-IPCity"),
		Region:     c.Request.Header.Get("CF-Region"),
		Latitude:   parseFloat(c.Request.Header.Get("CF-IPLatitude")),
		Longitude:  parseFloat(c.Request.Header.Get("CF-IPLongitude")),
		Timezone:   c.Request.Header.Get("CF-Timezone"),
		Datacenter: c.Request.Header.Get("CF-Ray"),
	}
	if req.Geo.Country == "" {
		req.Geo.Country = c.Request.Header.Get("X-Vercel-IP-Country")
	}
	if req.Geo.City == "" {
		req.Geo.City = c.Request.Header.Get("X-Vercel-IP-City")
	}
	return req
}

func (rt *Runtime) execute(route edgeRoute, req *Request) *Response {
	atomic.AddUint64(&rt.totalInvocations, 1)

	// Give the handler a timeout-scoped context it can honor.
	ctx, cancel := context.WithTimeout(req.Context(), rt.config.MaxExecTime)
	defer cancel()
	req.ctx = ctx

	ch := make(chan *Response, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				atomic.AddUint64(&rt.totalErrors, 1)
				if rt.config.OnError != nil {
					rt.config.OnError(route.pattern, fmt.Errorf("edge handler panic: %v", r))
				}
				ch <- nil
			}
		}()
		ch <- route.handler(req)
	}()

	select {
	case resp := <-ch:
		atomic.AddUint64(&rt.totalLatencyNs, uint64(time.Since(req.StartTime).Nanoseconds()))
		if resp == nil {
			return rt.fallbackResponse(route.pattern)
		}
		return resp
	case <-ctx.Done():
		atomic.AddUint64(&rt.totalTimeouts, 1)
		if rt.config.OnError != nil {
			rt.config.OnError(route.pattern, fmt.Errorf("edge handler timed out after %s", rt.config.MaxExecTime))
		}
		return rt.fallbackResponse(route.pattern)
	}
}

func (rt *Runtime) fallbackResponse(pattern string) *Response {
	switch rt.config.Fallback {
	case FallbackError:
		return Respond(502, "Edge handler error")
	case FallbackCached:
		if data, ok := rt.cache.Get("fallback:" + pattern); ok {
			var cr cachedResponse
			if json.Unmarshal(data, &cr) == nil {
				resp := &Response{Status: cr.Status, Headers: cr.Headers, Body: cr.Body}
				return resp.SetHeader("X-Edge-Fallback", "cached")
			}
		}
		return Next()
	default:
		return Next()
	}
}

func (rt *Runtime) writeResponse(c *nhttp.Context, resp *Response) error {
	for k, v := range resp.Headers {
		c.Response.Header().Set(k, v)
	}
	c.Response.Header().Set("X-Edge-Handler", "true")
	c.Response.WriteHeader(statusOr(resp.Status, 200))
	if b := resp.body(); len(b) > 0 {
		_, err := c.Response.Write(b)
		return err
	}
	return nil
}

func writeCached(c *nhttp.Context, cr cachedResponse) error {
	for k, v := range cr.Headers {
		c.Response.Header().Set(k, v)
	}
	c.Response.Header().Set("X-Edge-Cache", "HIT")
	c.Response.WriteHeader(statusOr(cr.Status, 200))
	if len(cr.Body) > 0 {
		_, err := c.Response.Write(cr.Body)
		return err
	}
	return nil
}

func (rt *Runtime) cacheKey(route edgeRoute, req *Request) string {
	if route.cache != nil && route.cache.key != nil {
		return route.cache.key(req)
	}
	return req.Method + ":" + req.Path
}

func statusOr(status, def int) int {
	if status == 0 {
		return def
	}
	return status
}

// ---------------------------------------------------------------------------
// Metrics
// ---------------------------------------------------------------------------

// Metrics returns a snapshot of runtime counters.
func (rt *Runtime) Metrics() map[string]any {
	inv := atomic.LoadUint64(&rt.totalInvocations)
	var avg uint64
	if inv > 0 {
		avg = atomic.LoadUint64(&rt.totalLatencyNs) / inv
	}
	rt.mu.RLock()
	routes := len(rt.handlers)
	rt.mu.RUnlock()
	return map[string]any{
		"total_invocations": inv,
		"total_errors":      atomic.LoadUint64(&rt.totalErrors),
		"total_cache_hits":  atomic.LoadUint64(&rt.totalCacheHits),
		"total_timeouts":    atomic.LoadUint64(&rt.totalTimeouts),
		"avg_latency_ns":    avg,
		"routes":            routes,
	}
}

// ---------------------------------------------------------------------------
// Plugin integration
// ---------------------------------------------------------------------------

var (
	_ nimbus.Plugin    = (*EdgePlugin)(nil)
	_ nimbus.HasRoutes = (*EdgePlugin)(nil)
)

// EdgePlugin wires the runtime into a Nimbus app: it applies the middleware and
// mounts a /_edge/metrics endpoint.
type EdgePlugin struct {
	runtime *Runtime
}

// Plugin returns the runtime as a Nimbus plugin. Register with app.Use(rt.Plugin()).
func (rt *Runtime) Plugin() *EdgePlugin { return &EdgePlugin{runtime: rt} }

func (ep *EdgePlugin) Name() string    { return "edge" }
func (ep *EdgePlugin) Version() string { return "1.0.0" }

// Register applies the edge middleware to the application router.
func (ep *EdgePlugin) Register(app *nimbus.App) error {
	app.Router.Use(ep.runtime.Middleware())
	return nil
}

func (ep *EdgePlugin) Boot(*nimbus.App) error { return nil }

// RegisterRoutes mounts the metrics endpoint.
func (ep *EdgePlugin) RegisterRoutes(r *router.Router) {
	r.Get("/_edge/metrics", func(c *nhttp.Context) error {
		return c.JSON(200, ep.runtime.Metrics())
	})
}
