package cache

import (
	"testing"
	"time"
)

// ── MemoryStore: Set + Get ──────────────────────────────────────

func TestMemoryStore_SetGet(t *testing.T) {
	s := NewMemoryStore()
	_ = s.Set("key1", "value1", time.Minute)

	v, ok := s.Get("key1")
	if !ok {
		t.Fatal("expected key1 to exist")
	}
	if v.(string) != "value1" {
		t.Errorf("expected 'value1', got %v", v)
	}
}

func TestMemoryStore_GetMissing(t *testing.T) {
	s := NewMemoryStore()
	_, ok := s.Get("missing")
	if ok {
		t.Error("expected missing key to return false")
	}
}

// ── TTL Expiration ──────────────────────────────────────────────

func TestMemoryStore_TTLExpiration(t *testing.T) {
	s := NewMemoryStore()
	_ = s.Set("short", "gone", 50*time.Millisecond)

	// Should exist immediately
	_, ok := s.Get("short")
	if !ok {
		t.Fatal("expected key to exist immediately")
	}

	// Wait for expiration
	time.Sleep(100 * time.Millisecond)

	_, ok = s.Get("short")
	if ok {
		t.Error("expected key to be expired")
	}
}

func TestMemoryStore_ZeroTTL_NoExpiry(t *testing.T) {
	s := NewMemoryStore()
	_ = s.Set("forever", "persistent", 0)

	time.Sleep(50 * time.Millisecond)
	v, ok := s.Get("forever")
	if !ok {
		t.Fatal("zero TTL should not expire")
	}
	if v.(string) != "persistent" {
		t.Errorf("expected 'persistent', got %v", v)
	}
}

// ── Delete ──────────────────────────────────────────────────────

func TestMemoryStore_Delete(t *testing.T) {
	s := NewMemoryStore()
	_ = s.Set("del", "value", time.Minute)
	_ = s.Delete("del")

	_, ok := s.Get("del")
	if ok {
		t.Error("expected key to be deleted")
	}
}

func TestMemoryStore_DeleteNonexistent(t *testing.T) {
	s := NewMemoryStore()
	err := s.Delete("nope")
	if err != nil {
		t.Errorf("deleting nonexistent key should not error: %v", err)
	}
}

// ── Remember ────────────────────────────────────────────────────

func TestMemoryStore_Remember_CacheHit(t *testing.T) {
	s := NewMemoryStore()
	_ = s.Set("cached", "existing", time.Minute)

	callCount := 0
	v, err := s.Remember("cached", time.Minute, func() (any, error) {
		callCount++
		return "computed", nil
	})
	if err != nil {
		t.Fatalf("Remember error: %v", err)
	}
	if v.(string) != "existing" {
		t.Errorf("expected cached 'existing', got %v", v)
	}
	if callCount != 0 {
		t.Error("fn should not be called on cache hit")
	}
}

func TestMemoryStore_Remember_CacheMiss(t *testing.T) {
	s := NewMemoryStore()

	callCount := 0
	v, err := s.Remember("new", time.Minute, func() (any, error) {
		callCount++
		return "computed", nil
	})
	if err != nil {
		t.Fatalf("Remember error: %v", err)
	}
	if v.(string) != "computed" {
		t.Errorf("expected 'computed', got %v", v)
	}
	if callCount != 1 {
		t.Errorf("expected fn called once, got %d", callCount)
	}

	// Second call should hit cache
	v2, _ := s.Remember("new", time.Minute, func() (any, error) {
		callCount++
		return "recomputed", nil
	})
	if v2.(string) != "computed" {
		t.Errorf("expected cached 'computed', got %v", v2)
	}
	if callCount != 1 {
		t.Error("fn should not be called again on cache hit")
	}
}

// ── InvalidatePrefix ────────────────────────────────────────────

func TestMemoryStore_InvalidatePrefix(t *testing.T) {
	s := NewMemoryStore()
	_ = s.Set("products:1", "p1", time.Minute)
	_ = s.Set("products:2", "p2", time.Minute)
	_ = s.Set("users:1", "u1", time.Minute)

	_ = s.InvalidatePrefix("products:")

	if _, ok := s.Get("products:1"); ok {
		t.Error("products:1 should be invalidated")
	}
	if _, ok := s.Get("products:2"); ok {
		t.Error("products:2 should be invalidated")
	}
	if _, ok := s.Get("users:1"); !ok {
		t.Error("users:1 should still exist")
	}
}

// ── Global Functions ────────────────────────────────────────────

func TestGlobal_SetGetHas(t *testing.T) {
	// Reset to ensure clean state
	Default = NewMemoryStore()

	_ = Set("gkey", "gval", time.Minute)

	v, ok := Get("gkey")
	if !ok {
		t.Fatal("expected global key to exist")
	}
	if v.(string) != "gval" {
		t.Errorf("expected 'gval', got %v", v)
	}
	if !Has("gkey") {
		t.Error("Has should return true")
	}
	if !Missing("nonexistent") {
		t.Error("Missing should return true for nonexistent key")
	}
}

func TestGlobal_Pull(t *testing.T) {
	Default = NewMemoryStore()
	_ = Set("pullme", "data", time.Minute)

	v, ok := Pull("pullme")
	if !ok || v.(string) != "data" {
		t.Errorf("Pull should return the value; got %v, %v", v, ok)
	}
	if Has("pullme") {
		t.Error("Pull should delete the key after retrieval")
	}
}

func TestGlobal_SetForever(t *testing.T) {
	Default = NewMemoryStore()
	_ = SetForever("permanent", "here")

	time.Sleep(50 * time.Millisecond)
	if !Has("permanent") {
		t.Error("SetForever key should not expire")
	}
}

func TestGlobal_Remember(t *testing.T) {
	Default = NewMemoryStore()
	v, err := Remember("rkey", time.Minute, func() (any, error) {
		return 42, nil
	})
	if err != nil {
		t.Fatalf("Remember error: %v", err)
	}
	if v.(int) != 42 {
		t.Errorf("expected 42, got %v", v)
	}
}

func TestGlobal_RememberT(t *testing.T) {
	Default = NewMemoryStore()
	type User struct {
		Name string
	}
	v, err := RememberT[User]("user:1", time.Minute, func() (User, error) {
		return User{Name: "Alice"}, nil
	})
	if err != nil {
		t.Fatalf("RememberT error: %v", err)
	}
	if v.Name != "Alice" {
		t.Errorf("expected Alice, got %s", v.Name)
	}
}

// ── Overwrite ───────────────────────────────────────────────────

func TestMemoryStore_Overwrite(t *testing.T) {
	s := NewMemoryStore()
	_ = s.Set("key", "v1", time.Minute)
	_ = s.Set("key", "v2", time.Minute)

	v, _ := s.Get("key")
	if v.(string) != "v2" {
		t.Errorf("expected 'v2' after overwrite, got %v", v)
	}
}

// ── Complex Values ──────────────────────────────────────────────

func TestMemoryStore_ComplexValues(t *testing.T) {
	s := NewMemoryStore()
	data := map[string]int{"a": 1, "b": 2}
	_ = s.Set("map", data, time.Minute)

	v, ok := s.Get("map")
	if !ok {
		t.Fatal("expected key to exist")
	}
	m := v.(map[string]int)
	if m["a"] != 1 || m["b"] != 2 {
		t.Errorf("unexpected map values: %v", m)
	}
}
