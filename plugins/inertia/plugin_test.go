package inertia

import "testing"

func TestNewPlugin(t *testing.T) {
	p := New(Config{})
	if p == nil {
		t.Fatal("expected plugin")
	}
	if p.Name() != "inertia" {
		t.Fatalf("unexpected name: %s", p.Name())
	}
}
