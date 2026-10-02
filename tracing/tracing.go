/*
|--------------------------------------------------------------------------
| Distributed Tracing
|--------------------------------------------------------------------------
|
| W3C Trace Context propagation and spans, with an OTLP/HTTP exporter, so
| a request can be followed across services, outbound HTTP calls and the
| queue in any OpenTelemetry backend (Collector, Jaeger, Tempo, Honeycomb,
| Grafana, Datadog's OTLP intake, ...). No OpenTelemetry SDK dependency.
|
|   tracing.ConfigureFromEnv()            // OTEL_* variables, see below
|   app.Router.Use(middleware.Tracing())  // server spans + traceparent
|   client := &http.Client{Transport: tracing.Transport(nil)}
|
|   ctx, span := tracing.Start(ctx, "charge-card")
|   defer span.End()
|   span.SetAttr("order.id", id)
|
| Environment (standard OpenTelemetry names):
|   OTEL_EXPORTER_OTLP_TRACES_ENDPOINT  full URL, e.g. http://collector:4318/v1/traces
|   OTEL_EXPORTER_OTLP_ENDPOINT         base URL; /v1/traces is appended
|   OTEL_EXPORTER_OTLP_HEADERS          k1=v1,k2=v2 (e.g. API keys)
|   OTEL_SERVICE_NAME                   service.name resource attribute
|   OTEL_TRACES_SAMPLER_ARG             sampling ratio for new traces (0..1, default 1)
|   OTEL_SDK_DISABLED=true              propagate IDs but export nothing
|
*/

package tracing

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// TraceID identifies a trace (16 bytes).
type TraceID [16]byte

// SpanID identifies a span (8 bytes).
type SpanID [8]byte

func (t TraceID) String() string { return hex.EncodeToString(t[:]) }
func (s SpanID) String() string  { return hex.EncodeToString(s[:]) }

// IsValid reports whether the ID is non-zero.
func (t TraceID) IsValid() bool { return t != TraceID{} }

// IsValid reports whether the ID is non-zero.
func (s SpanID) IsValid() bool { return s != SpanID{} }

// SpanContext is the part of a span that crosses process boundaries.
type SpanContext struct {
	TraceID    TraceID
	SpanID     SpanID
	Sampled    bool
	TraceState string // opaque vendor data, passed through unchanged
	Remote     bool
}

// IsValid reports whether both IDs are set.
func (sc SpanContext) IsValid() bool { return sc.TraceID.IsValid() && sc.SpanID.IsValid() }

// Traceparent renders the W3C traceparent header value.
func (sc SpanContext) Traceparent() string {
	flags := "00"
	if sc.Sampled {
		flags = "01"
	}
	return "00-" + sc.TraceID.String() + "-" + sc.SpanID.String() + "-" + flags
}

// ParseTraceparent parses a W3C traceparent header value.
func ParseTraceparent(h string) (SpanContext, error) {
	h = strings.TrimSpace(h)
	parts := strings.Split(h, "-")
	if len(parts) < 4 || len(parts[0]) != 2 || len(parts[1]) != 32 || len(parts[2]) != 16 || len(parts[3]) != 2 {
		return SpanContext{}, fmt.Errorf("tracing: malformed traceparent %q", h)
	}
	if parts[0] == "ff" || (parts[0] == "00" && len(parts) != 4) {
		return SpanContext{}, fmt.Errorf("tracing: unsupported traceparent version %q", parts[0])
	}
	var sc SpanContext
	if _, err := hex.Decode(sc.TraceID[:], []byte(parts[1])); err != nil {
		return SpanContext{}, fmt.Errorf("tracing: bad trace id: %w", err)
	}
	if _, err := hex.Decode(sc.SpanID[:], []byte(parts[2])); err != nil {
		return SpanContext{}, fmt.Errorf("tracing: bad span id: %w", err)
	}
	var flags [1]byte
	if _, err := hex.Decode(flags[:], []byte(parts[3])); err != nil {
		return SpanContext{}, fmt.Errorf("tracing: bad flags: %w", err)
	}
	if !sc.IsValid() {
		return SpanContext{}, fmt.Errorf("tracing: zero trace or span id")
	}
	sc.Sampled = flags[0]&1 == 1
	sc.Remote = true
	return sc, nil
}

