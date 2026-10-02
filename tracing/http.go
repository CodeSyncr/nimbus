package tracing

import (
	"context"
	"math"
	"net/http"
	"strconv"
)

// TraceparentHeader and TracestateHeader are the W3C Trace Context headers.
const (
	TraceparentHeader = "traceparent"
	TracestateHeader  = "tracestate"
)

// Extract returns ctx with the remote parent from the request's traceparent
// header, if it has a valid one.
func Extract(ctx context.Context, h http.Header) context.Context {
	sc, err := ParseTraceparent(h.Get(TraceparentHeader))
	if err != nil {
		return ctx
	}
	sc.TraceState = h.Get(TracestateHeader)
	return ContextWithRemote(ctx, sc)
}

// Inject writes the traceparent (and tracestate) of the span in ctx.
func Inject(ctx context.Context, h http.Header) {
	sc := SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return
	}
	h.Set(TraceparentHeader, sc.Traceparent())
	if sc.TraceState != "" {
		h.Set(TracestateHeader, sc.TraceState)
	}
}

// Transport wraps base (nil = http.DefaultTransport) so every outbound
// request gets a client span and a traceparent header.
func Transport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &transport{base: base}
}

type transport struct{ base http.RoundTripper }

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx, span := Start(req.Context(), "HTTP "+req.Method, WithKind(KindClient), WithAttrs(map[string]any{
		"http.request.method": req.Method,
		"url.full":            redactURL(req),
		"server.address":      req.URL.Hostname(),
	}))
	defer span.End()
	// RoundTrippers must not modify the caller's request.
	out := req.Clone(ctx)
	Inject(ctx, out.Header)
	resp, err := t.base.RoundTrip(out)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	span.SetAttr("http.response.status_code", resp.StatusCode)
	if resp.StatusCode >= 400 {
		span.SetError("HTTP " + strconv.Itoa(resp.StatusCode))
	}
	return resp, nil
}

// redactURL drops credentials and the query string, which often carry secrets.
func redactURL(req *http.Request) string {
	u := *req.URL
	u.User = nil
	u.RawQuery = ""
	return u.String()
}

func floatBits(f float64) uint64 { return math.Float64bits(f) }
func floatFrom(b uint64) float64 { return math.Float64frombits(b) }
