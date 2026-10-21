package testing

import (
	"io"
	"testing"

	"github.com/CodeSyncr/nimbus/cache"
	"github.com/CodeSyncr/nimbus/mail"
	"github.com/CodeSyncr/nimbus/storage"
)

// ── Mail ────────────────────────────────────────────────────────

// FakeMail makes mail.Send (and notifications' mail channel) capture messages
// on a fresh FakeMailer for the rest of the test, then restores mail.Default.
//
//	mails := nimbustesting.FakeMail(t)
//	client.PostJSON("/signup", body)
//	mails.AssertSent(t, func(m *mail.Message) bool { return m.Subject == "Welcome" })
func FakeMail(t testing.TB) *FakeMailer {
	t.Helper()
	claim(t, "mail")
	f := NewFakeMailer()
	prev := mail.Default
	mail.Default = f
	t.Cleanup(func() { mail.Default = prev })
	return f
}

// AssertSent fails the test unless a message was sent (and, when match is
// given, one of them matches it).
func (f *FakeMailer) AssertSent(t testing.TB, match ...func(*mail.Message) bool) {
	t.Helper()
	sent := f.Sent()
	if len(sent) == 0 {
		t.Errorf("expected a sent mail, got none")
		return
	}
	if len(match) > 0 && !anyMatch(sent, match[0]) {
		t.Errorf("%d mail(s) sent, none matched", len(sent))
	}
}

// AssertSentTo fails the test unless a message was sent to addr.
func (f *FakeMailer) AssertSentTo(t testing.TB, addr string) {
	t.Helper()
	if !f.HasSentTo(addr) {
		t.Errorf("expected a mail to %s, got none", addr)
	}
}

// AssertNothingSent fails the test if any message was sent.
func (f *FakeMailer) AssertNothingSent(t testing.TB) {
	t.Helper()
	if n := f.SentCount(); n > 0 {
		t.Errorf("expected no sent mail, got %d", n)
	}
}

// AssertSentCount fails the test unless exactly n messages were sent.
func (f *FakeMailer) AssertSentCount(t testing.TB, n int) {
	t.Helper()
	if got := f.SentCount(); got != n {
		t.Errorf("expected %d sent mails, got %d", n, got)
	}
}

// ── Cache ───────────────────────────────────────────────────────

// FakeCacheStore is the empty in-memory store FakeCache installs.
type FakeCacheStore struct {
	*cache.MemoryStore
}

// FakeCache gives the cache package helpers (cache.Get, cache.Remember, ...)
// an empty in-memory store for the rest of the test, so values cached by one
// test never reach another, then restores the previous global store.
func FakeCache(t testing.TB) *FakeCacheStore {
	t.Helper()
	claim(t, "cache")
	f := &FakeCacheStore{cache.NewMemoryStore()}
	prev := cache.GetGlobal()
	cache.SetGlobal(f)
	t.Cleanup(func() { cache.SetGlobal(prev) })
	return f
}

// AssertHas fails the test unless key is cached.
func (f *FakeCacheStore) AssertHas(t testing.TB, key string) {
	t.Helper()
	if _, ok := f.Get(key); !ok {
		t.Errorf("expected %q to be cached", key)
	}
}

// AssertMissing fails the test if key is cached.
func (f *FakeCacheStore) AssertMissing(t testing.TB, key string) {
	t.Helper()
	if v, ok := f.Get(key); ok {
		t.Errorf("expected %q not to be cached, got %v", key, v)
	}
}

// ── Storage ─────────────────────────────────────────────────────

// FakeDisk is an in-memory storage.Driver with test assertions.
type FakeDisk struct {
	*storage.MemoryDriver
}

// FakeStorage returns an empty in-memory disk. Storage has no global driver,
// so pass it wherever the code under test takes a storage.Driver.
//
//	disk := nimbustesting.FakeStorage(t)
//	app.Uploads = disk
//	client.Post("/avatar", body)
//	disk.AssertExists(t, "avatars/1.png")
func FakeStorage(t testing.TB) *FakeDisk {
	t.Helper()
	return &FakeDisk{storage.NewMemoryDriver()}
}

// Contents returns the stored bytes at path, failing the test if it is missing.
func (d *FakeDisk) Contents(t testing.TB, path string) []byte {
	t.Helper()
	rc, err := d.Get(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

// AssertExists fails the test unless a file is stored at path.
func (d *FakeDisk) AssertExists(t testing.TB, path string) {
	t.Helper()
	if ok, _ := d.Exists(path); !ok {
		t.Errorf("expected %s to exist; stored: %v", path, d.Paths())
	}
}

// AssertMissing fails the test if a file is stored at path.
func (d *FakeDisk) AssertMissing(t testing.TB, path string) {
	t.Helper()
	if ok, _ := d.Exists(path); ok {
		t.Errorf("expected %s not to exist", path)
	}
}

// AssertCount fails the test unless exactly n files are stored.
func (d *FakeDisk) AssertCount(t testing.TB, n int) {
	t.Helper()
	if got := d.Paths(); len(got) != n {
		t.Errorf("expected %d stored files, got %d: %v", n, len(got), got)
	}
}
