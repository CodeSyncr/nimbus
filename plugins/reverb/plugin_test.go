package reverb

import "testing"

func TestNewPlugin(t *testing.T) {
	p := New(nil)
	if p == nil {
		t.Fatal("expected plugin")
	}
	if p.Name() != "reverb" {
		t.Fatalf("unexpected name: %s", p.Name())
	}
}
