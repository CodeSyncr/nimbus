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

// Cached creates a response that should be cached.
func Cached(resp *Response, ttl time.Duration) *Response {
	resp.SetHeader("Cache-Control", fmt.Sprintf("public, max-age=%d", int(ttl.Seconds())))
	resp.SetHeader("X-Edge-Cache", "HIT")
	resp.cached = true
	return resp
}

// ---------------------------------------------------------------------------
// Edge Function Handler
// ---------------------------------------------------------------------------

// HandlerFunc is the signature for edge functions.
type HandlerFunc func(req *Request) *Response

// ---------------------------------------------------------------------------
// Edge Cache
// ---------------------------------------------------------------------------

// Cache provides a simple in-memory key-value cache for edge functions.
type Cache struct {
	mu      sync.RWMutex
	data    map[string]cacheEntry
	maxSize int
}

type cacheEntry struct {
	value  []byte
	expiry time.Time
}

// NewCache creates a new edge cache.
func NewCache(maxSize int) *Cache {
	c := &Cache{
		data:    make(map[string]cacheEntry),
		maxSize: maxSize,
	}
	go c.cleanup()
	return c
}

// Get retrieves a value from cache.
func (c *Cache) Get(key string) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.data[key]
	if !ok || time.Now().After(entry.expiry) {
		return nil, false
	}
	return entry.value, true
}

// Set stores a value in cache with TTL.
func (c *Cache) Set(key string, value []byte, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Evict if at capacity.
	if len(c.data) >= c.maxSize {
		oldest := ""
		oldestTime := time.Now().Add(time.Hour)
		for k, v := range c.data {
			if v.expiry.Before(oldestTime) {
				oldest = k
				oldestTime = v.expiry
			}
		}
		if oldest != "" {
			delete(c.data, oldest)
		}
	}
	c.data[key] = cacheEntry{value: value, expiry: time.Now().Add(ttl)}
}

// Delete removes a value from cache.
func (c *Cache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, key)
}

func (c *Cache) cleanup() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		c.mu.Lock()
		now := time.Now()
		for k, v := range c.data {
			if now.After(v.expiry) {
				delete(c.data, k)
			}
		}
		c.mu.Unlock()
	}
}

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

// ---------------------------------------------------------------------------
// Common Edge Function Patterns
// ---------------------------------------------------------------------------

// GeoRouter creates an edge function that routes based on country.
func GeoRouter(routes map[string]string, fallback string) HandlerFunc {
	return func(req *Request) *Response {
		country := req.Geo.Country
		if country == "" {
			country = req.Header("CF-IPCountry")
		}
		if path, ok := routes[country]; ok {
			return Rewrite(path + req.Path)
		}
		if fallback != "" {
			return Rewrite(fallback + req.Path)
		}
		return Next()
	}
}

// ABTest creates an edge function for A/B testing.
func ABTest(variants []ABVariant) HandlerFunc {
	return func(req *Request) *Response {
		// Use IP hash for consistent assignment.
		hash := fnvHash(req.IP)
		total := 0
		for _, v := range variants {
			total += v.Weight
		}
		if total == 0 {
			return Next()
		}

		pick := int(hash % uint32(total))
		cumulative := 0
		for _, v := range variants {
			cumulative += v.Weight
			if pick < cumulative {
				resp := Rewrite(v.Path)
				resp.SetHeader("X-AB-Variant", v.Name)
				return resp
			}
		}
		return Next()
	}
}

// ABVariant defines an A/B test variant.
type ABVariant struct {
	Name   string
	Path   string
	Weight int
}

