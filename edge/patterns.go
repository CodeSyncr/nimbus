package edge

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

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
