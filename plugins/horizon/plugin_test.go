package horizon

import "testing"

func TestNewPlugin(t *testing.T) {
	p := New()
	if p == nil {
		t.Fatal("expected plugin")
	}
	if p.Name() != "horizon" {
		t.Fatalf("unexpected name: %s", p.Name())
	}
}
