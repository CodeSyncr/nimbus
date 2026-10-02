package websocket

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/CodeSyncr/nimbus/redis"
	"github.com/alicebob/miniredis/v2"
	gws "github.com/gorilla/websocket"
)

func startHub(t *testing.T, h *Hub) string {
	t.Helper()
	go h.Run()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = h.Upgrade(w, r)
	}))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

func dial(t *testing.T, url string, header http.Header) *gws.Conn {
	t.Helper()
	c, _, err := gws.DefaultDialer.Dial(url, header)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func read(t *testing.T, c *gws.Conn) string {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, msg, err := c.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(msg)
}

func waitClients(t *testing.T, h *Hub, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for h.Len() != n {
		if time.Now().After(deadline) {
			t.Fatalf("hub has %d clients, want %d", h.Len(), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestBroadcastLocal(t *testing.T) {
	h := NewHub()
	url := startHub(t, h)
	a, b := dial(t, url, nil), dial(t, url, nil)
	waitClients(t, h, 2)
	h.Broadcast([]byte("hello"))
	if got := read(t, a); got != "hello" {
		t.Fatalf("a got %q", got)
	}
	if got := read(t, b); got != "hello" {
		t.Fatalf("b got %q", got)
	}
}

func TestBroadcastAcrossInstancesWithRedis(t *testing.T) {
	mr := miniredis.RunT(t)
	hubs := []*Hub{NewHub(), NewHub()}
	var urls []string
	for _, h := range hubs {
		c := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		t.Cleanup(func() { _ = c.Close() })
		if err := h.UseRedis(c, ""); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = h.Close() })
		urls = append(urls, startHub(t, h))
	}
	a, b := dial(t, urls[0], nil), dial(t, urls[1], nil)
	waitClients(t, hubs[0], 1)
	waitClients(t, hubs[1], 1)

	hubs[0].Broadcast([]byte("from instance 1"))
	if got := read(t, a); got != "from instance 1" {
		t.Fatalf("local client got %q", got)
	}
	if got := read(t, b); got != "from instance 1" {
		t.Fatalf("remote client got %q", got)
	}
}

func TestOriginCheck(t *testing.T) {
	h := NewHub()
	url := startHub(t, h)
	if _, _, err := gws.DefaultDialer.Dial(url, http.Header{"Origin": {"https://evil.example"}}); err == nil {
		t.Fatal("cross-origin upgrade should be rejected by default")
	}
	h.SetAllowedOrigins([]string{"https://app.example"})
	dial(t, url, http.Header{"Origin": {"https://app.example"}})
}
