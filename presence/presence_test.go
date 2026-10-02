package presence

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/CodeSyncr/nimbus/redis"
	"github.com/alicebob/miniredis/v2"
	"github.com/gorilla/websocket"
)

func serve(t *testing.T, h *Hub) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(h.HandleWebSocket))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

type wsClient struct {
	t      *testing.T
	conn   *websocket.Conn
	events chan Event
}

func dial(t *testing.T, base, channel, user string) *wsClient {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(base+"?channel="+channel+"&user_id="+user, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	c := &wsClient{t: t, conn: conn, events: make(chan Event, 64)}
	go func() {
		defer close(c.events)
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var e Event
			if json.Unmarshal(data, &e) == nil {
				c.events <- e
			}
		}
	}()
	t.Cleanup(func() { _ = conn.Close() })
	return c
}

// expect waits for an event of the given type, skipping others.
func (c *wsClient) expect(typ string) Event {
	c.t.Helper()
	timeout := time.After(3 * time.Second)
	for {
		select {
		case e, ok := <-c.events:
			if !ok {
				c.t.Fatalf("connection closed while waiting for %s", typ)
			}
			if e.Type == typ {
				return e
			}
		case <-timeout:
			c.t.Fatalf("timed out waiting for %s", typ)
		}
	}
}

// expectNone fails if an event of the given type arrives within d.
func (c *wsClient) expectNone(typ string, d time.Duration) {
	c.t.Helper()
	timeout := time.After(d)
	for {
		select {
		case e, ok := <-c.events:
			if !ok {
				return
			}
			if e.Type == typ {
				c.t.Fatalf("unexpected %s event: %+v", typ, e)
			}
		case <-timeout:
			return
		}
	}
}

func (c *wsClient) send(typ string, data any) {
	c.t.Helper()
	if err := c.conn.WriteJSON(map[string]any{"type": typ, "data": data}); err != nil {
		c.t.Fatal(err)
	}
}

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSecondTabDoesNotKickFirstAndLeaveWaitsForLastTab(t *testing.T) {
	h := NewHub(Config{})
	base := serve(t, h)

	watcher := dial(t, base, "room", "bob")
	watcher.expect("presence:state")

	tab1 := dial(t, base, "room", "alice")
	tab1.expect("presence:state")
	watcher.expect("presence:join")

	tab2 := dial(t, base, "room", "alice")
	tab2.expect("presence:state")
	watcher.expectNone("presence:join", 200*time.Millisecond)

	// The first tab is still connected and receives broadcasts.
	watcher.send("message", "hi")
	tab1.expect("message")
	tab2.expect("message")

	if n := h.UserCount("room"); n != 2 {
		t.Fatalf("UserCount = %d, want 2 distinct users", n)
	}

	_ = tab1.conn.Close()
	watcher.expectNone("presence:leave", 300*time.Millisecond)
	if n := h.UserCount("room"); n != 2 {
		t.Fatalf("alice should still be present with one tab open, count=%d", n)
	}

	_ = tab2.conn.Close()
	e := watcher.expect("presence:leave")
	if e.User == nil || e.User.ID != "alice" {
		t.Fatalf("leave for %+v, want alice", e.User)
	}
}

func TestPresenceAcrossInstancesWithRedis(t *testing.T) {
	mr := miniredis.RunT(t)
	newClient := func() *redis.Client {
		c := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		t.Cleanup(func() { _ = c.Close() })
		return c
	}
	h1 := NewHub(Config{Redis: newClient(), PingInterval: 50 * time.Millisecond})
	h2 := NewHub(Config{Redis: newClient(), PingInterval: 50 * time.Millisecond})
	t.Cleanup(func() { _ = h1.Close() })
	base1, base2 := serve(t, h1), serve(t, h2)

	alice := dial(t, base1, "room", "alice")
	alice.expect("presence:state")

	bob := dial(t, base2, "room", "bob")
	state := bob.expect("presence:state")
	if len(state.Users) != 2 {
		t.Fatalf("bob's state on instance 2 = %+v, want alice and bob", state.Users)
	}
	alice.expect("presence:join")

	for _, h := range []*Hub{h1, h2} {
		if users := h.UsersIn("room"); len(users) != 2 {
			t.Fatalf("UsersIn = %+v, want 2 users", users)
		}
	}
	if got := h1.Channels(); len(got) != 1 || got[0] != "room" {
		t.Fatalf("Channels = %v", got)
	}

	alice.send("message", "hello from 1")
	if e := bob.expect("message"); e.Data != "hello from 1" {
		t.Fatalf("bob got %+v", e)
	}
	bob.send("whisper", map[string]any{"to": "alice", "message": "psst"})
	if e := alice.expect("whisper"); e.Data != "psst" {
		t.Fatalf("alice got %+v", e)
	}

	// Instance 2 dies without saying goodbye: bob's lease runs out and
	// instance 1 announces that he left.
	_ = h2.Close()
	e := alice.expect("presence:leave")
	if e.User == nil || e.User.ID != "bob" {
		t.Fatalf("leave for %+v, want bob", e.User)
	}
	waitUntil(t, func() bool { return h1.UserCount("room") == 1 })
}