// SpanKind follows OpenTelemetry's span kinds.
type SpanKind int

const (
	KindInternal SpanKind = 1
	KindServer   SpanKind = 2
	KindClient   SpanKind = 3
	KindProducer SpanKind = 4
	KindConsumer SpanKind = 5
)

// Span is one timed operation. Methods are safe for concurrent use and are
// no-ops on a nil span.
type Span struct {
	mu      sync.Mutex
	sc      SpanContext
	parent  SpanID
	name    string
	kind    SpanKind
	start   time.Time
	end     time.Time
	attrs   map[string]any
	events  []Event
	errMsg  string
	isError bool
	ended   bool
	tracer  *Tracer
}

// Event is a timestamped annotation on a span.
type Event struct {
	Name  string
	Time  time.Time
	Attrs map[string]any
}

// SpanContext returns the span's propagation context.
func (s *Span) SpanContext() SpanContext {
	if s == nil {
		return SpanContext{}
	}
	return s.sc
}

// SetAttr records an attribute (string, bool, int, int64, float64 or
// anything else, which is formatted with %v).
func (s *Span) SetAttr(key string, value any) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.attrs == nil {
		s.attrs = map[string]any{}
	}
	s.attrs[key] = value
}

// AddEvent records a timestamped event.
func (s *Span) AddEvent(name string, attrs map[string]any) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, Event{Name: name, Time: time.Now(), Attrs: attrs})
}

// RecordError marks the span as failed.
func (s *Span) RecordError(err error) {
	if s == nil || err == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.isError = true
	s.errMsg = err.Error()
	s.events = append(s.events, Event{Name: "exception", Time: time.Now(), Attrs: map[string]any{
		"exception.message": err.Error(),
	}})
}

// SetError marks the span as failed with a description (e.g. "HTTP 503").
func (s *Span) SetError(description string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.isError = true
	s.errMsg = description
}

// End finishes the span and hands it to the exporter. Calling it again does
// nothing.
func (s *Span) End() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.ended {
		s.mu.Unlock()
		return
	}
	s.ended = true
	s.end = time.Now()
	data := s.snapshotLocked()
	s.mu.Unlock()
	if s.sc.Sampled && s.tracer != nil {
		s.tracer.export(data)
	}
}

// SpanData is a finished span as handed to an Exporter.
type SpanData struct {
	TraceID     TraceID
	SpanID      SpanID
	Parent      SpanID
	TraceState  string
	Name        string
	Kind        SpanKind
	Start, End  time.Time
	Attrs       map[string]any
	Events      []Event
	Error       bool
	ErrorString string
}

func (s *Span) snapshotLocked() SpanData {
	attrs := make(map[string]any, len(s.attrs))
	for k, v := range s.attrs {
		attrs[k] = v
	}
	return SpanData{
		TraceID: s.sc.TraceID, SpanID: s.sc.SpanID, Parent: s.parent, TraceState: s.sc.TraceState,
		Name: s.name, Kind: s.kind, Start: s.start, End: s.end,
		Attrs: attrs, Events: append([]Event(nil), s.events...),
		Error: s.isError, ErrorString: s.errMsg,
	}
}

// ── Context ─────────────────────────────────────────────────────

type spanKey struct{}
type remoteKey struct{}

// ContextWithSpan returns ctx carrying span.
func ContextWithSpan(ctx context.Context, s *Span) context.Context {
	return context.WithValue(ctx, spanKey{}, s)
}

// SpanFromContext returns the current span, or nil.
func SpanFromContext(ctx context.Context) *Span {
	s, _ := ctx.Value(spanKey{}).(*Span)
	return s
}

// ContextWithRemote returns ctx carrying a parent received from another
// process (traceparent header, queue job metadata).
func ContextWithRemote(ctx context.Context, sc SpanContext) context.Context {
	return context.WithValue(ctx, remoteKey{}, sc)
}

// SpanContextFromContext returns the current span's context, or a remote
// parent's, or an invalid SpanContext.
func SpanContextFromContext(ctx context.Context) SpanContext {
	if s := SpanFromContext(ctx); s != nil {
		return s.sc
	}
	sc, _ := ctx.Value(remoteKey{}).(SpanContext)
	return sc
}

