package middleware

import (
	"compress/gzip"
	"io"
	"strings"
	"sync"

	"github.com/CodeSyncr/nimbus/http"
	"github.com/CodeSyncr/nimbus/router"
)

// Gzip returns middleware that compresses response bodies using gzip when
// the client indicates support via Accept-Encoding. Small responses (< 256 B)
// are not compressed. Static assets served by CDN are typically excluded
// via route groups.
//
// Usage:
//
//	r.Use(middleware.Gzip())
func Gzip() router.Middleware {
	pool := sync.Pool{
		New: func() any {
			gz, _ := gzip.NewWriterLevel(io.Discard, gzip.DefaultCompression)
			return gz
		},
	}

	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c *http.Context) error {
			if !strings.Contains(c.Request.Header.Get("Accept-Encoding"), "gzip") {
				return next(c)
			}

			gz := pool.Get().(*gzip.Writer)
			defer pool.Put(gz)

			gw := &gzipResponseWriter{
				ResponseWriter: c.Response,
				gz:             gz,
				pool:           &pool,
				minSize:        256,
			}
			c.Response = gw

			err := next(c)

			// Flush and close the gzip writer if it was activated
			if gw.started {
				gz.Close()
			}

			return err
		}
	}
}

// gzipResponseWriter wraps http.ResponseWriter to gzip the body.
type gzipResponseWriter struct {
	http.ResponseWriter
	gz          *gzip.Writer
	pool        *sync.Pool
	buf         []byte
	started     bool
	minSize     int
	code        int
	codeWritten bool
}

func (w *gzipResponseWriter) WriteHeader(code int) {
	w.code = code
	w.codeWritten = true
	// Delay writing headers until we know whether to compress
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	if !w.started {
		w.buf = append(w.buf, b...)
		// Buffer until we have enough to decide
		if len(w.buf) < w.minSize {
			return len(b), nil
		}
		// Enough data — start compressing
		w.started = true
		w.ResponseWriter.Header().Set("Content-Encoding", "gzip")
		w.ResponseWriter.Header().Set("Vary", "Accept-Encoding")
		w.ResponseWriter.Header().Del("Content-Length")

		if w.codeWritten {
			w.ResponseWriter.WriteHeader(w.code)
		}

		w.gz.Reset(w.ResponseWriter)
		_, err := w.gz.Write(w.buf)
		w.buf = nil
		return len(b), err
	}
	return w.gz.Write(b)
}

// Flush flushes any buffered data. If gzip never activated (small response),
// write the buffered bytes directly.
func (w *gzipResponseWriter) Flush() {
	if !w.started && len(w.buf) > 0 {
		// Too small for gzip — write directly
		if w.codeWritten {
			w.ResponseWriter.WriteHeader(w.code)
		}
		w.ResponseWriter.Write(w.buf)
		w.buf = nil
		return
	}
	if w.started {
		w.gz.Flush()
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap exposes the underlying ResponseWriter for http.ResponseController.
func (w *gzipResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
