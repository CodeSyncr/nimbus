package tunnel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"
)

// Authorizer resolves a CLI token to a Grant. Return ErrUnauthorized (or an
// error wrapping it) for a bad token; any other error is a relay-side fault.
type Authorizer func(ctx context.Context, token string) (Grant, error)

// Relay is the public side of a tunnel. It serves two kinds of traffic on
// one listener: agent connections on ConnectPath (any host that is not a
// tunnel subdomain) and visitor requests on "<name>.<Domain>".
type Relay struct {
	// Domain is the suffix under which tunnels are published, e.g.
	// "nimbusgo.live" publishes "brisk-otter-3f9a.nimbusgo.live".
	Domain    string
	Authorize Authorizer
	// Logf receives one line per tunnel open/close (nil = silent).
	Logf func(format string, args ...any)

	upgrader websocket.Upgrader
	mu       sync.Mutex
	tunnels  map[string]*tunnelSession
	byUser   map[uint]int
}

type tunnelSession struct {
	id      string
	name    string
	userID  uint
	sess    *yamux.Session
	proxy   *httputil.ReverseProxy
	expires time.Time
}

// NewRelay returns a relay publishing tunnels under domain.
func NewRelay(domain string, auth Authorizer) *Relay {
	return &Relay{
		Domain:    strings.Trim(strings.ToLower(domain), "."),
		Authorize: auth,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  32 * 1024,
			WriteBufferSize: 32 * 1024,
			// Agents are CLIs, not browsers: no Origin to check.
			CheckOrigin: func(*http.Request) bool { return true },
		},
		tunnels: map[string]*tunnelSession{},
		byUser:  map[uint]int{},
	}
}

// Active returns the number of open tunnels.
func (r *Relay) Active() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.tunnels)
}

func (r *Relay) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	host := hostOnly(req.Host)
	if name, ok := r.subdomainOf(host); ok {
		r.serveVisitor(w, req, name)
		return
	}
	switch req.URL.Path {
	case ConnectPath:
		r.connect(w, req)
	case "/healthz":
		fmt.Fprintf(w, "ok %d\n", r.Active())
	default:
		http.Error(w, "nimbus tunnel relay", http.StatusNotFound)
	}
}

// subdomainOf returns the tunnel label when host is "<label>.<Domain>".
func (r *Relay) subdomainOf(host string) (string, bool) {
	suffix := "." + r.Domain
	if !strings.HasSuffix(host, suffix) {
		return "", false
	}
	label := strings.TrimSuffix(host, suffix)
	if label == "" || strings.Contains(label, ".") {
		return "", false
	}
	return label, true
}

func (r *Relay) serveVisitor(w http.ResponseWriter, req *http.Request, name string) {
	r.mu.Lock()
	t := r.tunnels[name]
	r.mu.Unlock()
	if t == nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, offlinePage, name+"."+r.Domain)
		return
	}
	w.Header().Set(HeaderTunnelID, t.id)
	t.proxy.ServeHTTP(w, req)
}

// connect authenticates an agent, reserves its name and hands the socket to
// yamux. Errors before the upgrade are plain JSON so the CLI can show them.
func (r *Relay) connect(w http.ResponseWriter, req *http.Request) {
	token := bearerToken(req)
	if token == "" {
		writeJSONError(w, http.StatusUnauthorized, "missing token: run 'nimbus login'")
		return
	}
	grant, err := r.Authorize(req.Context(), token)
	if err != nil {
		if errors.Is(err, ErrUnauthorized) {
			writeJSONError(w, http.StatusUnauthorized, err.Error())
		} else {
			writeJSONError(w, http.StatusBadGateway, "could not verify the token with Nimbus Cloud: "+err.Error())
		}
		return
	}

	name, status, err := r.reserve(grant, strings.ToLower(strings.TrimSpace(req.Header.Get(HeaderSubdomain))))
	if err != nil {
		writeJSONError(w, status, err.Error())
		return
	}

	var expires time.Time
	if grant.SessionTTLSeconds > 0 {
		expires = time.Now().Add(time.Duration(grant.SessionTTLSeconds) * time.Second)
	}
	hdr := http.Header{}
	hdr.Set(HeaderTunnelURL, "https://"+name+"."+r.Domain)
	if !expires.IsZero() {
		hdr.Set(HeaderTunnelExpires, expires.UTC().Format(time.RFC3339))
	}
	ws, err := r.upgrader.Upgrade(w, req, hdr)
	if err != nil {
		r.release(name, grant.UserID)
		return // Upgrade already wrote the error
	}

	// The relay opens streams (one per proxied connection), so it is the
	// yamux client; the agent accepts them as a listener.
	sess, err := yamux.Client(newWSConn(ws), yamuxConfig())
	if err != nil {
		r.release(name, grant.UserID)
		_ = ws.Close()
		return
	}

	t := &tunnelSession{
		id:      RandomSubdomain(),
		name:    name,
		userID:  grant.UserID,
		sess:    sess,
		expires: expires,
	}
	t.proxy = r.newProxy(t)
	r.mu.Lock()
	r.tunnels[name] = t
	r.mu.Unlock()
	r.logf("tunnel open  %s.%s user=%d plan=%s", name, r.Domain, grant.UserID, grant.Plan)

	var timer *time.Timer
	if !expires.IsZero() {
		timer = time.AfterFunc(time.Until(expires), func() { _ = sess.Close() })
	}
	<-sess.CloseChan()
	if timer != nil {
		timer.Stop()
	}
	r.mu.Lock()
	if r.tunnels[name] == t {
		delete(r.tunnels, name)
		r.byUser[t.userID]--
		if r.byUser[t.userID] <= 0 {
			delete(r.byUser, t.userID)
		}
	}
	r.mu.Unlock()
	_ = ws.Close()
	r.logf("tunnel close %s.%s user=%d", name, r.Domain, grant.UserID)
}

