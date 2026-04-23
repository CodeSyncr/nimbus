package mcp

import "testing"

func TestNewPlugin(t *testing.T) {
	p := New()
	if p == nil {
		t.Fatal("expected plugin")
	}
	if p.Name() != "mcp" {
		t.Fatalf("unexpected name: %s", p.Name())
	}
}
