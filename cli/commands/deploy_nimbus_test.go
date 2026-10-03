package commands

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Secrets and local state never leave the machine; .nimbusignore is honoured.
func TestPackAppLeavesOutSecretsAndState(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"main.go": "package main", "go.mod": "module x", "vendor/m/a.go": "package m",
		".env": "SECRET=1", ".env.production": "SECRET=2", ".git/config": "x", "storage/db.sqlite": "x",
		"node_modules/a/index.js": "x", "tmp/build": "x", "notes/private.txt": "x", "public/app.css": "a{}",
	} {
		p := filepath.Join(root, name)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte(body), 0o644)
	}
	_ = os.WriteFile(filepath.Join(root, ".nimbusignore"), []byte("# local\nnotes\n"), 0o644)
	buf, n, err := packApp(root)
	if err != nil {
		t.Fatal(err)
	}
	gz, _ := gzip.NewReader(buf)
	tr := tar.NewReader(gz)
	var got []string
	for {
		h, err := tr.Next()
		if err != nil {
			break
		}
		if h.Typeflag == tar.TypeReg {
			got = append(got, h.Name)
		}
	}
	sort.Strings(got)
	want := ".nimbusignore go.mod main.go public/app.css vendor/m/a.go"
	if strings.Join(got, " ") != want || n != 5 {
		t.Errorf("packed %v (n=%d), want %s", got, n, want)
	}
}

func TestNimbusSlug(t *testing.T) {
	for in, want := range map[string]string{"My API": "my-api", "  shop_v2 ": "shop-v2", "---": ""} {
		if got := nimbusSlug(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}
