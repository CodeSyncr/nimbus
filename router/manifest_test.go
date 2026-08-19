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

type DummySubStruct struct {
	Foo string `json:"foo"`
}

type DummyResponse struct {
	ID        int             `json:"id"`
	Title     string          `json:"title,omitempty"`
	Sub       DummySubStruct  `json:"sub"`
	PtrSub    *DummySubStruct `json:"ptr_sub"`
	Tags      []string        `json:"tags"`
	SecretVal string          `json:"-"`
}

func TestManifest_ResponseSerialization(t *testing.T) {
	r := New()
	r.Get("/test", noop).Response(&DummyResponse{})

	entries := Manifest(r)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry.Response == nil {
		t.Fatal("expected Response to be non-nil")
	}

	if entry.Response.Kind != "struct" {
		t.Fatalf("expected Kind struct, got %s", entry.Response.Kind)
	}

	fields := entry.Response.Fields
	if id, ok := fields["id"]; !ok || id.Kind != "primitive" || id.Type != "number" {
		t.Errorf("invalid fields[id]: %+v", id)
	}

	if title, ok := fields["title"]; !ok || title.Kind != "primitive" || title.Type != "string" || !title.IsNullable {
		t.Errorf("invalid fields[title]: %+v", title)
	}

	if sub, ok := fields["sub"]; !ok || sub.Kind != "struct" || sub.Fields["foo"].Type != "string" {
		t.Errorf("invalid fields[sub]: %+v", sub)
	}

	if ptrSub, ok := fields["ptr_sub"]; !ok || ptrSub.Kind != "struct" || !ptrSub.IsNullable || ptrSub.Fields["foo"].Type != "string" {
		t.Errorf("invalid fields[ptr_sub]: %+v", ptrSub)
	}

	if tags, ok := fields["tags"]; !ok || tags.Kind != "array" || tags.Elem.Type != "string" {
		t.Errorf("invalid fields[tags]: %+v", tags)
	}

	if _, ok := fields["SecretVal"]; ok {
		t.Error("SecretVal should be excluded due to json:\"-\"")
	}
}

type CircularNode struct {
	Value int           `json:"value"`
	Next  *CircularNode `json:"next"`
}

func TestManifest_CircularResponseSerialization(t *testing.T) {
	r := New()
	r.Get("/circular", noop).Response(CircularNode{})

	entries := Manifest(r)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry.Response == nil {
		t.Fatal("expected Response to be non-nil")
	}

	nextField := entry.Response.Fields["next"]
	if nextField.Kind != "any" {
		t.Errorf("expected circular reference nextField to be kind 'any', got %s", nextField.Kind)
	}
}

