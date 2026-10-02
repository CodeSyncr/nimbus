package tracing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// OTLPConfig configures the OTLP/HTTP (JSON) exporter.
type OTLPConfig struct {
	// Endpoint is the full traces URL, e.g. http://localhost:4318/v1/traces.
	Endpoint string
	// Headers are sent with every export (API keys etc.).
	Headers map[string]string
	// ServiceName is the service.name resource attribute.
	ServiceName string
	// Resource holds extra resource attributes (deployment.environment, ...).
	Resource map[string]string
	// BatchSize flushes once this many spans are buffered (default 512).
	BatchSize int
	// FlushInterval flushes buffered spans at least this often (default 5s).
	FlushInterval time.Duration
	// MaxQueue drops spans beyond this many unsent (default 8192).
	MaxQueue int
	// Client overrides the HTTP client (default: 10s timeout).
	Client *http.Client
}

// OTLPExporter batches spans and POSTs them as OTLP/HTTP JSON.
type OTLPExporter struct {
	cfg     OTLPConfig
	mu      sync.Mutex
	buf     []SpanData
	dropped int
	kick    chan struct{}
	stop    chan struct{}
	done    chan struct{}
	once    sync.Once
}

// NewOTLPExporter starts an exporter. Call Shutdown to flush on exit.
func NewOTLPExporter(cfg OTLPConfig) *OTLPExporter {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 512
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 5 * time.Second
	}
	if cfg.MaxQueue <= 0 {
		cfg.MaxQueue = 8192
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: 10 * time.Second}
	}
	if cfg.ServiceName == "" {
		cfg.ServiceName = "nimbus-app"
	}
	e := &OTLPExporter{
		cfg:  cfg,
		kick: make(chan struct{}, 1),
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	go e.loop()
	return e
}

// ExportSpans implements Exporter. It never blocks on the network.
func (e *OTLPExporter) ExportSpans(spans []SpanData) {
	e.mu.Lock()
	room := e.cfg.MaxQueue - len(e.buf)
	if room < len(spans) {
		e.dropped += len(spans) - max(room, 0)
		spans = spans[:max(room, 0)]
	}
	e.buf = append(e.buf, spans...)
	full := len(e.buf) >= e.cfg.BatchSize
	e.mu.Unlock()
	if full {
		select {
		case e.kick <- struct{}{}:
		default:
		}
	}
}

// Flush sends everything buffered now.
func (e *OTLPExporter) Flush(ctx context.Context) error {
	for {
		e.mu.Lock()
		n := min(len(e.buf), e.cfg.BatchSize)
		batch := append([]SpanData(nil), e.buf[:n]...)
		e.buf = e.buf[n:]
		dropped := e.dropped
		e.dropped = 0
		e.mu.Unlock()
		if dropped > 0 {
			log.Printf("[tracing] dropped %d spans (export queue full)", dropped)
		}
		if len(batch) == 0 {
			return nil
		}
		if err := e.send(ctx, batch); err != nil {
			return err
		}
	}
}

// Shutdown flushes and stops the exporter.
func (e *OTLPExporter) Shutdown(ctx context.Context) error {
	e.once.Do(func() { close(e.stop) })
	select {
	case <-e.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	return e.Flush(ctx)
}

func (e *OTLPExporter) loop() {
	defer close(e.done)
	t := time.NewTicker(e.cfg.FlushInterval)
	defer t.Stop()
	for {
		select {
		case <-e.stop:
			return
		case <-t.C:
		case <-e.kick:
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		if err := e.Flush(ctx); err != nil {
			log.Printf("[tracing] export: %v", err)
		}
		cancel()
	}
}

func (e *OTLPExporter) send(ctx context.Context, batch []SpanData) error {
	body, err := json.Marshal(e.encode(batch))
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.cfg.Endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range e.cfg.Headers {
		req.Header.Set(k, v)
	}
	resp, err := e.cfg.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("collector returned %s", resp.Status)
	}
	return nil
}

// ── OTLP JSON encoding (trace and span IDs are hex in OTLP/JSON) ────

type otlpKV struct {
	Key   string    `json:"key"`
	Value otlpValue `json:"value"`
}

type otlpValue struct {
	StringValue *string  `json:"stringValue,omitempty"`
	BoolValue   *bool    `json:"boolValue,omitempty"`
	IntValue    *string  `json:"intValue,omitempty"` // int64 as string per proto3 JSON
	DoubleValue *float64 `json:"doubleValue,omitempty"`
}

func attrValue(v any) otlpValue {
	switch x := v.(type) {
	case string:
		return otlpValue{StringValue: &x}
	case bool:
		return otlpValue{BoolValue: &x}
	case int:
		s := strconv.FormatInt(int64(x), 10)
		return otlpValue{IntValue: &s}
	case int64:
		s := strconv.FormatInt(x, 10)
		return otlpValue{IntValue: &s}
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			s := strconv.FormatFloat(x, 'g', -1, 64)
			return otlpValue{StringValue: &s}
		}
		return otlpValue{DoubleValue: &x}
	default:
		s := fmt.Sprint(x)
		return otlpValue{StringValue: &s}
	}
}

