package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalDriverRejectsEscapingSymlink(t *testing.T) {
	parent := t.TempDir()
	root, outside := filepath.Join(parent, "root"), filepath.Join(parent, "outside")
	for _, dir := range []string{root, outside} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	secret := filepath.Join(outside, "secret")
	if err := os.WriteFile(secret, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skip(err)
	}
	d := NewLocalDriver(root)
	if f, err := d.Get("link/secret"); err == nil {
		f.Close()
		t.Fatal("read escaped")
	}
	if err := d.Put("link/secret", strings.NewReader("overwritten")); err == nil {
		t.Fatal("write escaped")
	}
	if err := d.Delete("link/secret"); err == nil {
		t.Fatal("delete escaped")
	}
	if ok, err := d.Exists("link/secret"); err == nil || ok {
		t.Fatal("stat escaped")
	}
	got, err := os.ReadFile(secret)
	if err != nil || string(got) != "untouched" {
		t.Fatalf("outside changed: %s %v", got, err)
	}
}
func TestLocalDriverAllowsInternalRelativeSymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "real"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", filepath.Join(root, "link")); err != nil {
		t.Skip(err)
	}
	d := NewLocalDriver(root)
	if err := d.Put("link/file", strings.NewReader("ok")); err != nil {
		t.Fatal(err)
	}
	if got := readAll(t, d, "real/file"); got != "ok" {
		t.Fatal(got)
	}
}
