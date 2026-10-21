package ai

import (
	"context"
	"testing"
)

func TestFakeInstallsForTheTestAndRestores(t *testing.T) {
	clientMu.Lock()
	prev := globalClient
	clientMu.Unlock()

	t.Run("faked", func(t *testing.T) {
		fake := Fake(t, FakeText("hi"))
		resp, err := Generate(context.Background(), "hello")
		if err != nil {
			t.Fatal(err)
		}
		if resp.Text != "hi" || len(fake.Requests()) != 1 {
			t.Fatalf("resp = %q, requests = %d", resp.Text, len(fake.Requests()))
		}
	})

	clientMu.Lock()
	restored := globalClient == prev
	clientMu.Unlock()
	if !restored {
		t.Fatal("global client not restored after the test")
	}
	// The slot is free again once the faking test ended.
	t.Run("again", func(t *testing.T) { Fake(t) })
}
