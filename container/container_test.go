package container

import (
	"errors"
	"sync"
	"testing"
)

// ── Bind + Make ─────────────────────────────────────────────────

func TestBind_Make_Transient(t *testing.T) {
	c := New()
	callCount := 0
	c.Bind("counter", func() int {
		callCount++
		return callCount
	})

	v1, err := c.Make("counter")
	if err != nil {
		t.Fatalf("Make failed: %v", err)
	}
	v2, err := c.Make("counter")
	if err != nil {
		t.Fatalf("Make failed: %v", err)
	}

	if v1.(int) == v2.(int) {
		t.Errorf("Bind should produce new instances; got %v == %v", v1, v2)
	}
	if callCount != 2 {
		t.Errorf("expected constructor called 2 times, got %d", callCount)
	}
}

func TestMake_MissingBinding(t *testing.T) {
	c := New()
	_, err := c.Make("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing binding, got nil")
	}
}

// ── Singleton ───────────────────────────────────────────────────

func TestSingleton_ReturnsSameInstance(t *testing.T) {
	c := New()
	callCount := 0
	c.Singleton("service", func() *testService {
		callCount++
		return &testService{Name: "svc"}
	})

	v1, err := c.Make("service")
	if err != nil {
		t.Fatalf("Make failed: %v", err)
	}
	v2, err := c.Make("service")
	if err != nil {
		t.Fatalf("Make failed: %v", err)
	}

	if v1 != v2 {
		t.Errorf("Singleton should return same instance; got %p != %p", v1, v2)
	}
	if callCount != 1 {
		t.Errorf("expected constructor called once, got %d", callCount)
	}
}

func TestSingleton_ConcurrentAccess(t *testing.T) {
	c := New()
	callCount := 0
	var mu sync.Mutex
	c.Singleton("concurrent", func() int {
		mu.Lock()
		callCount++
		mu.Unlock()
		return 42
	})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := c.Make("concurrent")
			if err != nil {
				t.Errorf("Make failed: %v", err)
				return
			}
			if v.(int) != 42 {
				t.Errorf("expected 42, got %v", v)
			}
		}()
	}
	wg.Wait()

	if callCount != 1 {
		t.Errorf("expected singleton constructor called once, got %d", callCount)
	}
}

// ── Instance ────────────────────────────────────────────────────

func TestInstance_ReturnsExactValue(t *testing.T) {
	c := New()
	svc := &testService{Name: "pre-built"}
	c.Instance("pre", svc)

	v, err := c.Make("pre")
	if err != nil {
		t.Fatalf("Make failed: %v", err)
	}
	if v != svc {
		t.Errorf("Instance should return exact value; got %p != %p", v, svc)
	}
}

// ── Has ─────────────────────────────────────────────────────────

func TestHas(t *testing.T) {
	c := New()
	if c.Has("missing") {
		t.Error("expected Has to return false for missing binding")
	}
	c.Bind("exists", func() string { return "yes" })
	if !c.Has("exists") {
		t.Error("expected Has to return true for existing binding")
	}
}

// ── MustMake ────────────────────────────────────────────────────

func TestMustMake_PanicsOnMissing(t *testing.T) {
	c := New()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected MustMake to panic for missing binding")
		}
	}()
	c.MustMake("nothing")
}

func TestMustMake_ReturnsValue(t *testing.T) {
	c := New()
	c.Bind("val", func() string { return "hello" })
	v := c.MustMake("val")
	if v.(string) != "hello" {
		t.Errorf("expected 'hello', got %v", v)
	}
}

// ── Constructor Errors ──────────────────────────────────────────

func TestMake_ConstructorReturnsError(t *testing.T) {
	c := New()
	c.Bind("fail", func() (string, error) {
		return "", errors.New("construction failed")
	})

	_, err := c.Make("fail")
	if err == nil {
		t.Fatal("expected error from failing constructor")
	}
	if err.Error() != "construction failed" {
		t.Errorf("unexpected error: %v", err)
	}
}

// ── Auto-Wiring ─────────────────────────────────────────────────

func TestAutoWire_ResolvesParameterByType(t *testing.T) {
	c := New()
	c.Singleton("db", func() *testDB {
		return &testDB{DSN: "postgres://localhost"}
	})
	c.Bind("repo", func(db *testDB) *testRepo {
		return &testRepo{DB: db}
	})

	v, err := c.Make("repo")
	if err != nil {
		t.Fatalf("auto-wire Make failed: %v", err)
	}
	repo := v.(*testRepo)
	if repo.DB == nil {
		t.Fatal("expected DB to be auto-wired")
	}
	if repo.DB.DSN != "postgres://localhost" {
		t.Errorf("expected DSN 'postgres://localhost', got %q", repo.DB.DSN)
	}
}

// ── Bind Overrides Singleton ────────────────────────────────────

func TestBind_OverridesSingleton(t *testing.T) {
	c := New()
	c.Singleton("val", func() string { return "first" })
	v1, _ := c.Make("val")
	if v1.(string) != "first" {
		t.Fatalf("expected 'first', got %v", v1)
	}

	// Override with Bind (transient)
	c.Bind("val", func() string { return "second" })
	v2, _ := c.Make("val")
	if v2.(string) != "second" {
		t.Errorf("expected 'second' after override, got %v", v2)
	}
}

// ── test helpers ────────────────────────────────────────────────

type testService struct {
	Name string
}

type testDB struct {
	DSN string
}

type testRepo struct {
	DB *testDB
}
