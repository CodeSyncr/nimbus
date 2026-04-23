package telescope

import (
	"net/http"
	"time"
)

// InstrumentRoundTripper wraps an http.RoundTripper and records each outgoing
// request to Telescope (HTTP Client watcher). Use with a custom client:
//
//	client := &http.Client{Transport: telescope.InstrumentRoundTripper(nil)}
//
// Passing nil uses http.DefaultTransport as the inner transport.
func InstrumentRoundTripper(inner http.RoundTripper) http.RoundTripper {
	if inner == nil {
		inner = http.DefaultTransport
	}
	if inner == nil {
		return nil
	}
	return &telescopeRoundTripper{inner: inner}
}

type telescopeRoundTripper struct {
	inner http.RoundTripper
}

func (t *telescopeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.inner == nil || req == nil {
		return nil, http.ErrUseLastResponse
	}
	start := time.Now()
	resp, err := t.inner.RoundTrip(req)
	dur := time.Since(start)
	status := 0
	if resp != nil {
		status = resp.StatusCode
	}
	u := ""
	if req.URL != nil {
		u = req.URL.String()
	}
	reqHdr := pickHeaders(req.Header)
	var respHdr map[string]string
	if resp != nil {
		respHdr = pickHeaders(resp.Header)
	}
	recordEntry(EntryHTTPClient, map[string]any{
		"method":           req.Method,
		"url":              u,
		"status":           status,
		"duration_ms":      dur.Milliseconds(),
		"request_headers":  reqHdr,
		"response_headers": respHdr,
		"error":            errString(err),
	})
	return resp, err
}

func pickHeaders(h http.Header) map[string]string {
	if h == nil {
		return nil
	}
	out := make(map[string]string)
	for k, v := range h {
		if len(v) == 0 || isSensitiveHeader(k) {
			continue
		}
		out[k] = v[0]
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
