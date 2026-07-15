package commands

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetAppKey(t *testing.T) {
	t.Run("replaces existing key in place, preserving other lines", func(t *testing.T) {
		dir := t.TempDir()
		env := filepath.Join(dir, ".env")
		if err := os.WriteFile(env, []byte("APP_ENV=production\nAPP_KEY=old\nDB_DSN=x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := setAppKey(env, "newkey"); err != nil {
			t.Fatal(err)
		}
		got := readFile(t, env)
		want := "APP_ENV=production\nAPP_KEY=newkey\nDB_DSN=x\n"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("replaces empty key", func(t *testing.T) {
		dir := t.TempDir()
		env := filepath.Join(dir, ".env")
		if err := os.WriteFile(env, []byte("APP_KEY=\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := setAppKey(env, "k"); err != nil {
			t.Fatal(err)
		}
		if got := readFile(t, env); got != "APP_KEY=k\n" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("appends when APP_KEY absent", func(t *testing.T) {
		dir := t.TempDir()
		env := filepath.Join(dir, ".env")
		if err := os.WriteFile(env, []byte("APP_ENV=dev\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := setAppKey(env, "k"); err != nil {
			t.Fatal(err)
		}
		if got := readFile(t, env); got != "APP_ENV=dev\nAPP_KEY=k\n" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("creates file when missing", func(t *testing.T) {
		dir := t.TempDir()
		env := filepath.Join(dir, ".env")
		if err := setAppKey(env, "k"); err != nil {
			t.Fatal(err)
		}
		if got := readFile(t, env); got != "APP_KEY=k\n" {
			t.Fatalf("got %q", got)
		}
	})
}

func TestReadAppKey(t *testing.T) {
	dir := t.TempDir()
	env := filepath.Join(dir, ".env")

	// Missing file -> no key, no error.
	if v, had, err := readAppKey(env); err != nil || had || v != "" {
		t.Fatalf("missing file: got (%q,%v,%v)", v, had, err)
	}

	os.WriteFile(env, []byte("APP_KEY=abc123\n"), 0o600)
	if v, had, err := readAppKey(env); err != nil || !had || v != "abc123" {
		t.Fatalf("present key: got (%q,%v,%v)", v, had, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
