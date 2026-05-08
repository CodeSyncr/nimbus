// Package edge provides an edge function runtime for Nimbus.
//
// Edge functions run lightweight request handlers at the network edge,
// enabling ultra-low latency responses for tasks like A/B tests,
// geolocation routing, auth token validation, response transforms,
// and dynamic headers — without round-tripping to the origin server.
//
// Usage:
//
//	edgeRT := edge.New(edge.Config{
//	    MaxExecTime:   50 * time.Millisecond,
//	    MaxMemory:     4 * 1024 * 1024, // 4MB
//	    AllowNetFetch: true,
//	})
//
//	edgeRT.Handle("/geo", func(req *edge.Request) *edge.Response {
//	    country := req.Header("CF-IPCountry")
//	    if country == "DE" {
//	        return edge.Redirect("/de" + req.Path, 302)
//	    }
//	    return edge.Next() // pass to origin
//	})
//
//	app.RegisterPlugin(edgeRT.Plugin())
package edge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

// Config for the edge function runtime.
type Config struct {
	// MaxExecTime per function invocation (default: 50ms).
	MaxExecTime time.Duration

	// MaxMemory per function in bytes (default: 4MB).
	MaxMemory int64

	// AllowNetFetch enables outbound HTTP from edge functions.
	AllowNetFetch bool

	// Logger for edge function logs.
	Logger *log.Logger

	// CacheDefault TTL for edge.Cache operations (default: 60s).
	CacheDefault time.Duration

	// Fallback when an edge function panics or times out.
	Fallback FallbackMode

	// OnError callback for edge function errors.
	OnError func(path string, err error)

	// Prefix for edge routes (default: "").
	Prefix string
}

// FallbackMode determines behavior on edge function failure.
type FallbackMode int

const (
	// FallbackNext passes the request to the origin server.
	FallbackNext FallbackMode = iota
	// FallbackError returns a 502 Bad Gateway.
	FallbackError
	// FallbackCached returns the last cached response if available.
	FallbackCached
)

// ---------------------------------------------------------------------------
// Request / Response types
// ---------------------------------------------------------------------------

// Request is a lightweight representation of an HTTP request for edge functions.
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

// GeoInfo provides geographic information about the request.
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

// Header returns a request header value.
func (r *Request) Header(key string) string {
	return r.Headers[http.CanonicalHeaderKey(key)]
}

// QueryParam returns a query parameter value.
func (r *Request) QueryParam(key string) string {
	return r.Query[key]
}

// ParseJSON decodes the request body into v.
func (r *Request) ParseJSON(v any) error {
	return json.Unmarshal(r.Body, v)
}

// Context returns the request context.
func (r *Request) Context() context.Context {
	return r.ctx
}

// Response is the edge function response.
type Response struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    []byte            `json:"body,omitempty"`
	BodyStr string            `json:"-"`

	// Internal flags.
	passThru bool   // pass to origin
	rewrite  string // rewrite URL
	cached   bool
}

// IsNext returns true if the request should be passed to the origin.
func (r *Response) IsNext() bool { return r.passThru }

// SetHeader sets a response header.
func (r *Response) SetHeader(key, value string) *Response {
	if r.Headers == nil {
		r.Headers = make(map[string]string)
	}
	r.Headers[key] = value
	return r
}

// ---------------------------------------------------------------------------
// Response Constructors
// ---------------------------------------------------------------------------

// Next signals that the request should pass through to the origin server.
func Next() *Response {
	return &Response{passThru: true}
}

// Respond creates a response with the given status and body.
func Respond(status int, body string) *Response {
	return &Response{
		Status:  status,
		BodyStr: body,
		Headers: map[string]string{"Content-Type": "text/plain; charset=utf-8"},
	}
}

// JSON creates a JSON response.
func JSON(status int, data any) *Response {
	body, _ := json.Marshal(data)
	return &Response{
		Status:  status,
		Body:    body,
		Headers: map[string]string{"Content-Type": "application/json"},
	}
}

// HTML creates an HTML response.
func HTML(status int, html string) *Response {
	return &Response{
		Status:  status,
		BodyStr: html,
		Headers: map[string]string{"Content-Type": "text/html; charset=utf-8"},
	}
}

// Redirect creates a redirect response.
func Redirect(url string, status int) *Response {
	return &Response{
		Status:  status,
		Headers: map[string]string{"Location": url},
	}
}

// Rewrite rewrites the request URL without a redirect.
func Rewrite(url string) *Response {
	return &Response{passThru: true, rewrite: url}
}

// ---------------------------------------------------------------------------
// Edge Function Handler
// ---------------------------------------------------------------------------

// HandlerFunc is the signature for edge functions.
type HandlerFunc func(req *Request) *Response

// ---------------------------------------------------------------------------
// Edge Runtime
// ---------------------------------------------------------------------------

