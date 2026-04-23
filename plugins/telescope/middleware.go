package telescope

import (
	"bytes"
	"io"
	"strings"
	"time"

	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
	"github.com/go-chi/chi/v5"
)

const maxBodyCapture = 64 * 1024 // 64KB max for request/response body

// responseRecorder wraps ResponseWriter to capture status, size, and body.
type responseRecorder struct {
	http.ResponseWriter
	status int
	size   int
	body   *bytes.Buffer
}

func (r *responseRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	if r.body != nil && r.body.Len() < maxBodyCapture {
		remain := maxBodyCapture - r.body.Len()
		if remain > 0 && len(b) > 0 {
			toWrite := len(b)
			if toWrite > remain {
				toWrite = remain
			}
			r.body.Write(b[:toWrite])
		}
	}
	n, err := r.ResponseWriter.Write(b)
	r.size += n
	return n, err
}

// RequestWatcher returns middleware that records HTTP requests.
func (p *Plugin) RequestWatcher() router.Middleware {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *http.Context) error {
			// Skip telescope's own routes (respect TELESCOPE_PATH).
			if p.basePath != "" && strings.HasPrefix(c.Request.URL.Path, p.basePath) {
				return next(c)
			}
			start := time.Now()
			routePattern := ""
			if rc := chi.RouteContext(c.Request.Context()); rc != nil {
				// Prefer the most specific pattern (last).
				if n := len(rc.RoutePatterns); n > 0 {
					routePattern = rc.RoutePatterns[n-1]
				}
			}

			// Capture request body (restore for handler)
			var payload string
			if c.Request.Body != nil {
				body, _ := io.ReadAll(c.Request.Body)
				c.Request.Body = io.NopCloser(bytes.NewReader(body))
				if len(body) > 0 && len(body) <= maxBodyCapture {
					payload = string(body)
				} else if len(body) > maxBodyCapture {
					payload = string(body[:maxBodyCapture]) + "\n\n... (truncated)"
				}
			}

			rec := &responseRecorder{
				ResponseWriter: c.Response,
				status:         http.StatusOK,
				body:           &bytes.Buffer{},
			}
			c.Response = rec
			err := next(c)
			duration := time.Since(start)
			status := rec.status
			if status == 0 {
				status = http.StatusOK
			}

			// Capture response body (truncate if large)
			var responseBody string
			if rec.body.Len() > 0 {
				b := rec.body.Bytes()
				if len(b) <= maxBodyCapture {
					responseBody = string(b)
				} else {
					responseBody = string(b[:maxBodyCapture]) + "\n\n... (truncated)"
				}
			}

			reqHeaders := make(map[string]string)
			for k, v := range c.Request.Header {
				if len(v) > 0 && !isSensitiveHeader(k) {
					reqHeaders[k] = v[0]
				}
			}
			resHeaders := make(map[string]string)
			for k, v := range rec.Header() {
				if len(v) > 0 && !isSensitiveHeader(k) {
					resHeaders[k] = v[0]
				}
			}

			content := map[string]any{
				"method":           c.Request.Method,
				"path":             c.Request.URL.Path,
				"route":            routePattern,
				"handler":          "",
				"middleware":       nil,
				"query":            c.Request.URL.RawQuery,
				"request_headers":  reqHeaders,
				"payload":          payload,
				"response_status":  status,
				"duration_ms":      duration.Milliseconds(),
				"response_size":    rec.size,
				"response_headers": resHeaders,
				"response_body":    responseBody,
				"error":            err != nil,
			}
			if h, ok := c.Get("route_handler"); ok {
				content["handler"] = h
			}
			if mw, ok := c.Get("route_middleware"); ok {
				content["middleware"] = mw
			}
			if err != nil {
				content["error_message"] = err.Error()
			}
			tags := []string{statusCategory(status)}
			if err != nil {
				tags = append(tags, "error")
			}
			p.store.Record(&Entry{
				Type:    EntryRequest,
				Content: content,
				Tags:    tags,
			})
			return err
		}
	}
}

func isSensitiveHeader(name string) bool {
	lower := strings.ToLower(name)
	return lower == "authorization" || lower == "cookie" || strings.Contains(lower, "password") || strings.Contains(lower, "token")
}

func statusCategory(status int) string {
	switch {
	case status >= 500:
		return "5xx"
	case status >= 400:
		return "4xx"
	case status >= 300:
		return "3xx"
	case status >= 200:
		return "2xx"
	default:
		return "1xx"
	}
}

// Middleware returns named middleware provided by this plugin.
func (p *Plugin) Middleware() map[string]router.Middleware {
	return map[string]router.Middleware{
		"request": p.RequestWatcher(),
	}
}