func attrs(m map[string]any) []otlpKV {
	out := make([]otlpKV, 0, len(m))
	for k, v := range m {
		out = append(out, otlpKV{Key: k, Value: attrValue(v)})
	}
	return out
}

func (e *OTLPExporter) encode(batch []SpanData) map[string]any {
	res := map[string]any{"service.name": e.cfg.ServiceName, "telemetry.sdk.name": "nimbus"}
	for k, v := range e.cfg.Resource {
		res[k] = v
	}
	spans := make([]map[string]any, 0, len(batch))
	for _, d := range batch {
		s := map[string]any{
			"traceId":           d.TraceID.String(),
			"spanId":            d.SpanID.String(),
			"name":              d.Name,
			"kind":              int(d.Kind),
			"startTimeUnixNano": strconv.FormatInt(d.Start.UnixNano(), 10),
			"endTimeUnixNano":   strconv.FormatInt(d.End.UnixNano(), 10),
			"attributes":        attrs(d.Attrs),
		}
		if d.Parent.IsValid() {
			s["parentSpanId"] = d.Parent.String()
		}
		if d.TraceState != "" {
			s["traceState"] = d.TraceState
		}
		if d.Error {
			s["status"] = map[string]any{"code": 2, "message": d.ErrorString}
		}
		if len(d.Events) > 0 {
			evs := make([]map[string]any, 0, len(d.Events))
			for _, ev := range d.Events {
				evs = append(evs, map[string]any{
					"name":         ev.Name,
					"timeUnixNano": strconv.FormatInt(ev.Time.UnixNano(), 10),
					"attributes":   attrs(ev.Attrs),
				})
			}
			s["events"] = evs
		}
		spans = append(spans, s)
	}
	return map[string]any{
		"resourceSpans": []any{map[string]any{
			"resource": map[string]any{"attributes": attrs(res)},
			"scopeSpans": []any{map[string]any{
				"scope": map[string]any{"name": "github.com/CodeSyncr/nimbus/tracing"},
				"spans": spans,
			}},
		}},
	}
}

// ── Environment ─────────────────────────────────────────────────

// ConfigureFromEnv installs an OTLP exporter on the global tracer when an
// OTLP endpoint is set (see the package doc) and returns it, or nil. Spans
// and traceparent propagation work either way; without an exporter they
// are simply not sent anywhere.
func ConfigureFromEnv() *OTLPExporter {
	if v, _ := strconv.ParseFloat(os.Getenv("OTEL_TRACES_SAMPLER_ARG"), 64); os.Getenv("OTEL_TRACES_SAMPLER_ARG") != "" {
		global.SetSampleRatio(v)
	}
	if strings.EqualFold(os.Getenv("OTEL_SDK_DISABLED"), "true") {
		return nil
	}
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")
	if endpoint == "" {
		if base := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); base != "" {
			endpoint = strings.TrimRight(base, "/") + "/v1/traces"
		}
	}
	if endpoint == "" {
		return nil
	}
	headers := map[string]string{}
	for _, kv := range strings.Split(os.Getenv("OTEL_EXPORTER_OTLP_HEADERS"), ",") {
		if k, v, ok := strings.Cut(kv, "="); ok {
			// Values are URL-encoded per the OTLP exporter spec.
			if dec, err := url.QueryUnescape(strings.TrimSpace(v)); err == nil {
				v = dec
			}
			headers[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	service := os.Getenv("OTEL_SERVICE_NAME")
	if service == "" {
		service = os.Getenv("APP_NAME")
	}
	resource := map[string]string{}
	if env := os.Getenv("APP_ENV"); env != "" {
		resource["deployment.environment"] = env
	}
	exp := NewOTLPExporter(OTLPConfig{Endpoint: endpoint, Headers: headers, ServiceName: service, Resource: resource})
	global.SetExporter(exp)
	return exp
}

var _ Exporter = (*OTLPExporter)(nil)