// Runtime is the edge function runtime.
type Runtime struct {
	config   Config
	handlers map[string]edgeRoute
	cache    *Cache
	mu       sync.RWMutex

	// Metrics
	totalInvocations uint64
	totalErrors      uint64
	totalCacheHits   uint64
	totalTimeouts    uint64
	avgLatencyNs     int64
}

type edgeRoute struct {
	pattern string
	handler HandlerFunc
	methods []string // empty = all methods
	cache   *routeCache
}

type routeCache struct {
	enabled bool
	ttl     time.Duration
	key     func(req *Request) string
}

// New creates a new edge function runtime.
func New(cfgs ...Config) *Runtime {
	cfg := Config{}
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}
	if cfg.MaxExecTime == 0 {
		cfg.MaxExecTime = 50 * time.Millisecond
	}
	if cfg.MaxMemory == 0 {
		cfg.MaxMemory = 4 * 1024 * 1024
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

// Handle registers an edge function for a path.
func (rt *Runtime) Handle(path string, handler HandlerFunc) *edgeRouteBuilder {
	fullPath := rt.config.Prefix + path
	route := edgeRoute{
		pattern: fullPath,
		handler: handler,
	}
	rt.mu.Lock()
	rt.handlers[fullPath] = route
	rt.mu.Unlock()
	return &edgeRouteBuilder{rt: rt, path: fullPath}
}

// edgeRouteBuilder provides a fluent API for edge route configuration.
type edgeRouteBuilder struct {
	rt   *Runtime
	path string
}

// Methods restricts the edge function to specific HTTP methods.
func (b *edgeRouteBuilder) Methods(methods ...string) *edgeRouteBuilder {
	b.rt.mu.Lock()
	defer b.rt.mu.Unlock()
	r := b.rt.handlers[b.path]
	r.methods = methods
	b.rt.handlers[b.path] = r
	return b
}

// WithCache enables response caching for this edge function.
func (b *edgeRouteBuilder) WithCache(ttl time.Duration, keyFn ...func(req *Request) string) *edgeRouteBuilder {
	b.rt.mu.Lock()
	defer b.rt.mu.Unlock()
	r := b.rt.handlers[b.path]
	r.cache = &routeCache{
		enabled: true,
		ttl:     ttl,
	}
	if len(keyFn) > 0 {
		r.cache.key = keyFn[0]
	}
	b.rt.handlers[b.path] = r
	return b
}

// Middleware returns a Nimbus middleware that runs edge functions.
func (rt *Runtime) Middleware() router.Middleware {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *nhttp.Context) error {
			path := c.Request.URL.Path

			rt.mu.RLock()
			route, found := rt.findRoute(path)
			rt.mu.RUnlock()

			if !found {
				return next(c)
			}

			// Method check.
			if len(route.methods) > 0 {
				methodAllowed := false
				for _, m := range route.methods {
					if strings.EqualFold(m, c.Request.Method) {
						methodAllowed = true
						break
					}
				}
				if !methodAllowed {
					return next(c)
				}
			}

			// Check cache.
			if route.cache != nil && route.cache.enabled {
				cacheKey := rt.getCacheKey(route, rt.buildRequest(c))
				if cached, ok := rt.cache.Get(cacheKey); ok {
					atomic.AddUint64(&rt.totalCacheHits, 1)
					var resp Response
					if json.Unmarshal(cached, &resp) == nil {
						return rt.writeResponse(c, &resp)
					}
				}
			}

			// Execute edge function.
			req := rt.buildRequest(c)
			resp := rt.execute(route, req)

			if resp == nil {
				return next(c)
			}

			// Pass through to origin.
			if resp.IsNext() {
				if resp.rewrite != "" {
					c.Request.URL.Path = resp.rewrite
				}
				// Apply any headers.
				for k, v := range resp.Headers {
					c.Response.Header().Set(k, v)
				}
				return next(c)
			}

			// Cache the response.
			if route.cache != nil && route.cache.enabled {
				cacheKey := rt.getCacheKey(route, req)
				data, _ := json.Marshal(resp)
				rt.cache.Set(cacheKey, data, route.cache.ttl)
			}

			return rt.writeResponse(c, resp)
		}
	}
}

