package testing

import (
	"sync"
	"testing"
)

// ── Fake Ownership ──────────────────────────────────────────────

// The Fake* helpers swap package-level globals (mail.Default, the queue
// manager, ...), so two tests faking the same service at once would see each
// other's calls. claim fails the second test straight away instead.
var (
	activeMu sync.Mutex
	active   = map[string]string{} // service -> test name
)

func claim(t testing.TB, service string) {
	t.Helper()
	activeMu.Lock()
	defer activeMu.Unlock()
	if owner, ok := active[service]; ok {
		t.Fatalf("nimbustesting: %s is already faked by %s; tests that fake the same service cannot run in parallel", service, owner)
	}
	active[service] = t.Name()
	t.Cleanup(func() {
		activeMu.Lock()
		delete(active, service)
		activeMu.Unlock()
	})
}
