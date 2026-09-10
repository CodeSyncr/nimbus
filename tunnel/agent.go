package tunnel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"
)

// Agent is the local side of a tunnel: it dials the relay and serves the
// streams it receives with a reverse proxy to LocalAddr.
type Agent struct {
	// RelayURL is the relay's connect endpoint, e.g.
	// "wss://tunnel.nimbusgo.live/connect".
	RelayURL string
	// Token is the Nimbus Cloud CLI token from `nimbus login`.
	Token string
	// LocalAddr is the app being exposed, e.g. "127.0.0.1:3333".
	LocalAddr string
	// Subdomain asks for a fixed name (Pro plans); "" gets a random one.
	Subdomain string
	// KeepHost forwards the public Host header instead of LocalAddr. Useful
	// for apps that build absolute URLs from the request host.
	KeepHost bool
	// OnRequest, when set, is called after each proxied request.
	OnRequest func(RequestLog)
}

// RequestLog describes one request that passed through the tunnel.
type RequestLog struct {
	Method   string
	Path     string
	Status   int
	Duration time.Duration
}

// ConnectError is a rejection by the relay before the tunnel was
// established. Permanent reports whether retrying without changing
// anything is pointless (bad token, plan limit, name taken).
type ConnectError struct {
	Status  int
	Message string
}

func (e *ConnectError) Error() string { return e.Message }

// Permanent is true for rejections a reconnect loop must not retry.
func (e *ConnectError) Permanent() bool {
	switch e.Status {
	case http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusForbidden, http.StatusConflict, http.StatusBadRequest:
		return true
	}
	return false
}

// Session is an established tunnel.
type Session struct {
	// URL is the public address visitors use.
	URL string
	// Expires is when the relay will close the session (zero = never).
	Expires time.Time
	agent   *Agent
	sess    *yamux.Session
}

// NormalizeRelayURL accepts "https://host", "wss://host/connect" or a bare
// host and returns the WebSocket connect URL.
func NormalizeRelayURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("relay URL is empty")
	}
	if !strings.Contains(raw, "://") {
		raw = "wss://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("unsupported relay scheme %q", u.Scheme)
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = ConnectPath
	}
	return u.String(), nil
}

// Connect dials the relay and returns the established session. The caller
// then runs Session.Serve.
func (a *Agent) Connect(ctx context.Context) (*Session, error) {
	hdr := http.Header{}
	hdr.Set("Authorization", "Bearer "+a.Token)
	if a.Subdomain != "" {
		hdr.Set(HeaderSubdomain, a.Subdomain)
	}
	dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
	ws, resp, err := dialer.DialContext(ctx, a.RelayURL, hdr)
	if err != nil {
		if resp != nil {
			defer resp.Body.Close()
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			var payload struct {
				Error string `json:"error"`
			}
			msg := strings.TrimSpace(string(body))
			if json.Unmarshal(body, &payload) == nil && payload.Error != "" {
				msg = payload.Error
			}
			if msg == "" {
				msg = resp.Status
			}
			return nil, &ConnectError{Status: resp.StatusCode, Message: msg}
		}
		return nil, err
	}
	s := &Session{URL: resp.Header.Get(HeaderTunnelURL), agent: a}
	if v := resp.Header.Get(HeaderTunnelExpires); v != "" {
		s.Expires, _ = time.Parse(time.RFC3339, v)
	}
	s.sess, err = yamux.Server(newWSConn(ws), yamuxConfig())
	if err != nil {
		_ = ws.Close()
		return nil, err
	}
	return s, nil
}

// Serve proxies relay streams to the local app until the session ends or
// ctx is cancelled. It returns nil on a clean shutdown and the underlying
// error when the relay went away.
func (s *Session) Serve(ctx context.Context) error {
	target, err := url.Parse("http://" + s.agent.LocalAddr)
	if err != nil {
		return err
	}
	proxy := &httputil.ReverseProxy{
		FlushInterval: -1,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			if s.agent.KeepHost {
				pr.Out.Host = pr.In.Host
			}
			// Rewrite mode drops inbound X-Forwarded-* headers; the relay's
			// values are the truth (real visitor IP, https), so pass them on.
			for _, h := range []string{"X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto"} {
				if v := pr.In.Header.Get(h); v != "" {
					pr.Out.Header.Set(h, v)
				}
			}
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusBadGateway)
			fmt.Fprintf(w, "nimbus expose: nothing is listening on %s (%v)\n", s.agent.LocalAddr, err)
		},
	}
	srv := &http.Server{Handler: s.logging(proxy)}
	stop := context.AfterFunc(ctx, func() { _ = s.sess.Close() })
	defer stop()
	err = srv.Serve(s.sess)
	_ = srv.Close()
	if ctx.Err() != nil || errors.Is(err, yamux.ErrSessionShutdown) || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Close ends the session.
func (s *Session) Close() error { return s.sess.Close() }

func (s *Session) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.agent.OnRequest == nil {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.agent.OnRequest(RequestLog{Method: r.Method, Path: r.URL.RequestURI(), Status: rec.status, Duration: time.Since(start)})
	})
}

// statusRecorder captures the status code while keeping the writer's
// Flush/Hijack abilities, which the proxy needs for streaming and upgrades.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