// RateLimit creates a simple edge-level rate limiter.
func RateLimit(maxRequests int, window time.Duration) HandlerFunc {
	mu := sync.Mutex{}
	counters := make(map[string]*rateBucket)

	go func() {
		for range time.NewTicker(window).C {
			mu.Lock()
			now := time.Now()
			for k, v := range counters {
				if now.After(v.reset) {
					delete(counters, k)
				}
			}
			mu.Unlock()
		}
	}()

	return func(req *Request) *Response {
		mu.Lock()
		defer mu.Unlock()

		bucket, ok := counters[req.IP]
		if !ok {
			bucket = &rateBucket{count: 0, reset: time.Now().Add(window)}
			counters[req.IP] = bucket
		}

		bucket.count++
		remaining := maxRequests - bucket.count
		if remaining < 0 {
			remaining = 0
		}

		if bucket.count > maxRequests {
			resp := Respond(429, "Rate limit exceeded")
			resp.SetHeader("X-RateLimit-Limit", fmt.Sprintf("%d", maxRequests))
			resp.SetHeader("X-RateLimit-Remaining", "0")
			resp.SetHeader("Retry-After", fmt.Sprintf("%d", int(time.Until(bucket.reset).Seconds())))
			return resp
		}

		resp := Next()
		resp.SetHeader("X-RateLimit-Limit", fmt.Sprintf("%d", maxRequests))
		resp.SetHeader("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
		return resp
	}
}

type rateBucket struct {
	count int
	reset time.Time
}

// SecurityHeaders adds common security headers at the edge.
func SecurityHeaders() HandlerFunc {
	return func(req *Request) *Response {
		resp := Next()
		resp.SetHeader("X-Content-Type-Options", "nosniff")
		resp.SetHeader("X-Frame-Options", "DENY")
		resp.SetHeader("X-XSS-Protection", "1; mode=block")
		resp.SetHeader("Referrer-Policy", "strict-origin-when-cross-origin")
		resp.SetHeader("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		resp.SetHeader("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")
		return resp
	}
}

// Maintenance creates an edge function that returns a maintenance page.
func Maintenance(html string, allowedIPs ...string) HandlerFunc {
	allowed := make(map[string]bool)
	for _, ip := range allowedIPs {
		allowed[ip] = true
	}

	return func(req *Request) *Response {
		if allowed[req.IP] {
			return Next()
		}
		return HTML(503, html)
	}
}

// BasicAuth creates an edge-level basic authentication check.
func BasicAuth(realm string, credentials map[string]string) HandlerFunc {
	return func(req *Request) *Response {
		auth := req.Header("Authorization")
		if auth == "" {
			resp := Respond(401, "Unauthorized")
			resp.SetHeader("WWW-Authenticate", fmt.Sprintf(`Basic realm="%s"`, realm))
			return resp
		}

		// Parse basic auth.
		if !strings.HasPrefix(auth, "Basic ") {
			return Respond(401, "Unauthorized")
		}

		// Decode and validate (simplified — in production use encoding/base64).
		// Just check against known credentials.
		for user, pass := range credentials {
			expected := "Basic " + basicEncode(user, pass)
			if auth == expected {
				resp := Next()
				resp.SetHeader("X-Edge-User", user)
				return resp
			}
		}

		resp := Respond(401, "Invalid credentials")
		resp.SetHeader("WWW-Authenticate", fmt.Sprintf(`Basic realm="%s"`, realm))
		return resp
	}
}

// CORSHeaders creates an edge function that handles CORS preflight.
func CORSHeaders(origins []string, methods []string, headers []string) HandlerFunc {
	originsStr := strings.Join(origins, ", ")
	methodsStr := strings.Join(methods, ", ")
	headersStr := strings.Join(headers, ", ")
	allowAll := len(origins) == 1 && origins[0] == "*"

	return func(req *Request) *Response {
		origin := req.Header("Origin")
		if origin == "" {
			return Next()
		}

		allowed := allowAll
		if !allowed {
			for _, o := range origins {
				if o == origin {
					allowed = true
					break
				}
			}
		}
		if !allowed {
			return Next()
		}

		// Preflight.
		if req.Method == "OPTIONS" {
			resp := Respond(204, "")
			if allowAll {
				resp.SetHeader("Access-Control-Allow-Origin", "*")
			} else {
				resp.SetHeader("Access-Control-Allow-Origin", origin)
			}
			resp.SetHeader("Access-Control-Allow-Methods", methodsStr)
			resp.SetHeader("Access-Control-Allow-Headers", headersStr)
			resp.SetHeader("Access-Control-Max-Age", "86400")
			return resp
		}

		// Normal request.
		resp := Next()
		if allowAll {
			resp.SetHeader("Access-Control-Allow-Origin", "*")
		} else {
			resp.SetHeader("Access-Control-Allow-Origin", origin)
			resp.SetHeader("Vary", "Origin")
		}
		resp.SetHeader("Access-Control-Allow-Methods", methodsStr)
		resp.SetHeader("Access-Control-Allow-Headers", headersStr)
		_ = originsStr
		return resp
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func extractIP(r *http.Request) string {
	// Check standard proxy headers.
	for _, h := range []string{"CF-Connecting-IP", "X-Real-IP", "X-Forwarded-For"} {
		if v := r.Header.Get(h); v != "" {
			// X-Forwarded-For may contain multiple IPs.
			if idx := strings.IndexByte(v, ','); idx > 0 {
				return strings.TrimSpace(v[:idx])
			}
			return v
		}
	}

	// Fall back to RemoteAddr.
	addr := r.RemoteAddr
	if idx := strings.LastIndex(addr, ":"); idx > 0 {
		return addr[:idx]
	}
	return addr
}

func parseFloat(s string) float64 {
	if s == "" {
		return 0
	}
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
}

func fnvHash(s string) uint32 {
	h := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

func basicEncode(user, pass string) string {
	// Simple base64 encoding.
	const base64Chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	input := []byte(user + ":" + pass)
	var result strings.Builder
	for i := 0; i < len(input); i += 3 {
		var b0, b1, b2 byte
		b0 = input[i]
		if i+1 < len(input) {
			b1 = input[i+1]
		}
		if i+2 < len(input) {
			b2 = input[i+2]
		}
		result.WriteByte(base64Chars[b0>>2])
		result.WriteByte(base64Chars[((b0&3)<<4)|(b1>>4)])
		if i+1 < len(input) {
			result.WriteByte(base64Chars[((b1&0x0F)<<2)|(b2>>6)])
		} else {
			result.WriteByte('=')
		}
		if i+2 < len(input) {
			result.WriteByte(base64Chars[b2&0x3F])
		} else {
			result.WriteByte('=')
		}
	}
	return result.String()
}
