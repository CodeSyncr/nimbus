package container

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConstructorCanResolveIndependentBinding(t *testing.T) {
	c := New()
	c.Instance("inner", 42)
	c.Singleton("outer", func() any { return c.MustMake("inner") })
	done := make(chan any, 1)
	go func() { done <- c.MustMake("outer") }()
	select {
	case v := <-done:
		if v != 42 {
			t.Fatal(v)
		}
	case <-time.After(time.Second):
		t.Fatal("constructor deadlocked")
	}
}
func TestIndependentConstructorsDoNotSerialize(t *testing.T) {
	c := New()
	entered := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	c.Bind("slow", func() int { close(entered); <-release; return 1 })
	go c.Make("slow")
	<-entered
	done := make(chan struct{})
	go func() {
		c.Instance("fast", 2)
		if c.MustMake("fast") != 2 {
			t.Error("wrong instance")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("slow constructor blocked independent binding")
	}
}

type cycleA struct{}
type cycleB struct{}

func TestTypedCycleFailsWithoutDeadlock(t *testing.T) {
	c := New()
	c.Singleton("a", func(*cycleB) *cycleA { return &cycleA{} })
	c.Singleton("b", func(*cycleA) *cycleB { return &cycleB{} })
	done := make(chan error, 2)
	go func() { _, err := c.Make("a"); done <- err }()
	go func() { _, err := c.Make("b"); done <- err }()
	for i := 0; i < 2; i++ {
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("cycle accepted")
			}
		case <-time.After(time.Second):
			t.Fatal("cycle deadlocked")
		}
	}
}
func TestAutoWiredSingletonConstructedOnce(t *testing.T) {
	c := New()
	var count atomic.Int32
	c.Singleton("dep", func() *testDB { time.Sleep(10 * time.Millisecond); return &testDB{} })
	c.Singleton("repo", func(d *testDB) *testRepo { count.Add(1); return &testRepo{DB: d} })
	var wg sync.WaitGroup
	results := make(chan any, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- c.MustMake("repo") }()
	}
	wg.Wait()
	close(results)
	var first any
	for v := range results {
		if first == nil {
			first = v
		} else if v != first {
			t.Fatal("different singletons")
		}
	}
	if count.Load() != 1 {
		t.Fatal(count.Load())
	}
}
func TestRebindingDoesNotPublishOldConstruction(t *testing.T) {
	c := New()
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	c.Singleton("v", func() string { close(entered); <-release; return "old" })
	go func() { c.Make("v"); close(done) }()
	<-entered
	c.Instance("v", "new")
	close(release)
	<-done
	if c.MustMake("v") != "new" {
		t.Fatal("old construction overwrote new binding")
	}
}
