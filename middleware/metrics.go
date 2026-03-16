package middleware

import (
	"fmt"
	"net/http"
	"time"

	nhttp "github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/metrics"
	"github.com/CodeSyncr/nimbus/router"
)

// HTTP metrics collected by the Metrics middleware.
var (
	httpRequestsTotal = metrics.NewCounter(
		"http_requests_total",
		"Total number of HTTP requests.",
	)
	httpRequestDuration = metrics.NewHistogram(
		"http_request_duration_seconds",
		"HTTP request latency in seconds.",
		metrics.DefaultBuckets,
	)
	httpRequestsInFlight = metrics.NewGauge(
		"http_requests_in_flight",
		"Number of HTTP requests currently being served.",
	)
	httpResponseSize = metrics.NewCounter(
		"http_response_size_bytes",
		"Total bytes written in HTTP responses.",
	)
)

func init() {
	metrics.DefaultRegistry.Register(httpRequestsTotal)
	metrics.DefaultRegistry.Register(httpRequestDuration)
	metrics.DefaultRegistry.Register(httpRequestsInFlight)
	metrics.DefaultRegistry.Register(httpResponseSize)
}

// Metrics returns middleware that records Prometheus-compatible HTTP metrics:
//   - http_requests_total       (counter)  labels: method, path, status
//   - http_request_duration_seconds (histogram) labels: method, path, status
//   - http_requests_in_flight   (gauge)
//   - http_response_size_bytes  (counter)  labels: method, path, status
func Metrics() router.Middleware {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *nhttp.Context) error {
			labels := metrics.Labels{
				"method": c.Request.Method,
				"path":   c.Request.URL.Path,
			}
			httpRequestsInFlight.Inc(nil)
			start := time.Now()

			err := next(c)

			duration := time.Since(start).Seconds()
			httpRequestsInFlight.Dec(nil)

			status := statusFromWriter(c.Response)
			labels["status"] = fmt.Sprintf("%d", status)

			httpRequestsTotal.Inc(labels)
			httpRequestDuration.Observe(duration, labels)
			httpResponseSize.Add(uint64(bytesWritten(c.Response)), labels)

			return err
		}
	}
}

// statusFromWriter extracts the status code from an http.ResponseWriter.
// If the writer implements a StatusCode() method (common in wrapped writers),
// that is used. Otherwise defaults to 200.
func statusFromWriter(w http.ResponseWriter) int {
	type statusCoder interface {
		StatusCode() int
	}
	if sc, ok := w.(statusCoder); ok {
		code := sc.StatusCode()
		if code != 0 {
			return code
		}
	}
	return http.StatusOK
}

// bytesWritten extracts the total bytes written from the writer if it tracks them.
func bytesWritten(w http.ResponseWriter) int {
	type byteCounter interface {
		BytesWritten() int
	}
	if bc, ok := w.(byteCounter); ok {
		return bc.BytesWritten()
	}
	return 0
}
