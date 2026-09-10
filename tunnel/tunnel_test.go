package tunnel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func testAuthorizer(_ context.Context, token string) (Grant, error) {
	switch token {
	case "free":
		return Grant{UserID: 1, Plan: "free", MaxTunnels: 1, SessionTTLSeconds: 3600}, nil
	case "pro":
		return Grant{UserID: 2, Plan: "pro", MaxTunnels: 3, CustomSubdomain: true}, nil
	}
	return Grant{}, ErrUnauthorized
}

func startRelay(t *testing.T) (*Relay, *httptest.Server) {
	t.Helper()
	relay := NewRelay("tunnel.test", testAuthorizer)
	srv := httptest.NewServer(relay)
	t.Cleanup(srv.Close)
	return relay, srv
}

func startBackend(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fail" {
			w.WriteHeader(http.StatusTeapot)
		}
		fmt.Fprintf(w, "path=%s host=%s proto=%s for=%s", r.URL.RequestURI(), r.Host, r.Header.Get("X-Forwarded-Proto"), r.Header.Get("X-Forwarded-For"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func connect(t *testing.T, relay *httptest.Server, backend *httptest.Server, token, sub string, keepHost bool) (*Session, *Agent) {
	t.Helper()
	relayURL, err := NormalizeRelayURL(relay.URL)
	if err != nil {
		t.Fatal(err)
	}
	agent := &Agent{RelayURL: relayURL, Token: token, LocalAddr: strings.TrimPrefix(backend.URL, "http://"), Subdomain: sub, KeepHost: keepHost}
	sess, err := agent.Connect(context.Background())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess, agent
}

// visit sends a request to the relay as if it arrived for the tunnel host.
func visit(t *testing.T, relay *httptest.Server, publicURL, path string) (*http.Response, string) {
	t.Helper()
	u, err := url.Parse(publicURL)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, relay.URL+path, nil)
	req.Host = u.Host
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, string(body)
}

func TestTunnelProxiesRequests(t *testing.T) {
	_, relaySrv := startRelay(t)
	backend := startBackend(t)
	var logged []RequestLog
	sess, agent := connect(t, relaySrv, backend, "free", "", false)
	agent.OnRequest = func(r RequestLog) { logged = append(logged, r) }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = sess.Serve(ctx) }()

	if !strings.HasSuffix(sess.URL, ".tunnel.test") || !strings.HasPrefix(sess.URL, "https://") {
		t.Fatalf("unexpected public URL %q", sess.URL)
	}
	if sess.Expires.IsZero() {
		t.Fatal("free plan session should carry an expiry")
	}

	resp, body := visit(t, relaySrv, sess.URL, "/hello?x=1")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d body %q", resp.StatusCode, body)
	}
	if !strings.Contains(body, "path=/hello?x=1") || !strings.Contains(body, "proto=https") {
		t.Fatalf("backend saw %q", body)
	}
	// Default rewrites Host to the local address; the tunnel id is stamped.
	if strings.Contains(body, "host="+strings.TrimPrefix(sess.URL, "https://")) {
		t.Fatalf("Host should be rewritten to the local address by default: %q", body)
	}
	if resp.Header.Get(HeaderTunnelID) == "" {
		t.Fatal("missing tunnel id header")
	}

	resp, _ = visit(t, relaySrv, sess.URL, "/fail")
	if resp.StatusCode != http.StatusTeapot {
		t.Fatalf("status passthrough: got %d", resp.StatusCode)
	}
	if len(logged) != 2 || logged[1].Status != http.StatusTeapot || logged[1].Path != "/fail" {
		t.Fatalf("request log: %+v", logged)
	}
}

func TestKeepHost(t *testing.T) {
	_, relaySrv := startRelay(t)
	backend := startBackend(t)
	sess, _ := connect(t, relaySrv, backend, "pro", "myapp", true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = sess.Serve(ctx) }()
	if sess.URL != "https://myapp.tunnel.test" {
		t.Fatalf("custom subdomain not honoured: %q", sess.URL)
	}
	_, body := visit(t, relaySrv, sess.URL, "/")
	if !strings.Contains(body, "host=myapp.tunnel.test") {
		t.Fatalf("expected public host forwarded, got %q", body)
	}
}

func TestRejections(t *testing.T) {
	_, relaySrv := startRelay(t)
	backend := startBackend(t)
	relayURL, _ := NormalizeRelayURL(relaySrv.URL)
	local := strings.TrimPrefix(backend.URL, "http://")

	try := func(token, sub string) *ConnectError {
		t.Helper()
		a := &Agent{RelayURL: relayURL, Token: token, LocalAddr: local, Subdomain: sub}
		s, err := a.Connect(context.Background())
		if err == nil {
			_ = s.Close()
			t.Fatalf("expected rejection for token=%s sub=%s", token, sub)
		}
		var ce *ConnectError
		if !errors.As(err, &ce) {
			t.Fatalf("expected ConnectError, got %T %v", err, err)
		}
		return ce
	}
	if ce := try("nope", ""); ce.Status != http.StatusUnauthorized || !ce.Permanent() {
		t.Fatalf("bad token: %+v", ce)
	}
	if ce := try("free", "wanted"); ce.Status != http.StatusForbidden {
		t.Fatalf("free plan custom name: %+v", ce)
	}
	if ce := try("pro", "Bad_Name!"); ce.Status != http.StatusBadRequest {
		t.Fatalf("invalid name: %+v", ce)
	}

	// Name clash and per-account limit.
	first, _ := connect(t, relaySrv, backend, "pro", "taken", false)
	if ce := try("pro", "taken"); ce.Status != http.StatusConflict {
		t.Fatalf("clash: %+v", ce)
	}
	_ = first
	connect(t, relaySrv, backend, "free", "", false)
	if ce := try("free", ""); ce.Status != http.StatusForbidden {
		t.Fatalf("limit: %+v", ce)
	}
}

func TestOfflineAndUnknownHosts(t *testing.T) {
	relay, relaySrv := startRelay(t)
	backend := startBackend(t)
	sess, _ := connect(t, relaySrv, backend, "free", "", false)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = sess.Serve(ctx) }()

	resp, _ := visit(t, relaySrv, "https://ghost.tunnel.test", "/")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown tunnel: %d", resp.StatusCode)
	}
	resp, _ = visit(t, relaySrv, "https://deep.ghost.tunnel.test", "/")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("nested label must not route: %d", resp.StatusCode)
	}

	cancel()
	deadline := time.Now().Add(3 * time.Second)
	for relay.Active() != 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if relay.Active() != 0 {
		t.Fatal("tunnel not released after the agent left")
	}
	resp, body := visit(t, relaySrv, sess.URL, "/")
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(body, "Tunnel offline") {
		t.Fatalf("closed tunnel: %d %q", resp.StatusCode, body)
	}
}

func TestNormalizeRelayURL(t *testing.T) {
	cases := map[string]string{
		"tunnel.nimbusgo.live":         "wss://tunnel.nimbusgo.live/connect",
		"https://tunnel.nimbusgo.live": "wss://tunnel.nimbusgo.live/connect",
		"http://localhost:8090/":       "ws://localhost:8090/connect",
		"wss://relay.example/custom":   "wss://relay.example/custom",
	}
	for in, want := range cases {
		got, err := NormalizeRelayURL(in)
		if err != nil || got != want {
			t.Errorf("NormalizeRelayURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := NormalizeRelayURL("ftp://x"); err == nil {
		t.Error("ftp must be rejected")
	}
}
