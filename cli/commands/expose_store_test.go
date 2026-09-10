package commands

import (
	"testing"

	"github.com/CodeSyncr/nimbus/cli/auth"
)

func TestExposeStoreRemembersPerProject(t *testing.T) {
	t.Setenv(auth.ConfigDirEnv, t.TempDir())
	a, b := "/tmp/project-a", "/tmp/project-b"

	if got := rememberedSubdomain(a); got != "" {
		t.Fatalf("empty store returned %q", got)
	}
	rememberSubdomain(a, "myapp")
	rememberSubdomain(b, "other")
	if got := rememberedSubdomain(a); got != "myapp" {
		t.Fatalf("project a: %q", got)
	}
	if got := rememberedSubdomain(b); got != "other" {
		t.Fatalf("project b: %q", got)
	}

	// A later name replaces the old one; forgetting affects one project only.
	rememberSubdomain(a, "renamed")
	if got := rememberedSubdomain(a); got != "renamed" {
		t.Fatalf("after rename: %q", got)
	}
	forgetSubdomain(a)
	if got := rememberedSubdomain(a); got != "" {
		t.Fatalf("after forget: %q", got)
	}
	if got := rememberedSubdomain(b); got != "other" {
		t.Fatalf("forget touched the other project: %q", got)
	}

	// Outside a project there is nothing to key on, and nothing must panic.
	rememberSubdomain("", "nowhere")
	if got := rememberedSubdomain(""); got != "" {
		t.Fatalf("no app root: %q", got)
	}
}

func TestSubdomainOfURL(t *testing.T) {
	cases := map[string]string{
		"https://myapp.tunnel.nimbusgo.space":       "myapp",
		"https://brisk-otter-3f9a.tunnel.test/path": "brisk-otter-3f9a",
		"http://localhost:8090":                     "",
		"":                                          "",
	}
	for in, want := range cases {
		if got := subdomainOfURL(in); got != want {
			t.Errorf("subdomainOfURL(%q) = %q, want %q", in, got, want)
		}
	}
}
