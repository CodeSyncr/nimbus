package websocket

import (
	"context"
	"errors"
	gws "github.com/gorilla/websocket"
	"strings"
	"testing"
	"time"
)

func TestCloseStopsRunAndClients(t *testing.T) {
	h := NewHub()
	url := startHub(t, h)
	c := dial(t, url, nil)
	waitClients(t, h, 1)
	done := make(chan struct{})
	go func() { h.Run(); close(done) }()
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not stop")
	}
	c.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := c.ReadMessage(); err == nil {
		t.Fatal("client remained open")
	}
	if h.Len() != 0 {
		t.Fatal("clients retained")
	}
	if err := h.BroadcastContext(context.Background(), []byte("late")); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
}
func TestCloseBeforeRunAndQueueBounds(t *testing.T) {
	h := NewHubWithOptions(HubOptions{MaxMessageBytes: 8})
	if err := h.BroadcastContext(context.Background(), []byte("too large")); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatal(err)
	}
	message := []byte("original")
	if err := h.BroadcastContext(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	message[0] = 'X'
	if got := <-h.broadcast; string(got) != "original" {
		t.Fatal("caller mutation retained")
	}
	for i := 0; i < cap(h.broadcast); i++ {
		if err := h.BroadcastContext(context.Background(), nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.BroadcastContext(context.Background(), nil); !errors.Is(err, ErrBackpressure) {
		t.Fatal(err)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	h.Run()
	if len(h.broadcast) != 0 {
		t.Fatal("closed hub retained queued messages")
	}
}
func TestOversizedInboundMessageClosesClient(t *testing.T) {
	h := NewHubWithOptions(HubOptions{MaxMessageBytes: 64})
	url := startHub(t, h)
	c := dial(t, url, nil)
	if err := c.WriteMessage(gws.TextMessage, []byte(strings.Repeat("x", 1024))); err != nil {
		t.Fatal(err)
	}
	c.SetReadDeadline(time.Now().Add(time.Second))
	_, _, err := c.ReadMessage()
	if !gws.IsCloseError(err, gws.CloseMessageTooBig) {
		t.Fatalf("oversize not rejected: %v", err)
	}
	waitClients(t, h, 0)
}
func TestConnectionLimitAndIdleExpiry(t *testing.T) {
	h := NewHubWithOptions(HubOptions{MaxConnections: 1, PongTimeout: 100 * time.Millisecond, PingInterval: 20 * time.Millisecond})
	url := startHub(t, h)
	dial(t, url, nil)
	waitClients(t, h, 1)
	second, response, err := gws.DefaultDialer.Dial(url, nil)
	if err == nil {
		second.Close()
		t.Fatal("connection cap ignored")
	}
	if response == nil || response.StatusCode != 503 {
		t.Fatalf("wrong capacity response: %v", response)
	}
	// The first client deliberately never reads, so it cannot reply to ping.
	waitClients(t, h, 0)
}
