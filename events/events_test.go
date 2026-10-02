package events

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDispatchRunsListenersInOrderAndReturnsFirstError(t *testing.T) {
	d := New()
	var order []int
	d.Listen("e", func(any) error { order = append(order, 1); return nil })
	d.Listen("e", func(any) error { order = append(order, 2); return errors.New("first") })
	d.Listen("e", func(any) error { order = append(order, 3); return errors.New("second") })

	err := d.Dispatch("e", nil)
	if err == nil || err.Error() != "first" {
		t.Fatalf("Dispatch error = %v, want first", err)
	}
	if len(order) != 3 || order[0] != 1 || order[2] != 3 {
		t.Fatalf("listeners ran %v, want all three in order", order)
	}
}

func TestPayloadReachesListener(t *testing.T) {
	d := New()
	var got any
	d.Listen("user.created", func(p any) error { got = p; return nil })
	_ = d.Dispatch("user.created", 42)
	if got != 42 {
		t.Fatalf("payload = %v", got)
	}
	if err := d.Dispatch("nobody.listens", nil); err != nil {
		t.Fatalf("event without listeners: %v", err)
	}
}

func TestDispatchAsync(t *testing.T) {
	d := New()
	var wg sync.WaitGroup
	var n int32
	wg.Add(2)
	for i := 0; i < 2; i++ {
		d.Listen("e", func(any) error { atomic.AddInt32(&n, 1); wg.Done(); return errors.New("logged") })
	}
	d.DispatchAsync("e", nil)
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("async listeners did not run")
	}
}

func TestHasCountClear(t *testing.T) {
	d := New()
	d.Listen("a", func(any) error { return nil })
	d.Listen("a", func(any) error { return nil })
	d.Listen("b", func(any) error { return nil })
	if !d.Has("a") || d.ListenerCount("a") != 2 || d.Has("c") {
		t.Fatal("Has/ListenerCount wrong")
	}
	d.Clear("a")
	if d.Has("a") || !d.Has("b") {
		t.Fatal("Clear(a) should only clear a")
	}
	d.Clear()
	if d.Has("b") {
		t.Fatal("Clear() should clear everything")
	}
}

func TestAfterDispatchHook(t *testing.T) {
	var seen []string
	var mu sync.Mutex
	AfterDispatch(func(event string, _ any) {
		mu.Lock()
		seen = append(seen, event)
		mu.Unlock()
	})
	AfterDispatch(nil) // ignored
	d := New()
	_ = d.Dispatch("hooked", nil)
	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, e := range seen {
		if e == "hooked" {
			found = true
		}
	}
	if !found {
		t.Fatal("AfterDispatch hook did not see the event")
	}
}

func TestGlobalHelpers(t *testing.T) {
	t.Cleanup(func() { Default.Clear("global.test") })
	var called int32
	Listen("global.test", func(any) error { atomic.AddInt32(&called, 1); return nil })
	if err := Dispatch("global.test", nil); err != nil || called != 1 {
		t.Fatalf("global Dispatch: called=%d err=%v", called, err)
	}
}
