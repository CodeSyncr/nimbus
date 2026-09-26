package ai

import (
	"testing"

	"github.com/CodeSyncr/nimbus"
)

func TestReloadPicksUpNewModel(t *testing.T) {
	t.Setenv("AI_PROVIDER", "ollama")
	t.Setenv("AI_MODEL", "first-model")
	app := nimbus.New()
	if err := New().Register(app); err != nil {
		t.Fatal(err)
	}
	if got := GetClient().config.Model; got != "first-model" {
		t.Fatalf("registered with %q", got)
	}
	t.Setenv("AI_MODEL", "second-model")
	if err := Reload(); err != nil {
		t.Fatal(err)
	}
	if got := GetClient().config.Model; got != "second-model" {
		t.Fatalf("after Reload: %q", got)
	}
	c, err := app.Container.Make("ai.client")
	if err != nil {
		t.Fatal(err)
	}
	if got := c.(*Client).config.Model; got != "second-model" {
		t.Fatalf("container still hands out %q", got)
	}
}
