package resource

import "testing"

type user struct{ ID int }

func (u user) ToJSON() map[string]any { return map[string]any{"id": u.ID} }

func TestCollection(t *testing.T) {
	out := Collection([]Resource{
		user{ID: 1},
		ResourceFunc(func() map[string]any { return map[string]any{"id": 2} }),
	})
	if len(out) != 2 || out[0]["id"] != 1 || out[1]["id"] != 2 {
		t.Fatalf("Collection = %v", out)
	}
	if got := Collection(nil); len(got) != 0 {
		t.Fatalf("empty collection = %v", got)
	}
}
