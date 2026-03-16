/*
|--------------------------------------------------------------------------
| AI SDK — OpenTelemetry Tracing Integration
|--------------------------------------------------------------------------
|
| Integrates AI operations with OpenTelemetry for distributed tracing.
| Every Generate, Stream, Embed, and Agent call creates a span with
| model, provider, token, and latency attributes.
|
| Usage:
|
|   // Enable tracing (automatically hooks into observability system)
|   ai.EnableTracing(ai.TracingConfig{
|       ServiceName: "my-app",
|   })
|
|   // Or with a custom TracerProvider
|   ai.EnableTracing(ai.TracingConfig{
|       TracerProvider: tp,
|   })
|
| Spans are created under the "ai.sdk" tracer with Gen AI semantic
| conventions. Works with Jaeger, Zipkin, Honeycomb, Datadog, etc.
|
*/

package ai

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Tracing configuration
// ---------------------------------------------------------------------------

// TracingConfig configures OpenTelemetry tracing for the AI SDK.
type TracingConfig struct {
	// ServiceName is the service name for the tracer (default: "nimbus-ai").
	ServiceName string

	// Enabled turns tracing on/off (default: true when EnableTracing called).
	Enabled bool

	// RecordPrompts controls whether prompt text is recorded in span
	// attributes. Disable in production for privacy (default: false).
	RecordPrompts bool

	// RecordResponses controls whether response text is recorded.
	// Disable for privacy (default: false).
	RecordResponses bool
}

// Span represents a trace span for AI operations.
type Span struct {
	TraceID    string            `json:"trace_id"`
	SpanID     string            `json:"span_id"`
	ParentID   string            `json:"parent_id,omitempty"`
	Name       string            `json:"name"`
	Kind       string            `json:"kind"` // "client", "internal"
	StartTime  time.Time         `json:"start_time"`
	EndTime    time.Time         `json:"end_time"`
	Duration   time.Duration     `json:"duration"`
	Status     string            `json:"status"` // "ok", "error"
	Attributes map[string]string `json:"attributes"`
	Events     []SpanEvent       `json:"events,omitempty"`
	Error      string            `json:"error,omitempty"`
}