// TraceIDFromContext returns the current trace ID as hex, or "".
func TraceIDFromContext(ctx context.Context) string {
	if sc := SpanContextFromContext(ctx); sc.IsValid() {
		return sc.TraceID.String()
	}
	return ""
}

// ── Tracer ──────────────────────────────────────────────────────

// Exporter receives finished, sampled spans.
type Exporter interface {
	ExportSpans(spans []SpanData)
	Shutdown(ctx context.Context) error
}

// Tracer starts spans and sends them to an exporter.
type Tracer struct {
	exporter    atomic.Pointer[exporterBox]
	sampleRatio atomic.Uint64 // float64 bits
}

type exporterBox struct{ Exporter }

var global = func() *Tracer {
	t := &Tracer{}
	t.SetSampleRatio(1)
	return t
}()

// Global returns the process-wide tracer.
func Global() *Tracer { return global }

// SetExporter sets where finished spans go (nil drops them).
func (t *Tracer) SetExporter(e Exporter) {
	if e == nil {
		t.exporter.Store(nil)
		return
	}
	t.exporter.Store(&exporterBox{e})
}

// SetSampleRatio sets the share of new traces that are recorded (0..1).
// Traces continued from a caller follow the caller's sampling decision.
func (t *Tracer) SetSampleRatio(r float64) {
	r = max(0, min(1, r))
	t.sampleRatio.Store(floatBits(r))
}

func (t *Tracer) export(d SpanData) {
	if box := t.exporter.Load(); box != nil {
		box.ExportSpans([]SpanData{d})
	}
}

// StartOption configures a span.
type StartOption func(*Span)

// WithKind sets the span kind (default internal).
func WithKind(k SpanKind) StartOption { return func(s *Span) { s.kind = k } }

// WithAttrs sets initial attributes.
func WithAttrs(attrs map[string]any) StartOption {
	return func(s *Span) {
		for k, v := range attrs {
			if s.attrs == nil {
				s.attrs = map[string]any{}
			}
			s.attrs[k] = v
		}
	}
}

// WithParent starts the span under sc instead of the span in ctx.
func WithParent(sc SpanContext) StartOption {
	return func(s *Span) {
		if sc.IsValid() {
			s.sc.TraceID, s.parent, s.sc.Sampled, s.sc.TraceState = sc.TraceID, sc.SpanID, sc.Sampled, sc.TraceState
		}
	}
}

// Start begins a span under the span (or remote parent) in ctx, or a new
// trace if there is none, and returns ctx carrying it.
func (t *Tracer) Start(ctx context.Context, name string, opts ...StartOption) (context.Context, *Span) {
	s := &Span{name: name, kind: KindInternal, start: time.Now(), tracer: t}
	if parent := SpanContextFromContext(ctx); parent.IsValid() {
		s.sc.TraceID, s.parent, s.sc.Sampled, s.sc.TraceState = parent.TraceID, parent.SpanID, parent.Sampled, parent.TraceState
	}
	for _, o := range opts {
		o(s)
	}
	if !s.sc.TraceID.IsValid() {
		s.sc.TraceID = newTraceID()
		s.sc.Sampled = t.sample(s.sc.TraceID)
	}
	s.sc.SpanID = newSpanID()
	return ContextWithSpan(ctx, s), s
}

// Start begins a span on the global tracer.
func Start(ctx context.Context, name string, opts ...StartOption) (context.Context, *Span) {
	return global.Start(ctx, name, opts...)
}

// sample decides from the trace ID so every service makes the same call.
func (t *Tracer) sample(id TraceID) bool {
	r := floatFrom(t.sampleRatio.Load())
	if r >= 1 {
		return true
	}
	if r <= 0 {
		return false
	}
	// Lower 8 bytes as a fraction of the uint64 range.
	return float64(binary.BigEndian.Uint64(id[8:])) < r*float64(^uint64(0))
}

func newTraceID() TraceID {
	var id TraceID
	for !id.IsValid() {
		_, _ = rand.Read(id[:])
	}
	return id
}

func newSpanID() SpanID {
	var id SpanID
	for !id.IsValid() {
		_, _ = rand.Read(id[:])
	}
	return id
}
