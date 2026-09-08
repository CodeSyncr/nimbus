package docs

import (
	"testing"

	"github.com/CodeSyncr/nimbus"
	"github.com/CodeSyncr/nimbus/packages/nimbusdocs"
)

func reset() {
	mu.Lock()
	clients = map[nimbusdocs.Product]*nimbusdocs.Client{}
	mu.Unlock()
}

// Only the products with a key become available; the rest stay nil rather
// than failing the boot, so a Sigma-only app does not need a Carbon key.
func TestRegisterBuildsAClientPerConfiguredKey(t *testing.T) {
	reset()
	t.Setenv("SIGMA_API_KEY", "sgm_live_test")
	t.Setenv("CARBON_API_KEY", "")
	t.Setenv("NIMBUS_DOCS_URL", "http://localhost:3333/")

	if err := New().Register(nimbus.New()); err != nil {
		t.Fatalf("register: %v", err)
	}
	if Sigma() == nil {
		t.Error("SIGMA_API_KEY was set but Sigma() is nil")
	}
	if Carbon() != nil {
		t.Error("no CARBON_API_KEY, but Carbon() is not nil")
	}
	if For(nimbusdocs.Sigma) != Sigma() {
		t.Error("For(Sigma) and Sigma() disagree")
	}
	if got := Sigma().Product(); got != nimbusdocs.Sigma {
		t.Errorf("product %q", got)
	}
}

func TestRegisterWithNoKeysIsNotAnError(t *testing.T) {
	reset()
	t.Setenv("SIGMA_API_KEY", "")
	t.Setenv("CARBON_API_KEY", "")
	if err := New().Register(nimbus.New()); err != nil {
		t.Fatalf("an app without document keys must still boot: %v", err)
	}
	if Sigma() != nil || Carbon() != nil {
		t.Error("clients exist without keys")
	}
}
