package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
	"github.com/CodeSyncr/nimbus/tracing"
)

type spanRecorder struct {
	mu    sync.Mutex
	spans []tracing.SpanData
}

func (r *spanRecorder) ExportSpans(s []tracing.SpanData) {
	r.mu.Lock()
	r.spans = append(r.spans, s...)
	r.mu.Unlock()
}
func (r *spanRecorder) Shutdown(context.Context) error { return nil }

func TestTracingContinuesIncomingTrace(t *testing.T) {
	rec := &spanRecorder{}
	tracing.Global().SetExporter(rec)
	t.Cleanup(func() { tracing.Global().SetExporter(nil) })

	var traceID any
	var handlerTrace string
	r := router.New()
	r.Use(RequestID(), Tracing())
	r.Get("/users/:id", func(c *nhttp.Context) error {
		traceID, _ = c.Get("trace_id")
		handlerTrace = tracing.TraceIDFromContext(c.Request.Context())
		return c.JSON(500, map[string]string{"error": "x"})
	})

	req := httptest.NewRequest("GET", "/users/42", nil)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if traceID != "4bf92f3577b34da6a3ce929d0e0e4736" || handlerTrace != traceID {
		t.Fatalf("trace_id = %v, handler ctx trace = %s", traceID, handlerTrace)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.spans) != 1 {
		t.Fatalf("spans = %d", len(rec.spans))
	}
	s := rec.spans[0]
	if s.Kind != tracing.KindServer || s.Parent.String() != "00f067aa0ba902b7" {
		t.Fatalf("server span = %+v", s)
	}
	if s.Attrs["http.response.status_code"] != 500 || !s.Error {
		t.Fatalf("status not recorded: %+v", s.Attrs)
	}
	if s.Attrs["request.id"] == nil {
		t.Fatal("request id not attached")
	}
}

func TestRouteTemplate(t *testing.T) {
	if got := routeTemplate("/users/42/posts/7", map[string]string{"id": "42", "post": "7"}); got != "/users/:id/posts/:post" {
		t.Fatalf("routeTemplate = %s", got)
	}
	if got := routeTemplate("/health", nil); got != "/health" {
		t.Fatalf("routeTemplate = %s", got)
	}
	_ = http.StatusOK
}
