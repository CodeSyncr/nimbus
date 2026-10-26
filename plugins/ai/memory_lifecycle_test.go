package ai

import (
	"context"
	"testing"
	"time"
)

func TestMemoryTTLAndSweep(t *testing.T) {
	ctx := context.Background()
	m := MemoryStore(WithMemoryTTL(time.Hour)).(*memoryStore)
	if err := m.Save(ctx, "expired", []Message{{Content: "old"}}); err != nil {
		t.Fatal(err)
	}
	m.data["expired"].Value.(*memoryEntry).expires = time.Now().Add(-time.Second)
	if got, _ := m.Load(ctx, "expired"); len(got) != 0 {
		t.Fatal("expired conversation returned")
	}
	m.Save(ctx, "unread", []Message{{Content: "old"}})
	m.data["unread"].Value.(*memoryEntry).expires = time.Now().Add(-time.Second)
	m.lastSweep = time.Now().Add(-2 * time.Minute)
	m.Save(ctx, "new", []Message{{Content: "new"}})
	if _, exists := m.data["unread"]; exists {
		t.Fatal("expired unread entry not swept")
	}
}
func TestMemoryLRUAndSnapshotIsolation(t *testing.T) {
	ctx := context.Background()
	m := MemoryStore(WithMemoryMaxEntries(2))
	messages := []Message{{Content: "one", Images: []string{"original"}, ToolCalls: []ToolCall{{Args: []byte(`{"x":1}`)}}}}
	m.Save(ctx, "one", messages)
	messages[0].Images[0] = "mutated"
	messages[0].ToolCalls[0].Args[5] = '9'
	got, _ := m.Load(ctx, "one")
	if got[0].Images[0] != "original" || string(got[0].ToolCalls[0].Args) != `{"x":1}` {
		t.Fatal("Save retained mutable input")
	}
	got[0].Images[0] = "mutated again"
	next, _ := m.Load(ctx, "one")
	if next[0].Images[0] != "original" {
		t.Fatal("Load returned mutable snapshot")
	}
	m.Save(ctx, "two", nil)
	m.Load(ctx, "one")
	m.Save(ctx, "three", nil)
	internal := m.(*memoryStore)
	if internal.data["two"] != nil || len(internal.data) != 2 {
		t.Fatal("LRU eviction failed")
	}
}