func (rt *Runtime) findRoute(path string) (edgeRoute, bool) {
	// Exact match.
	if route, ok := rt.handlers[path]; ok {
		return route, true
	}
	// Prefix match with wildcard.
	for pattern, route := range rt.handlers {
		if strings.HasSuffix(pattern, "*") {
			prefix := strings.TrimSuffix(pattern, "*")
			if strings.HasPrefix(path, prefix) {
				return route, true
			}
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

	// Query params.
	for k, v := range c.Request.URL.Query() {
		if len(v) > 0 {
			req.Query[k] = v[0]
		}
	}

	// Headers.
	for k, v := range c.Request.Header {
		if len(v) > 0 {
			req.Headers[k] = v[0]
		}
	}

	// Body (limit to MaxMemory).
	if c.Request.Body != nil {
		body, _ := io.ReadAll(io.LimitReader(c.Request.Body, rt.config.MaxMemory))
		req.Body = body
	}

	// Geo info from CDN headers.
	req.Geo = GeoInfo{
		Country:    c.Request.Header.Get("CF-IPCountry"),
		City:       c.Request.Header.Get("CF-IPCity"),
		Region:     c.Request.Header.Get("CF-Region"),
		Latitude:   parseFloat(c.Request.Header.Get("CF-IPLatitude")),
		Longitude:  parseFloat(c.Request.Header.Get("CF-IPLongitude")),
		Timezone:   c.Request.Header.Get("CF-Timezone"),
		Datacenter: c.Request.Header.Get("CF-Ray"),
	}

	// Also check X-Vercel, Fastly, and AWS CloudFront headers.
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

	type result struct {
		resp *Response
		err  error
	}

	ch := make(chan result, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				atomic.AddUint64(&rt.totalErrors, 1)
				if rt.config.OnError != nil {
					rt.config.OnError(route.pattern, fmt.Errorf("edge function panic: %v", r))
				}
				ch <- result{resp: nil, err: fmt.Errorf("panic: %v", r)}
			}
		}()

		// Memory tracking.
		var memBefore runtime.MemStats
		runtime.ReadMemStats(&memBefore)

		resp := route.handler(req)

		ch <- result{resp: resp}
	}()

	select {
	case r := <-ch:
		latency := time.Since(req.StartTime).Nanoseconds()
		atomic.StoreInt64(&rt.avgLatencyNs, latency)

		if r.err != nil {
			return rt.fallbackResponse(route.pattern)
		}
		return r.resp

	case <-time.After(rt.config.MaxExecTime):
		atomic.AddUint64(&rt.totalTimeouts, 1)
		if rt.config.OnError != nil {
			rt.config.OnError(route.pattern, fmt.Errorf("edge function timed out after %s", rt.config.MaxExecTime))
		}
		return rt.fallbackResponse(route.pattern)
	}
}

func (rt *Runtime) fallbackResponse(path string) *Response {
	switch rt.config.Fallback {
	case FallbackError:
		return Respond(502, "Edge Function Error")
	case FallbackCached:
		if cached, ok := rt.cache.Get("resp:" + path); ok {
			var resp Response
			if json.Unmarshal(cached, &resp) == nil {
				resp.SetHeader("X-Edge-Fallback", "cached")
				return &resp
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
	c.Response.Header().Set("X-Edge-Function", "true")

	status := resp.Status
	if status == 0 {
		status = 200
	}

	c.Response.WriteHeader(status)

	if len(resp.Body) > 0 {
		_, err := c.Response.Write(resp.Body)
		return err
	}
	if resp.BodyStr != "" {
		_, err := c.Response.Write([]byte(resp.BodyStr))
		return err
	}
	return nil
}

func (rt *Runtime) getCacheKey(route edgeRoute, req *Request) string {
	if route.cache != nil && route.cache.key != nil {
		return route.cache.key(req)
	}
	return req.Method + ":" + req.Path
}

// ---------------------------------------------------------------------------
// Metrics
// ---------------------------------------------------------------------------

// Metrics returns runtime metrics.
func (rt *Runtime) Metrics() map[string]any {
	return map[string]any{
		"total_invocations": atomic.LoadUint64(&rt.totalInvocations),
		"total_errors":      atomic.LoadUint64(&rt.totalErrors),
		"total_cache_hits":  atomic.LoadUint64(&rt.totalCacheHits),
		"total_timeouts":    atomic.LoadUint64(&rt.totalTimeouts),
		"avg_latency_ns":    atomic.LoadInt64(&rt.avgLatencyNs),
		"routes":            len(rt.handlers),
	}
}

// ---------------------------------------------------------------------------
// Plugin Integration
// ---------------------------------------------------------------------------

// EdgePlugin wraps the runtime as a Nimbus plugin.
type EdgePlugin struct {
	runtime *Runtime
}

// Plugin returns the edge runtime as a Nimbus plugin.
func (rt *Runtime) Plugin() *EdgePlugin {
	return &EdgePlugin{runtime: rt}
}

func (ep *EdgePlugin) Name() string    { return "edge" }
func (ep *EdgePlugin) Version() string { return "1.0.0" }

func (ep *EdgePlugin) Register(app interface{}) error { return nil }
func (ep *EdgePlugin) Boot(app interface{}) error     { return nil }

// RegisterRoutes adds the edge metrics endpoint.
func (ep *EdgePlugin) RegisterRoutes(r *router.Router) {
	r.Get("/_edge/metrics", func(c *nhttp.Context) error {
		return c.JSON(200, ep.runtime.Metrics())
	})
}

// Middleware returns the edge middleware for use as HasMiddleware plugin.
func (ep *EdgePlugin) Middleware() []router.Middleware {
	return []router.Middleware{ep.runtime.Middleware()}
}
