package testing_test

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/CodeSyncr/nimbus/cache"
	"github.com/CodeSyncr/nimbus/mail"
	"github.com/CodeSyncr/nimbus/storage"
	nimbustesting "github.com/CodeSyncr/nimbus/testing"
)

func TestFakeMailCapturesSendAndRestores(t *testing.T) {
	prev := mail.Default
	t.Run("faked", func(t *testing.T) {
		mails := nimbustesting.FakeMail(t)
		mails.AssertNothingSent(t)
		m := mail.NewMessage("Welcome")
		m.To = []string{"a@b.co"}
		if err := mail.Send(m); err != nil {
			t.Fatal(err)
		}
		mails.AssertSentCount(t, 1)
		mails.AssertSentTo(t, "a@b.co")
		mails.AssertSent(t, func(m *mail.Message) bool { return m.Subject == "Welcome" })
	})
	if mail.Default != prev {
		t.Fatal("mail.Default not restored")
	}
}

func TestFakeCacheIsolatesAndRestores(t *testing.T) {
	outer := cache.NewMemoryStore()
	cache.SetGlobal(outer)
	t.Cleanup(func() { cache.SetGlobal(nil) })
	_ = cache.Set("shared", "outer", time.Minute)

	t.Run("faked", func(t *testing.T) {
		c := nimbustesting.FakeCache(t)
		c.AssertMissing(t, "shared")
		if _, err := cache.Remember("k", time.Minute, func() (any, error) { return 1, nil }); err != nil {
			t.Fatal(err)
		}
		c.AssertHas(t, "k")
	})
	if cache.GetGlobal() != cache.Store(outer) {
		t.Fatal("global cache not restored")
	}
	if _, ok := cache.Get("k"); ok {
		t.Fatal("value cached in a faked test leaked into the real store")
	}
}

func TestFakeStorage(t *testing.T) {
	disk := nimbustesting.FakeStorage(t)
	var d storage.Driver = disk
	if err := d.Put("avatars/../avatars/1.png", strings.NewReader("png")); err != nil {
		t.Fatal(err)
	}
	disk.AssertExists(t, "/avatars/1.png")
	disk.AssertCount(t, 1)
	if got := string(disk.Contents(t, "avatars/1.png")); got != "png" {
		t.Fatalf("contents = %q", got)
	}
	if err := d.Delete("avatars/1.png"); err != nil {
		t.Fatal(err)
	}
	disk.AssertMissing(t, "avatars/1.png")
	if _, err := d.Get("avatars/1.png"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Get missing: %v, want fs.ErrNotExist", err)
	}
	if err := d.Delete("nope"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Delete missing: %v, want fs.ErrNotExist", err)
	}
}