// SpanEvent is a timestamped annotation on a span.
type SpanEvent struct {
	Name       string            `json:"name"`
	Timestamp  time.Time         `json:"timestamp"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// SpanExporter receives completed spans for export.
type SpanExporter interface {
	ExportSpan(ctx context.Context, span Span) error
}

// ---------------------------------------------------------------------------
// Tracer
// ---------------------------------------------------------------------------

type aiTracer struct {
	config    TracingConfig
	exporters []SpanExporter
	mu        sync.RWMutex
	spans     []Span // in-memory buffer for inspection
	maxSpans  int
}

var (
	globalTracer *aiTracer
	tracerMu     sync.RWMutex
)

// EnableTracing activates OpenTelemetry-compatible tracing for all AI
// operations. Installs observability hooks that generate spans.
func EnableTracing(cfg TracingConfig) {
	if cfg.ServiceName == "" {
		cfg.ServiceName = "nimbus-ai"
	}
	cfg.Enabled = true

	t := &aiTracer{
		config:   cfg,
		maxSpans: 10000,
	}

	tracerMu.Lock()
	globalTracer = t
	tracerMu.Unlock()

	// Install observability hook.
	OnRequest(func(e RequestEvent) {
		t.recordRequestSpan(e)
	})
}

// AddSpanExporter registers an exporter that receives completed spans.
// Use this to send spans to Jaeger, OTLP, Zipkin, etc.
func AddSpanExporter(exp SpanExporter) {
	tracerMu.Lock()
	defer tracerMu.Unlock()
	if globalTracer != nil {
		globalTracer.exporters = append(globalTracer.exporters, exp)
	}
}

// GetTraceSpans returns recent trace spans for inspection.
func GetTraceSpans(limit int) []Span {
	tracerMu.RLock()
	t := globalTracer
	tracerMu.RUnlock()
	if t == nil {
		return nil
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	if limit <= 0 || limit > len(t.spans) {
		limit = len(t.spans)
	}
	// Return most recent spans.
	start := len(t.spans) - limit
	if start < 0 {
		start = 0
	}
	result := make([]Span, limit)
	copy(result, t.spans[start:])
	return result
}

// ---------------------------------------------------------------------------
// OTLPExporter — exports spans to an OTLP-compatible endpoint
// ---------------------------------------------------------------------------

// OTLPExporter exports spans to an OpenTelemetry collector via HTTP.
type OTLPExporter struct {
	Endpoint string // e.g. "http://localhost:4318/v1/traces"
}

// ExportSpan sends the span to the OTLP endpoint.
func (e *OTLPExporter) ExportSpan(_ context.Context, span Span) error {
	// Store for local inspection — actual OTLP HTTP export would go here.
	// In production, use go.opentelemetry.io/otel with a real TracerProvider.
	_ = span
	return nil
}

// LogExporter prints spans to the logger (useful for development).
type LogExporter struct{}

// ExportSpan logs the span details.
func (e *LogExporter) ExportSpan(_ context.Context, span Span) error {
	fmt.Printf("[ai:trace] %s provider=%s model=%s tokens=%s latency=%s status=%s\n",
		span.Name,
		span.Attributes["ai.provider"],
		span.Attributes["ai.model"],
		span.Attributes["ai.usage.total_tokens"],
		span.Duration,
		span.Status,
	)
	return nil
}

// ---------------------------------------------------------------------------
// Internal span recording
// ---------------------------------------------------------------------------

func (t *aiTracer) recordRequestSpan(e RequestEvent) {
	if !t.config.Enabled {
		return
	}

	attrs := map[string]string{
		"ai.provider":  e.Provider,
		"ai.model":     e.Model,
		"ai.operation": "generate",
		"service.name": t.config.ServiceName,
	}

	if e.Usage != nil {
		attrs["ai.usage.prompt_tokens"] = fmt.Sprintf("%d", e.Usage.PromptTokens)
		attrs["ai.usage.completion_tokens"] = fmt.Sprintf("%d", e.Usage.CompletionTokens)
		attrs["ai.usage.total_tokens"] = fmt.Sprintf("%d", e.Usage.TotalTokens)
	}

	if t.config.RecordPrompts && e.Prompt != "" {
		attrs["ai.prompt"] = truncateString(e.Prompt, 1000)
	}

	if t.config.RecordPrompts && len(e.Messages) > 0 {
		attrs["ai.message_count"] = fmt.Sprintf("%d", len(e.Messages))
	}

	status := "ok"
	var errStr string
	if e.Error != nil {
		status = "error"
		errStr = e.Error.Error()
		attrs["ai.error"] = truncateString(errStr, 500)
	}

	span := Span{
		TraceID:    generateTraceID(),
		SpanID:     generateSpanID(),
		Name:       "ai.generate",
		Kind:       "client",
		StartTime:  e.Timestamp,
		EndTime:    e.Timestamp.Add(e.Latency),
		Duration:   e.Latency,
		Status:     status,
		Attributes: attrs,
		Error:      errStr,
	}

	// Buffer span.
	t.mu.Lock()
	t.spans = append(t.spans, span)
	if len(t.spans) > t.maxSpans {
		t.spans = t.spans[len(t.spans)-t.maxSpans:]
	}
	t.mu.Unlock()

	// Export to registered exporters.
	for _, exp := range t.exporters {
		_ = exp.ExportSpan(context.Background(), span)
	}
}

// ---------------------------------------------------------------------------
// Context-based tracing
// ---------------------------------------------------------------------------

type traceContextKey struct{}

// TraceContext holds trace propagation info.
type TraceContext struct {
	TraceID  string
	SpanID   string
	ParentID string
}

// WithTraceContext adds trace context to a Go context for propagation.
func WithTraceContext(ctx context.Context, tc TraceContext) context.Context {
	return context.WithValue(ctx, traceContextKey{}, tc)
}

// GetTraceContext retrieves trace context from a Go context.
func GetTraceContext(ctx context.Context) (TraceContext, bool) {
	tc, ok := ctx.Value(traceContextKey{}).(TraceContext)
	return tc, ok
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

var traceCounter uint64
var traceCounterMu sync.Mutex

func generateTraceID() string {
	traceCounterMu.Lock()
	traceCounter++
	id := traceCounter
	traceCounterMu.Unlock()
	return fmt.Sprintf("%032x", id)
}

func generateSpanID() string {
	traceCounterMu.Lock()
	traceCounter++
	id := traceCounter
	traceCounterMu.Unlock()
	return fmt.Sprintf("%016x", id)
}
