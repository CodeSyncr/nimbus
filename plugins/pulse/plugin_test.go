package pulse

import "testing"

func TestNewPulsePlugin(t *testing.T) {
	p := NewPlugin()
	if p == nil || p.Pulse == nil {
		t.Fatal("expected pulse plugin and pulse instance")
	}
	if p.Name() != "pulse" {
		t.Fatalf("unexpected name: %s", p.Name())
	}
}
