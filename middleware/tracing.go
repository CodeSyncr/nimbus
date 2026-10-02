package middleware

import (
	stdhttp "net/http"
	"strconv"
	"strings"

	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
	"github.com/CodeSyncr/nimbus/tracing"
)

// Tracing starts a server span for every request, continuing the caller's
// trace when the request carries a W3C traceparent header. The span is on
// the request context (so tracing.Transport, queue dispatches and
// tracing.Start inside handlers join the trace), the trace ID is stored
// under "trace_id" for logs, and finished spans go to the exporter set by
// tracing.ConfigureFromEnv. Place it right after RequestID.
func Tracing() router.Middleware {
	return router.NameMiddleware("tracing", func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *http.Context) error {
			r := c.Request
			ctx := tracing.Extract(r.Context(), r.Header)
			route := routeTemplate(r.URL.Path, c.Params)
			ctx, span := tracing.Start(ctx, r.Method+" "+route, tracing.WithKind(tracing.KindServer), tracing.WithAttrs(map[string]any{
				"http.request.method": r.Method,
				"http.route":          route,
				"url.path":            r.URL.Path,
				"client.address":      r.RemoteAddr,
				"user_agent.original": r.UserAgent(),
			}))
			defer span.End()
			if id, ok := c.Get("request_id"); ok {
				span.SetAttr("request.id", id)
			}
			c.Request = r.WithContext(ctx)
			c.Set("trace_id", span.SpanContext().TraceID.String())

			rec := &statusRecorder{ResponseWriter: c.Response, status: stdhttp.StatusOK}
			original := c.Response
			c.Response = rec
			err := next(c)
			c.Response = original

			status := rec.status
			if !rec.written && err != nil {
				status = statusFromError(err)
			}
			span.SetAttr("http.response.status_code", status)
			if status >= 500 {
				if err != nil {
					span.RecordError(err)
				} else {
					span.SetError("HTTP " + strconv.Itoa(status))
				}
			}
			return err
		}
	})
}

// routeTemplate turns /users/42 with {id: 42} into /users/:id so span names
// stay low-cardinality.
func routeTemplate(path string, params map[string]string) string {
	if len(params) == 0 {
		return path
	}
	byValue := make(map[string]string, len(params))
	for k, v := range params {
		if v != "" {
			byValue[v] = k
		}
	}
	segs := strings.Split(path, "/")
	for i, s := range segs {
		if name, ok := byValue[s]; ok {
			segs[i] = ":" + name
		}
	}
	return strings.Join(segs, "/")
}
