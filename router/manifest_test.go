package router

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	nhttp "github.com/CodeSyncr/nimbus/http"
)

func noop(_ *nhttp.Context) error { return nil }

func TestPathParams_BothSyntaxes(t *testing.T) {
	cases := []struct {
		path string
		want []string
	}{
		{"/posts", []string{}},
		{"/posts/:id", []string{"id"}},
		{"/posts/{id}", []string{"id"}},
		{"/sessions/{id}/diffs/{diffId}/approve", []string{"id", "diffId"}},
		{"/a/:foo/b/{bar}", []string{"foo", "bar"}},
	}
	for _, tc := range cases {
		got := PathParams(tc.path)
		if len(got) != len(tc.want) {
			t.Fatalf("PathParams(%q) = %v, want %v", tc.path, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("PathParams(%q) = %v, want %v", tc.path, got, tc.want)
			}
		}
	}
}

func TestDeriveName(t *testing.T) {
	cases := []struct {
		method, path, want string
	}{
		{"GET", "/api/repos", "api.repos.index"},
		{"POST", "/api/repos", "api.repos.store"},
		{"GET", "/api/sessions/{id}", "api.sessions.show"},
		{"DELETE", "/api/sessions/{id}", "api.sessions.destroy"},
		{"PUT", "/api/sessions/:id", "api.sessions.update"},
		{"GET", "/api/sessions/{id}/messages", "api.sessions.messages.index"},
		{"POST", "/api/sessions/{id}/messages", "api.sessions.messages.store"},
		{"POST", "/api/sessions/{id}/diffs/{diffId}/approve", "api.sessions.diffs.approve.store"},
		{"GET", "/health", "health.index"},
		{"GET", "/", "index"},
		{"GET", "/build/*", "build.index"},
	}
	for _, tc := range cases {
		if got := DeriveName(tc.method, tc.path); got != tc.want {
			t.Errorf("DeriveName(%s %s) = %q, want %q", tc.method, tc.path, got, tc.want)
		}
	}
}

// TestManifest_UnnamedRoutesAreIncluded is the regression test for gen:client
// emitting an empty registry: routes registered without .As() must still land
// in the manifest with a derived name.
func TestManifest_UnnamedRoutesAreIncluded(t *testing.T) {
	r := New()
	r.Get("/health", noop)
	r.Get("/api/repos", noop)
	r.Post("/api/repos", noop)
	r.Get("/api/sessions/{id}", noop)
	r.Delete("/api/sessions/{id}", noop)
	r.Post("/api/sessions/{id}/messages", noop)
	r.Get("/api/sessions/{id}/messages", noop)

	entries := Manifest(r)
	if len(entries) != 7 {
		t.Fatalf("expected 7 entries, got %d", len(entries))
	}
	byName := map[string]ManifestEntry{}
	for _, e := range entries {
		if e.Name == "" {
			t.Fatalf("entry has empty name: %+v", e)
		}
		byName[e.Name] = e
	}
	for _, want := range []string{
		"health.index", "api.repos.index", "api.repos.store",
		"api.sessions.show", "api.sessions.destroy",
		"api.sessions.messages.store", "api.sessions.messages.index",
	} {
		if _, ok := byName[want]; !ok {
			t.Errorf("missing derived route name %q; got %v", want, keys(byName))
		}
	}
	// Params are extracted from {id} syntax.
	if p := byName["api.sessions.show"].Params; len(p) != 1 || p[0] != "id" {
		t.Errorf("api.sessions.show params = %v, want [id]", p)
	}
}

func TestManifest_ExplicitNamesWinAndAreUnique(t *testing.T) {
	r := New()
	r.Get("/api/posts", noop).As("posts.index")
	r.Post("/api/posts", noop) // derived → api.posts.store

	entries := Manifest(r)
	names := map[string]bool{}
	for _, e := range entries {
		if names[e.Name] {
			t.Fatalf("duplicate route name %q", e.Name)
		}
		names[e.Name] = true
	}
	if !names["posts.index"] {
		t.Errorf("explicit name not preserved; got %v", keys2(names))
	}
	if !names["api.posts.store"] {
		t.Errorf("derived name missing; got %v", keys2(names))
	}
}

func TestManifest_CollisionGetsMethodSuffix(t *testing.T) {
	r := New()
	// Force a collision: an explicit name equal to what the next route derives.
	r.Get("/x", noop).As("api.posts.store")
	r.Post("/api/posts", noop)

	names := map[string]bool{}
	for _, e := range Manifest(r) {
		if names[e.Name] {
			t.Fatalf("duplicate name %q", e.Name)
		}
		names[e.Name] = true
	}
	if !names["api.posts.store.post"] {
		t.Errorf("expected collision suffix api.posts.store.post; got %v", keys2(names))
	}
}

func TestWriteManifest(t *testing.T) {
	r := New()
	r.Get("/api/sessions/{id}", noop)

	dir := t.TempDir()
	if err := WriteManifest(r, dir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ManifestFileName))
	if err != nil {
		t.Fatal(err)
	}
	var entries []ManifestEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "api.sessions.show" || entries[0].Path != "/api/sessions/{id}" {
		t.Fatalf("manifest round-trip: %+v", entries)
	}
}

func keys(m map[string]ManifestEntry) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func keys2(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
