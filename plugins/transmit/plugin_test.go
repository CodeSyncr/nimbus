package transmit

import "testing"

func TestNewPlugin(t *testing.T) {
	p := New(nil)
	if p == nil {
		t.Fatal("expected plugin")
	}
	if p.Name() != "transmit" {
		t.Fatalf("unexpected name: %s", p.Name())
	}
}