// reserve picks (or validates) the label and counts it against the account.
func (r *Relay) reserve(g Grant, want string) (name string, status int, err error) {
	if want != "" {
		if !g.CustomSubdomain {
			return "", http.StatusForbidden, errors.New("custom subdomains need a Pro plan; drop --subdomain for a random one")
		}
		if !ValidSubdomain(want) {
			return "", http.StatusBadRequest, errors.New("subdomain must be 1-40 lowercase letters, digits or hyphens")
		}
	}
	maxTunnels := g.MaxTunnels
	if maxTunnels <= 0 {
		maxTunnels = 1
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byUser[g.UserID] >= maxTunnels {
		return "", http.StatusForbidden, fmt.Errorf("this account already has %d open tunnel(s); close one or upgrade", r.byUser[g.UserID])
	}
	if want != "" {
		if _, taken := r.tunnels[want]; taken {
			return "", http.StatusConflict, fmt.Errorf("%s.%s is already in use", want, r.Domain)
		}
		name = want
	} else {
		for {
			name = RandomSubdomain()
			if _, taken := r.tunnels[name]; !taken {
				break
			}
		}
	}
	// Hold the slot with a placeholder until the upgrade completes.
	r.tunnels[name] = &tunnelSession{name: name, userID: g.UserID, proxy: offlineProxy}
	r.byUser[g.UserID]++
	return name, 0, nil
}

func (r *Relay) release(name string, userID uint) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tunnels, name)
	r.byUser[userID]--
	if r.byUser[userID] <= 0 {
		delete(r.byUser, userID)
	}
}

// newProxy builds the visitor-facing reverse proxy for one tunnel. Every
// outbound connection is a fresh yamux stream; the agent sees the public
// Host and the usual X-Forwarded-* headers (chained through whatever edge
// proxy sits in front of the relay).
func (r *Relay) newProxy(t *tunnelSession) *httputil.ReverseProxy {
	transport := &http.Transport{
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			return t.sess.Open()
		},
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
	}
	return &httputil.ReverseProxy{
		Transport:     transport,
		FlushInterval: -1, // stream SSE and chunked bodies as they arrive
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = "http"
			pr.Out.URL.Host = "tunnel" // ignored by DialContext
			pr.Out.Host = pr.In.Host
			pr.SetXForwarded()
			if v := pr.In.Header.Get("X-Forwarded-Proto"); v != "" {
				pr.Out.Header.Set("X-Forwarded-Proto", v)
			} else if pr.In.TLS == nil {
				// The relay sits behind a TLS-terminating edge in production.
				pr.Out.Header.Set("X-Forwarded-Proto", "https")
			}
			if prior := pr.In.Header.Get("X-Forwarded-For"); prior != "" {
				pr.Out.Header.Set("X-Forwarded-For", prior+", "+clientIP(pr.In))
			}
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusBadGateway)
			fmt.Fprintf(w, "The tunnel agent did not answer: %v\n", err)
		},
	}
}

// offlineProxy answers for a slot that is reserved but not yet connected.
var offlineProxy = &httputil.ReverseProxy{
	Rewrite: func(*httputil.ProxyRequest) {},
	Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("tunnel is still connecting")
	}),
	ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
	},
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func (r *Relay) logf(format string, args ...any) {
	if r.Logf != nil {
		r.Logf(format, args...)
	}
}

func hostOnly(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return strings.ToLower(h)
	}
	return strings.ToLower(hostport)
}

func clientIP(req *http.Request) string {
	if h, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
		return h
	}
	return req.RemoteAddr
}

func bearerToken(req *http.Request) string {
	h := req.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	return ""
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

const offlinePage = `<!doctype html><meta charset="utf-8"><title>Tunnel offline</title>
<style>body{font:15px/1.5 system-ui,sans-serif;color:#1f2937;background:#f8fafc;display:grid;place-items:center;min-height:100vh;margin:0}
main{max-width:28rem;padding:2rem;background:#fff;border:1px solid #e5e7eb;border-radius:12px}code{background:#f1f5f9;padding:.1em .4em;border-radius:4px}</style>
<main><h1 style="margin:0 0 .5rem;font-size:1.25rem">Tunnel offline</h1>
<p><code>%s</code> is not connected right now. The developer's <code>nimbus expose</code> session has ended or is reconnecting.</p></main>`
