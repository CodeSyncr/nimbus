package tracing

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Recorder is an Exporter that keeps spans in memory, for tests.
type recorder struct {
	mu    sync.Mutex
	spans []SpanData
}

func (r *recorder) ExportSpans(s []SpanData) {
	r.mu.Lock()
	r.spans = append(r.spans, s...)
	r.mu.Unlock()
}
func (r *recorder) Shutdown(context.Context) error { return nil }

func (r *recorder) byName(name string) *SpanData {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.spans {
		if r.spans[i].Name == name {
			return &r.spans[i]
		}
	}
	return nil
}

func record(t *testing.T) *recorder {
	t.Helper()
	r := &recorder{}
	Global().SetExporter(r)
	Global().SetSampleRatio(1)
	t.Cleanup(func() { Global().SetExporter(nil); Global().SetSampleRatio(1) })
	return r
}

func TestTraceparentRoundTrip(t *testing.T) {
	const h = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	sc, err := ParseTraceparent(h)
	if err != nil {
		t.Fatal(err)
	}
	if sc.TraceID.String() != "4bf92f3577b34da6a3ce929d0e0e4736" || sc.SpanID.String() != "00f067aa0ba902b7" || !sc.Sampled || !sc.Remote {
		t.Fatalf("parsed %+v", sc)
	}
	if sc.Traceparent() != h {
		t.Fatalf("Traceparent() = %s", sc.Traceparent())
	}
	for _, bad := range []string{
		"", "garbage",
		"00-00000000000000000000000000000000-00f067aa0ba902b7-01", // zero trace id
		"00-4bf92f3577b34da6a3ce929d0e0e4736-0000000000000000-01", // zero span id
		"ff-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", // forbidden version
		"00-4bf92f3577b34da6a3ce929d0e0e47zz-00f067aa0ba902b7-01", // not hex
		"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01-extra",
	} {
		if _, err := ParseTraceparent(bad); err == nil {
			t.Errorf("ParseTraceparent(%q) should fail", bad)
		}
	}
	// Future versions may append fields.
	if _, err := ParseTraceparent("01-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01-future"); err != nil {
		t.Errorf("future version with extra field: %v", err)
	}
}

func TestStartBuildsParentChildTree(t *testing.T) {
	r := record(t)
	ctx, root := Start(context.Background(), "root")
	_, child := Start(ctx, "child", WithAttrs(map[string]any{"k": "v"}))
	child.RecordError(errors.New("bad"))
	child.End()
	child.End() // idempotent
	root.End()

	c, p := r.byName("child"), r.byName("root")
	if c == nil || p == nil || len(r.spans) != 2 {
		t.Fatalf("spans = %+v", r.spans)
	}
	if c.TraceID != p.TraceID || c.Parent != p.SpanID || p.Parent.IsValid() {
		t.Fatal("child is not linked to root")
	}
	if !c.Error || c.ErrorString != "bad" || c.Attrs["k"] != "v" {
		t.Fatalf("child = %+v", c)
	}
	if TraceIDFromContext(ctx) != p.TraceID.String() {
		t.Fatal("TraceIDFromContext mismatch")
	}
}

func TestSamplingIsDecidedOncePerTrace(t *testing.T) {
	r := record(t)
	Global().SetSampleRatio(0)
	ctx, root := Start(context.Background(), "unsampled")
	_, child := Start(ctx, "child")
	child.End()
	root.End()
	if len(r.spans) != 0 {
		t.Fatalf("exported %d spans at ratio 0", len(r.spans))
	}
	// A sampled caller wins over the local ratio.
	remote, _ := ParseTraceparent("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	_, s := Start(ContextWithRemote(context.Background(), remote), "continued")
	s.End()
	if r.byName("continued") == nil {
		t.Fatal("span of a sampled remote trace was dropped")
	}
	var nilSpan *Span
	nilSpan.SetAttr("x", 1) // no panic
	nilSpan.End()
}

func TestTransportInjectsTraceparent(t *testing.T) {
	r := record(t)
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		got = req.Header.Get(TraceparentHeader)
		w.WriteHeader(503)
	}))
	defer srv.Close()

	ctx, root := Start(context.Background(), "handler")
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/x?token=secret", nil)
	resp, err := (&http.Client{Transport: Transport(nil)}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	root.End()

	if req.Header.Get(TraceparentHeader) != "" {
		t.Fatal("Transport modified the caller's request")
	}
	sc, err := ParseTraceparent(got)
	if err != nil || sc.TraceID != root.SpanContext().TraceID {
		t.Fatalf("server saw traceparent %q (%v)", got, err)
	}
	client := r.byName("HTTP GET")
	if client == nil || client.SpanID != sc.SpanID || client.Parent != root.SpanContext().SpanID || !client.Error {
		t.Fatalf("client span = %+v", client)
	}
	if strings.Contains(client.Attrs["url.full"].(string), "secret") {
		t.Fatal("query string leaked into span attributes")
	}
}

func TestExtractInject(t *testing.T) {
	h := http.Header{}
	h.Set(TraceparentHeader, "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	h.Set(TracestateHeader, "vendor=1")
	ctx := Extract(context.Background(), h)
	out := http.Header{}
	Inject(ctx, out)
	if out.Get(TraceparentHeader) != h.Get(TraceparentHeader) || out.Get(TracestateHeader) != "vendor=1" {
		t.Fatalf("Inject = %v", out)
	}
	empty := http.Header{}
	Inject(context.Background(), empty)
	if len(empty) != 0 {
		t.Fatal("Inject without a span should write nothing")
	}
}

func TestOTLPExporterPostsJSON(t *testing.T) {
	var mu sync.Mutex
	var bodies []map[string]any
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		mu.Lock()
		bodies = append(bodies, m)
		auth = r.Header.Get("x-api-key")
		mu.Unlock()
	}))
	defer srv.Close()

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", srv.URL)
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "x-api-key=abc%3D")
	t.Setenv("OTEL_SERVICE_NAME", "billing")
	exp := ConfigureFromEnv()
	if exp == nil {
		t.Fatal("ConfigureFromEnv returned no exporter")
	}
	t.Cleanup(func() { Global().SetExporter(nil) })

	ctx, root := Start(context.Background(), "checkout", WithKind(KindServer))
	_, child := Start(ctx, "db", WithAttrs(map[string]any{"rows": 3, "ok": true, "ratio": 0.5}))
	child.End()
	root.End()
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := exp.Shutdown(sctx); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) == 0 || auth != "abc=" {
		t.Fatalf("collector got %d requests, auth=%q", len(bodies), auth)
	}
	rs := bodies[0]["resourceSpans"].([]any)[0].(map[string]any)
	res := rs["resource"].(map[string]any)["attributes"].([]any)
	foundService := false
	for _, a := range res {
		kv := a.(map[string]any)
		if kv["key"] == "service.name" && kv["value"].(map[string]any)["stringValue"] == "billing" {
			foundService = true
		}
	}
	if !foundService {
		t.Fatalf("service.name missing: %v", res)
	}
	spans := rs["scopeSpans"].([]any)[0].(map[string]any)["spans"].([]any)
	if len(spans) != 2 {
		t.Fatalf("exported %d spans", len(spans))
	}
	db := spans[0].(map[string]any)
	if db["name"] != "db" || db["parentSpanId"] != root.SpanContext().SpanID.String() || len(db["traceId"].(string)) != 32 {
		t.Fatalf("db span = %v", db)
	}
}
